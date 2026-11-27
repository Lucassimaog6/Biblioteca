package bookmetadata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSearchCombinesGoogleBooksAndOpenLibrary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/google":
			io.WriteString(w, `{"items":[{"id":"google-id","volumeInfo":{"title":"The Book","authors":["Author"],"description":"A description","publisher":"Publisher","publishedDate":"2024","pageCount":240,"categories":["Fiction"],"language":"en","industryIdentifiers":[{"type":"ISBN_13","identifier":"9780000000000"}],"imageLinks":{"thumbnail":"http://books.google.com/books/content?id=google-id"}}}]}`)
		case "/openlibrary":
			io.WriteString(w, `{"docs":[{"key":"/works/OL1W","title":"Another Book","author_name":["Writer"],"first_publish_year":2020,"publisher":["House"],"isbn":["9781111111111"],"language":["eng"],"number_of_pages_median":180,"subject":["Novel"],"first_sentence":["A first sentence."],"cover_i":123}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := NewService()
	service.client = server.Client()
	service.googleURL = server.URL + "/google"
	service.openLibraryURL = server.URL + "/openlibrary"
	results, warnings, err := service.Search(context.Background(), "book title")
	if err != nil {
		t.Fatalf("search metadata: %v", err)
	}
	if len(warnings) != 0 || len(results) != 2 {
		t.Fatalf("search results=%+v warnings=%v", results, warnings)
	}
	google := results[0]
	if google.Provider != "google_books" || google.ID != "google-id" || google.Author != "Author" ||
		google.ISBN != "9780000000000" || google.CoverURL != "https://books.google.com/books/content?id=google-id" {
		t.Fatalf("unexpected Google Books result: %+v", google)
	}
	openLibrary := results[1]
	if openLibrary.Provider != "open_library" || openLibrary.ID != "/works/OL1W" ||
		openLibrary.Publisher != "House" || openLibrary.PageCount != 180 ||
		openLibrary.Description != "A first sentence." || openLibrary.CoverURL != "https://covers.openlibrary.org/b/id/123-M.jpg" {
		t.Fatalf("unexpected Open Library result: %+v", openLibrary)
	}
}

func TestSearchReturnsPartialResultsAndRejectsInvalidQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/google" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"docs":[{"key":"/works/OL2W","title":"Available"}]}`)
	}))
	defer server.Close()
	service := NewService()
	service.client = server.Client()
	service.googleURL = server.URL + "/google"
	service.openLibraryURL = server.URL + "/openlibrary"
	results, warnings, err := service.Search(context.Background(), "book")
	if err != nil || len(results) != 1 || len(warnings) != 1 || warnings[0] != "Google Books" {
		t.Fatalf("partial search results=%+v warnings=%v error=%v", results, warnings, err)
	}
	if _, _, err := service.Search(context.Background(), strings.Repeat("x", 201)); err == nil {
		t.Fatal("expected long search query to be rejected")
	}
}

func TestDownloadCoverRestrictsHostAndValidatesImage(t *testing.T) {
	service := NewService()
	service.client = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host != "covers.openlibrary.org" {
			t.Fatalf("cover host = %q", request.URL.Host)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string([]byte{0xff, 0xd8, 0xff, 0xd9}))),
		}, nil
	})}
	image, err := service.DownloadCover(context.Background(), "open_library", "https://covers.openlibrary.org/b/id/123-M.jpg")
	if err != nil || len(image) == 0 {
		t.Fatalf("download valid cover: bytes=%d error=%v", len(image), err)
	}
	if _, err := service.DownloadCover(context.Background(), "open_library", "http://127.0.0.1/image.jpg"); err == nil {
		t.Fatal("expected untrusted cover URL to be rejected")
	}
	if _, err := service.DownloadCover(context.Background(), "google_books", "https://covers.openlibrary.org/b/id/123-M.jpg"); err == nil {
		t.Fatal("expected provider and cover host mismatch to be rejected")
	}
}

func TestProviderRedirectPolicyRestrictsDestinations(t *testing.T) {
	allowedURL, err := url.Parse("https://books.googleusercontent.com/books/content")
	if err != nil {
		t.Fatal(err)
	}
	allowed := &http.Request{URL: allowedURL}
	if err := allowProviderRedirects(allowed, []*http.Request{{}}); err != nil {
		t.Fatalf("approved cover redirect rejected: %v", err)
	}

	for _, target := range []string{
		"http://books.google.com/books/content",
		"https://example.com/image.jpg",
		"https://covers.openlibrary.org:8443/b/id/1-M.jpg",
	} {
		redirectURL, err := url.Parse(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := allowProviderRedirects(&http.Request{URL: redirectURL}, []*http.Request{{}}); err == nil {
			t.Errorf("unapproved redirect %q was allowed", target)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
