package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucassimaog6/Biblioteca/internal/auth"
	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

func TestRegistrationAndAuthenticatedDashboardFlow(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "users.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	handler, err := New(auth.NewService(store), false)
	if err != nil {
		t.Fatal(err)
	}

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", unauthenticated.Code, http.StatusUnauthorized)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"email":"reader@example.com","password":"correct-horse"}`))
	request.Header.Set("Content-Type", "application/json")
	registered := httptest.NewRecorder()
	handler.ServeHTTP(registered, request)
	if registered.Code != http.StatusCreated {
		t.Fatalf("registration status = %d, body: %s", registered.Code, registered.Body.String())
	}
	cookie := registered.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie missing security attributes: %+v", cookie)
	}
	if strings.Contains(registered.Body.String(), "password") {
		t.Fatalf("registration response exposed password data: %s", registered.Body.String())
	}

	currentRequest := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	currentRequest.AddCookie(cookie)
	current := httptest.NewRecorder()
	handler.ServeHTTP(current, currentRequest)
	if current.Code != http.StatusOK {
		t.Fatalf("current user status = %d, body: %s", current.Code, current.Body.String())
	}
	var response struct {
		User storage.User `json:"user"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.User.Email != "reader@example.com" {
		t.Fatalf("current user email = %q", response.User.Email)
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.AddCookie(cookie)
	logout := httptest.NewRecorder()
	handler.ServeHTTP(logout, logoutRequest)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body: %s", logout.Code, logout.Body.String())
	}

	currentRequest = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	currentRequest.AddCookie(cookie)
	current = httptest.NewRecorder()
	handler.ServeHTTP(current, currentRequest)
	if current.Code != http.StatusUnauthorized {
		t.Fatalf("status after logout = %d, want %d", current.Code, http.StatusUnauthorized)
	}
}
