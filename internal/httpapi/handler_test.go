package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucassimaog6/Biblioteca/internal/auth"
	"github.com/Lucassimaog6/Biblioteca/internal/books"
	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

func TestBookCatalogUploadEditAndReadAuthorization(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "users.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bookService, err := books.NewService(store, filepath.Join(t.TempDir(), "books"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(auth.NewService(store), bookService, false)
	if err != nil {
		t.Fatal(err)
	}

	ownerCookie := registerTestUser(t, handler, "owner@example.com")
	createdResponse := performBookRequest(t, handler, http.MethodPost, "/api/v1/books", ownerCookie, "Book", "Author", "Description", "first.epub", "EPUB bytes")
	if createdResponse.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, body: %s", createdResponse.Code, createdResponse.Body.String())
	}
	var created struct {
		Book storage.Book `json:"book"`
	}
	if err := json.Unmarshal(createdResponse.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Book.ID == 0 || !created.Book.CanEdit || created.Book.Filename != "first.epub" {
		t.Fatalf("unexpected upload response: %+v", created.Book)
	}

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/books", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated catalog status = %d", unauthorized.Code)
	}

	otherCookie := registerTestUser(t, handler, "other@example.com")
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/books", nil)
	listRequest.AddCookie(otherCookie)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	var listed struct {
		Books []storage.Book `json:"books"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listResponse.Code != http.StatusOK || len(listed.Books) != 1 || listed.Books[0].CanEdit {
		t.Fatalf("other account should see the shared book without edit permission: status=%d books=%+v", listResponse.Code, listed.Books)
	}

	forbidden := performBookRequest(t, handler, http.MethodPut,
		fmt.Sprintf("/api/v1/books/%d", created.Book.ID), otherCookie, "Attempted edit", "", "", "", "")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("non-owner edit status = %d, want %d", forbidden.Code, http.StatusForbidden)
	}

	fileRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/books/%d/file", created.Book.ID), nil)
	fileRequest.AddCookie(ownerCookie)
	fileResponse := httptest.NewRecorder()
	handler.ServeHTTP(fileResponse, fileRequest)
	if fileResponse.Code != http.StatusOK || fileResponse.Header().Get("Content-Type") != "application/epub+zip" || fileResponse.Body.String() != "EPUB bytes" {
		t.Fatalf("EPUB response status=%d type=%q body=%q", fileResponse.Code, fileResponse.Header().Get("Content-Type"), fileResponse.Body.String())
	}

	readerResponse := httptest.NewRecorder()
	handler.ServeHTTP(readerResponse, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/read/%d", created.Book.ID), nil))
	if readerResponse.Code != http.StatusOK || !strings.Contains(readerResponse.Body.String(), "Leitor de PDF") {
		t.Fatalf("reader page status=%d", readerResponse.Code)
	}
	policy := readerResponse.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "script-src 'nonce-") || strings.Contains(policy, "script-src 'self'") {
		t.Fatalf("reader CSP should restrict script execution to a per-request nonce: %q", policy)
	}
	directiveStart := strings.Index(policy, "script-src 'nonce-")
	if directiveStart < 0 {
		t.Fatalf("reader CSP should include a nonce source: %q", policy)
	}
	nonceStart := directiveStart + len("script-src 'nonce-")
	nonceEnd := strings.Index(policy[nonceStart:], "'")
	if nonceEnd < 1 {
		t.Fatalf("could not parse reader nonce from CSP: %q", policy)
	}
	nonce := policy[nonceStart : nonceStart+nonceEnd]
	if !strings.Contains(readerResponse.Body.String(), `nonce="`+nonce+`"`) {
		t.Fatal("reader scripts should carry the CSP nonce")
	}

	updatedResponse := performBookRequest(t, handler, http.MethodPut,
		fmt.Sprintf("/api/v1/books/%d", created.Book.ID), ownerCookie, "Revised title", "New author", "Updated", "new.pdf", "%PDF-1.7 data")
	if updatedResponse.Code != http.StatusOK {
		t.Fatalf("owner update status = %d, body: %s", updatedResponse.Code, updatedResponse.Body.String())
	}
	pdfRequest := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/v1/books/%d/file", created.Book.ID), nil)
	pdfRequest.AddCookie(ownerCookie)
	pdfResponse := httptest.NewRecorder()
	handler.ServeHTTP(pdfResponse, pdfRequest)
	if pdfResponse.Code != http.StatusOK || pdfResponse.Header().Get("Content-Type") != "application/pdf" || pdfResponse.Body.String() != "%PDF-1.7 data" {
		t.Fatalf("PDF response status=%d type=%q body=%q", pdfResponse.Code, pdfResponse.Header().Get("Content-Type"), pdfResponse.Body.String())
	}
	if pdfResponse.Header().Get("Content-Security-Policy") != "default-src 'none'; frame-ancestors 'self'" || pdfResponse.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("PDF response should allow embedding only by this origin: CSP=%q X-Frame-Options=%q",
			pdfResponse.Header().Get("Content-Security-Policy"), pdfResponse.Header().Get("X-Frame-Options"))
	}

	replacement := performBookRequest(t, handler, http.MethodPut,
		fmt.Sprintf("/api/v1/books/%d", created.Book.ID), ownerCookie, "Metadata only", "", "", "", "")
	if replacement.Code != http.StatusOK {
		t.Fatalf("metadata-only update status=%d body=%s", replacement.Code, replacement.Body.String())
	}
	var updated struct {
		Book storage.Book `json:"book"`
	}
	if err := json.Unmarshal(replacement.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Book.Filename != "new.pdf" || updated.Book.Title != "Metadata only" {
		t.Fatalf("metadata-only update changed or lost book file: %+v", updated.Book)
	}
}

func TestRegistrationAndAuthenticatedDashboardFlow(t *testing.T) {
	store, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "users.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	bookService, err := books.NewService(store, filepath.Join(t.TempDir(), "books"))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(auth.NewService(store), bookService, false)
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

func registerTestUser(t *testing.T, handler http.Handler, email string) *http.Cookie {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(fmt.Sprintf(`{"email":%q,"password":"correct-horse"}`, email)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("register test user status=%d body=%s", response.Code, response.Body.String())
	}
	return response.Result().Cookies()[0]
}

func performBookRequest(t *testing.T, handler http.Handler, method, path string, cookie *http.Cookie, title, author, description, filename, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("title", title)
	if author != "" {
		_ = writer.WriteField("author", author)
	}
	if description != "" {
		_ = writer.WriteField("description", description)
	}
	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
