package httpapi

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/Lucassimaog6/Biblioteca/internal/auth"
	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

const sessionCookieName = "biblioteca_session"

//go:embed web/*
var webFiles embed.FS

type Handler struct {
	auth *auth.Service
	web  fs.FS
}

func New(authService *auth.Service) (http.Handler, error) {
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	handler := &Handler{auth: authService, web: web}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", handler.home)
	mux.HandleFunc("GET /login", handler.page("login.html"))
	mux.HandleFunc("GET /register", handler.page("register.html"))
	mux.HandleFunc("GET /dashboard", handler.page("dashboard.html"))
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(handler.web))))

	mux.HandleFunc("POST /api/v1/auth/register", handler.register)
	mux.HandleFunc("POST /api/v1/auth/login", handler.login)
	mux.HandleFunc("POST /api/v1/auth/logout", handler.logout)
	mux.HandleFunc("GET /api/v1/auth/me", handler.currentUser)

	return handler.securityHeaders(mux), nil
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, h.web, name)
	}
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var request credentials
	if !decodeJSON(w, r, &request) {
		return
	}
	user, token, err := h.auth.Register(r.Context(), request.Email, request.Password)
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "Use um e-mail válido e uma senha de 8 a 72 caracteres.")
		return
	case errors.Is(err, auth.ErrEmailExists):
		writeError(w, http.StatusConflict, "Já existe uma conta com este e-mail.")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "Não foi possível criar sua conta.")
		return
	}
	h.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusCreated, map[string]storage.User{"user": user})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request credentials
	if !decodeJSON(w, r, &request) {
		return
	}
	user, token, err := h.auth.Login(r.Context(), request.Email, request.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "E-mail ou senha inválidos.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Não foi possível entrar.")
		return
	}
	h.setSessionCookie(w, r, token)
	writeJSON(w, http.StatusOK, map[string]storage.User{"user": user})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		if err := h.auth.Logout(r.Context(), cookie.Value); err != nil {
			writeError(w, http.StatusInternalServerError, "Não foi possível sair da conta.")
			return
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "Sessão encerrada."})
}

func (h *Handler) currentUser(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Autenticação necessária.")
		return
	}
	user, err := h.auth.UserForToken(r.Context(), cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Autenticação necessária.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]storage.User{"user": user})
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(auth.SessionLifetime.Seconds()),
	})
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "Envie os dados no formato JSON.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "Dados inválidos.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Dados inválidos.")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (h *Handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
