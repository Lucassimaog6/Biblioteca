package httpapi

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Lucassimaog6/Biblioteca/internal/auth"
	"github.com/Lucassimaog6/Biblioteca/internal/bookmetadata"
	"github.com/Lucassimaog6/Biblioteca/internal/books"
	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

const sessionCookieName = "biblioteca_session"

//go:embed web/*
var webFiles embed.FS

type Handler struct {
	auth          *auth.Service
	books         *books.Service
	metadata      *bookmetadata.Service
	web           fs.FS
	secureCookies bool
}

func New(authService *auth.Service, bookService *books.Service, secureCookies bool, metadataServices ...*bookmetadata.Service) (http.Handler, error) {
	web, err := fs.Sub(webFiles, "web")
	if err != nil {
		return nil, err
	}
	metadataService := bookmetadata.NewService()
	if len(metadataServices) > 0 && metadataServices[0] != nil {
		metadataService = metadataServices[0]
	}
	handler := &Handler{auth: authService, books: bookService, metadata: metadataService, web: web, secureCookies: secureCookies}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", handler.home)
	mux.HandleFunc("GET /login", handler.page("login.html"))
	mux.HandleFunc("GET /register", handler.page("register.html"))
	mux.HandleFunc("GET /dashboard", handler.page("dashboard.html"))
	mux.HandleFunc("GET /read/{id}", handler.readerPage)
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(handler.web))))

	mux.HandleFunc("POST /api/v1/auth/register", handler.register)
	mux.HandleFunc("POST /api/v1/auth/login", handler.login)
	mux.HandleFunc("POST /api/v1/auth/logout", handler.logout)
	mux.HandleFunc("GET /api/v1/auth/me", handler.currentUser)
	mux.HandleFunc("GET /api/v1/books", handler.listBooks)
	mux.HandleFunc("GET /api/v1/books/metadata/search", handler.searchBookMetadata)
	mux.HandleFunc("POST /api/v1/books", handler.createBook)
	mux.HandleFunc("GET /api/v1/books/{id}", handler.getBook)
	mux.HandleFunc("PUT /api/v1/books/{id}", handler.updateBook)
	mux.HandleFunc("GET /api/v1/books/{id}/cover", handler.bookCover)
	mux.HandleFunc("GET /api/v1/books/{id}/file", handler.bookFile)
	mux.HandleFunc("GET /api/v1/books/{id}/file/{filename}", handler.bookFile)

	return handler.securityHeaders(mux), nil
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (h *Handler) readerPage(w http.ResponseWriter, r *http.Request) {
	if _, err := parseBookID(r); err != nil {
		http.NotFound(w, r)
		return
	}
	var nonceBytes [18]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		http.Error(w, "Não foi possível abrir o leitor.", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(nonceBytes[:])
	page, err := fs.ReadFile(h.web, "reader.html")
	if err != nil {
		http.Error(w, "Não foi possível abrir o leitor.", http.StatusInternalServerError)
		return
	}
	page = bytes.ReplaceAll(page, []byte("{{CSP_NONCE}}"), []byte(nonce))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", fmt.Sprintf(
		"default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'nonce-%s' 'unsafe-eval'; connect-src 'self' blob:; frame-src 'self' blob:; img-src 'self' data: blob:; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'",
		nonce,
	))
	_, _ = w.Write(page)
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
		writeError(w, http.StatusBadRequest, "Use um e-mail válido e uma senha de pelo menos 8 caracteres e até 72 bytes.")
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
		Secure:   r.TLS != nil || h.secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"message": "Sessão encerrada."})
}

func (h *Handler) currentUser(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticatedUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]storage.User{"user": user})
}

func (h *Handler) authenticatedUser(w http.ResponseWriter, r *http.Request) (storage.User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "Autenticação necessária.")
		return storage.User{}, false
	}
	user, err := h.auth.UserForToken(r.Context(), cookie.Value)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(w, http.StatusUnauthorized, "Autenticação necessária.")
		return storage.User{}, false
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Não foi possível validar a sessão.")
		return storage.User{}, false
	}
	return user, true
}

func (h *Handler) listBooks(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticatedUser(w, r)
	if !ok {
		return
	}
	list, err := h.books.List(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Não foi possível carregar os livros.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"books": list})
}

func (h *Handler) searchBookMetadata(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticatedUser(w, r); !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if query == "" || len(query) > 200 {
		writeError(w, http.StatusBadRequest, "Informe um título de até 200 caracteres.")
		return
	}
	results, warnings, err := h.metadata.Search(r.Context(), query)
	if errors.Is(err, bookmetadata.ErrUnavailable) {
		writeError(w, http.StatusBadGateway, "Os provedores de metadados estão indisponíveis.")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "Não foi possível pesquisar os metadados.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "warnings": warnings})
}

func (h *Handler) getBook(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticatedUser(w, r)
	if !ok {
		return
	}
	id, err := parseBookID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identificador de livro inválido.")
		return
	}
	book, err := h.books.Get(r.Context(), id, user.ID)
	if errors.Is(err, storage.ErrBookNotFound) {
		writeError(w, http.StatusNotFound, "Livro não encontrado.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Não foi possível carregar o livro.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]storage.Book{"book": book})
}

func (h *Handler) createBook(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticatedUser(w, r)
	if !ok {
		return
	}
	metadata, file, filename, ok := parseBookUpload(w, r, true)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !ok {
		return
	}
	if !h.resolveSelectedCover(w, r, &metadata) {
		return
	}
	defer file.Close()

	book, err := h.books.Create(r.Context(), user.ID, metadata, filename, file)
	if err != nil {
		writeBookError(w, err, "Não foi possível enviar o livro.")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]storage.Book{"book": book})
}

func (h *Handler) updateBook(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticatedUser(w, r)
	if !ok {
		return
	}
	id, err := parseBookID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identificador de livro inválido.")
		return
	}
	book, err := h.books.Get(r.Context(), id, user.ID)
	if errors.Is(err, storage.ErrBookNotFound) {
		writeError(w, http.StatusNotFound, "Livro não encontrado.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Não foi possível carregar o livro.")
		return
	}
	if !book.CanEdit {
		writeError(w, http.StatusForbidden, "Somente o proprietário pode editar este livro.")
		return
	}

	metadata, file, filename, ok := parseBookUpload(w, r, false)
	if r.MultipartForm != nil {
		defer r.MultipartForm.RemoveAll()
	}
	if !ok {
		return
	}
	if !h.resolveSelectedCover(w, r, &metadata) {
		return
	}
	if file != nil {
		defer file.Close()
	}
	updated, err := h.books.Update(r.Context(), id, user.ID, metadata, filename, file)
	if err != nil {
		writeBookError(w, err, "Não foi possível atualizar o livro.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]storage.Book{"book": updated})
}

func (h *Handler) bookCover(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticatedUser(w, r); !ok {
		return
	}
	id, err := parseBookID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identificador de livro inválido.")
		return
	}
	book, file, contentType, err := h.books.OpenCover(r.Context(), id)
	if errors.Is(err, storage.ErrBookNotFound) || errors.Is(err, os.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Não foi possível abrir a capa.")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, book.CoverKey, book.UpdatedAt, file)
}

func (h *Handler) bookFile(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authenticatedUser(w, r); !ok {
		return
	}
	id, err := parseBookID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Identificador de livro inválido.")
		return
	}
	book, file, err := h.books.OpenFile(r.Context(), id)
	if errors.Is(err, storage.ErrBookNotFound) {
		writeError(w, http.StatusNotFound, "Livro não encontrado.")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Não foi possível abrir o arquivo.")
		return
	}
	defer file.Close()
	if filename := r.PathValue("filename"); filename != "" && filename != book.Filename {
		http.NotFound(w, r)
		return
	}

	contentType := "application/octet-stream"
	switch strings.ToLower(filepath.Ext(book.Filename)) {
	case ".epub":
		contentType = "application/epub+zip"
	case ".pdf":
		contentType = "application/pdf"
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'self'")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": book.Filename}))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, book.Filename, book.UpdatedAt, file)
}

func parseBookID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, fmt.Errorf("invalid book ID")
	}
	return id, nil
}

func parseBookUpload(w http.ResponseWriter, r *http.Request, requireFile bool) (books.Metadata, multipart.File, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, books.MaxUploadSize+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxError *http.MaxBytesError
		if errors.As(err, &maxError) {
			writeError(w, http.StatusRequestEntityTooLarge, "O arquivo excede o limite de 100 MiB.")
		} else {
			writeError(w, http.StatusBadRequest, "Formulário inválido.")
		}
		return books.Metadata{}, nil, "", false
	}

	form := r.MultipartForm
	if form == nil {
		writeError(w, http.StatusBadRequest, "Formulário inválido.")
		return books.Metadata{}, nil, "", false
	}
	allowedFields := map[string]bool{
		"title": true, "author": true, "description": true, "publisher": true,
		"published_date": true, "isbn": true, "language": true, "page_count": true,
		"categories": true, "metadata_source": true, "metadata_id": true,
		"provider": true, "provider_id": true, "cover_url": true, "metadata_selected": true,
	}
	for field, values := range form.Value {
		if !allowedFields[field] || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "Metadados inválidos.")
			return books.Metadata{}, nil, "", false
		}
	}
	titleValues := form.Value["title"]
	if len(titleValues) != 1 {
		writeError(w, http.StatusBadRequest, "Informe o título do livro.")
		return books.Metadata{}, nil, "", false
	}
	metadata := books.Metadata{
		Title:          titleValues[0],
		Author:         firstFormValue(form.Value["author"]),
		Description:    firstFormValue(form.Value["description"]),
		Publisher:      firstFormValue(form.Value["publisher"]),
		PublishedDate:  firstFormValue(form.Value["published_date"]),
		ISBN:           firstFormValue(form.Value["isbn"]),
		Language:       firstFormValue(form.Value["language"]),
		MetadataSource: firstFormValue(form.Value["provider"]),
		MetadataID:     firstFormValue(form.Value["provider_id"]),
		CoverURL:       firstFormValue(form.Value["cover_url"]),
	}
	pageCount := 0
	if rawPageCount := firstFormValue(form.Value["page_count"]); rawPageCount != "" {
		parsedPageCount, err := strconv.Atoi(rawPageCount)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Número de páginas inválido.")
			return books.Metadata{}, nil, "", false
		}
		pageCount = parsedPageCount
	}
	metadata.PageCount = pageCount
	var categories []string
	if rawCategories := firstFormValue(form.Value["categories"]); rawCategories != "" {
		if err := json.Unmarshal([]byte(rawCategories), &categories); err != nil || len(categories) > 50 {
			writeError(w, http.StatusBadRequest, "Categorias inválidas.")
			return books.Metadata{}, nil, "", false
		}
		metadata.Categories = categories
	}
	selected := firstFormValue(form.Value["metadata_selected"])
	if selected != "" && selected != "true" && selected != "false" {
		writeError(w, http.StatusBadRequest, "Seleção de metadados inválida.")
		return books.Metadata{}, nil, "", false
	}
	metadataSelected := selected == "true"
	metadata.CoverChanged = metadataSelected && metadata.CoverURL != ""
	if metadataSelected && (metadata.MetadataSource == "" || metadata.MetadataID == "") {
		writeError(w, http.StatusBadRequest, "Selecione um resultado de metadados válido.")
		return books.Metadata{}, nil, "", false
	}
	if metadataSelected && metadata.MetadataSource != "google_books" && metadata.MetadataSource != "open_library" {
		writeError(w, http.StatusBadRequest, "Provedor de metadados inválido.")
		return books.Metadata{}, nil, "", false
	}

	var fileHeaders []*multipart.FileHeader
	for field, headers := range form.File {
		if field != "file" {
			writeError(w, http.StatusBadRequest, "Campo de arquivo inválido.")
			return books.Metadata{}, nil, "", false
		}
		fileHeaders = headers
	}
	if len(fileHeaders) > 1 || (requireFile && len(fileHeaders) != 1) {
		writeError(w, http.StatusBadRequest, "Envie exatamente um arquivo de livro.")
		return books.Metadata{}, nil, "", false
	}
	if len(fileHeaders) == 0 {
		return metadata, nil, "", true
	}
	file, err := fileHeaders[0].Open()
	if err != nil {
		writeError(w, http.StatusBadRequest, "Não foi possível ler o arquivo enviado.")
		return books.Metadata{}, nil, "", false
	}
	return metadata, file, fileHeaders[0].Filename, true
}

func (h *Handler) resolveSelectedCover(w http.ResponseWriter, r *http.Request, metadata *books.Metadata) bool {
	if !metadata.CoverChanged || metadata.CoverURL == "" {
		return true
	}
	image, err := h.metadata.DownloadCover(r.Context(), metadata.MetadataSource, metadata.CoverURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Não foi possível salvar a capa selecionada.")
		return false
	}
	metadata.CoverImage = image
	return true
}

func firstFormValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func writeBookError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, books.ErrInvalidBook):
		writeError(w, http.StatusBadRequest, "Verifique o título, os metadados e o arquivo enviado.")
	case errors.Is(err, books.ErrFileTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "O arquivo excede o limite de 100 MiB.")
	case errors.Is(err, storage.ErrBookNotFound):
		writeError(w, http.StatusNotFound, "Livro não encontrado.")
	case errors.Is(err, storage.ErrBookNotOwned):
		writeError(w, http.StatusForbidden, "Somente o proprietário pode editar este livro.")
	case errors.Is(err, storage.ErrBookChanged):
		writeError(w, http.StatusConflict, "O livro foi alterado. Atualize a página e tente novamente.")
	default:
		writeError(w, http.StatusInternalServerError, fallback)
	}
}

func (h *Handler) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || h.secureCookies,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(auth.SessionLifetime.Seconds()),
	})
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; frame-src 'self'; img-src 'self' https://books.google.com https://books.googleusercontent.com https://covers.openlibrary.org; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
