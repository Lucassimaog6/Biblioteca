package bookmetadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxResultsPerProvider = 5
	maxResponseSize       = 2 << 20
	maxCoverSize          = 5 << 20
)

var ErrUnavailable = errors.New("book metadata providers unavailable")

type Result struct {
	Provider     string   `json:"provider"`
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Author       string   `json:"author"`
	Description  string   `json:"description"`
	Publisher    string   `json:"publisher"`
	PublishedDate string  `json:"published_date"`
	ISBN         string   `json:"isbn"`
	Language     string   `json:"language"`
	PageCount    int      `json:"page_count"`
	Categories   []string `json:"categories"`
	CoverURL     string   `json:"cover_url,omitempty"`
}

type Service struct {
	client       *http.Client
	googleURL    string
	openLibraryURL string
}

func NewService() *Service {
	return &Service{
		client: &http.Client{
			Timeout: 6 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		googleURL:      "https://www.googleapis.com/books/v1/volumes",
		openLibraryURL: "https://openlibrary.org/search.json",
	}
}

func (s *Service) Search(ctx context.Context, query string) ([]Result, []string, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 200 {
		return nil, nil, errors.New("invalid search query")
	}

	type providerResponse struct {
		results  []Result
		provider string
		err      error
	}
	responses := make(chan providerResponse, 2)
	go func() {
		results, err := s.searchGoogle(ctx, query)
		responses <- providerResponse{results: results, provider: "Google Books", err: err}
	}()
	go func() {
		results, err := s.searchOpenLibrary(ctx, query)
		responses <- providerResponse{results: results, provider: "Open Library", err: err}
	}()

	results := make([]Result, 0, maxResultsPerProvider*2)
	warnings := make([]string, 0, 2)
	for range 2 {
		response := <-responses
		if response.err != nil {
			warnings = append(warnings, response.provider)
			continue
		}
		results = append(results, response.results...)
	}
	if len(results) == 0 && len(warnings) == 2 {
		return nil, warnings, ErrUnavailable
	}
	return results, warnings, nil
}

func (s *Service) DownloadCover(ctx context.Context, provider, rawURL string) ([]byte, error) {
	coverURL, err := validateCoverURL(provider, rawURL)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, coverURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create cover request: %w", err)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download book cover: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download book cover: unexpected status %d", response.StatusCode)
	}
	if response.ContentLength > maxCoverSize {
		return nil, errors.New("book cover exceeds the size limit")
	}
	image, err := io.ReadAll(io.LimitReader(response.Body, maxCoverSize+1))
	if err != nil {
		return nil, fmt.Errorf("read book cover: %w", err)
	}
	if len(image) == 0 || len(image) > maxCoverSize {
		return nil, errors.New("invalid book cover size")
	}
	contentType := http.DetectContentType(image)
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/webp" {
		return nil, errors.New("unsupported book cover image")
	}
	return image, nil
}

func (s *Service) searchGoogle(ctx context.Context, query string) ([]Result, error) {
	endpoint, err := url.Parse(s.googleURL)
	if err != nil {
		return nil, err
	}
	parameters := endpoint.Query()
	parameters.Set("q", query)
	parameters.Set("maxResults", strconv.Itoa(maxResultsPerProvider))
	endpoint.RawQuery = parameters.Encode()

	var response struct {
		Items []struct {
			ID         string `json:"id"`
			VolumeInfo struct {
				Title               string   `json:"title"`
				Authors             []string `json:"authors"`
				Description         string   `json:"description"`
				Publisher           string   `json:"publisher"`
				PublishedDate       string   `json:"publishedDate"`
				PageCount           int      `json:"pageCount"`
				Categories          []string `json:"categories"`
				Language            string   `json:"language"`
				IndustryIdentifiers []struct {
					Type       string `json:"type"`
					Identifier string `json:"identifier"`
				} `json:"industryIdentifiers"`
				ImageLinks struct {
					Thumbnail string `json:"thumbnail"`
				} `json:"imageLinks"`
			} `json:"volumeInfo"`
		} `json:"items"`
	}
	if err := s.getJSON(ctx, endpoint.String(), &response); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(response.Items))
	for _, item := range response.Items {
		info := item.VolumeInfo
		if strings.TrimSpace(info.Title) == "" || item.ID == "" {
			continue
		}
		isbn := ""
		for _, identifier := range info.IndustryIdentifiers {
			if identifier.Type == "ISBN_13" {
				isbn = identifier.Identifier
				break
			}
			if identifier.Type == "ISBN_10" && isbn == "" {
				isbn = identifier.Identifier
			}
		}
		coverURL := strings.Replace(info.ImageLinks.Thumbnail, "http://", "https://", 1)
		results = append(results, Result{
			Provider: "google_books", ID: item.ID, Title: info.Title,
			Author: strings.Join(info.Authors, ", "), Description: info.Description,
			Publisher: info.Publisher, PublishedDate: info.PublishedDate,
			ISBN: isbn, Language: info.Language, PageCount: info.PageCount,
			Categories: info.Categories, CoverURL: coverURL,
		})
	}
	return results, nil
}

func (s *Service) searchOpenLibrary(ctx context.Context, query string) ([]Result, error) {
	endpoint, err := url.Parse(s.openLibraryURL)
	if err != nil {
		return nil, err
	}
	parameters := endpoint.Query()
	parameters.Set("title", query)
	parameters.Set("limit", strconv.Itoa(maxResultsPerProvider))
	parameters.Set("fields", "key,title,author_name,first_publish_year,publisher,isbn,language,number_of_pages_median,subject,cover_i")
	endpoint.RawQuery = parameters.Encode()

	var response struct {
		Docs []struct {
			Key                    string   `json:"key"`
			Title                  string   `json:"title"`
			Authors                []string `json:"author_name"`
			FirstPublishYear       int      `json:"first_publish_year"`
			Publishers             []string `json:"publisher"`
			ISBNs                  []string `json:"isbn"`
			Languages              []string `json:"language"`
			PageCount              int      `json:"number_of_pages_median"`
			Categories             []string `json:"subject"`
			CoverID                int      `json:"cover_i"`
		} `json:"docs"`
	}
	if err := s.getJSON(ctx, endpoint.String(), &response); err != nil {
		return nil, err
	}
	results := make([]Result, 0, len(response.Docs))
	for _, doc := range response.Docs {
		if strings.TrimSpace(doc.Title) == "" || doc.Key == "" {
			continue
		}
		coverURL := ""
		if doc.CoverID > 0 {
			coverURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", doc.CoverID)
		}
		publishedDate := ""
		if doc.FirstPublishYear > 0 {
			publishedDate = strconv.Itoa(doc.FirstPublishYear)
		}
		results = append(results, Result{
			Provider: "open_library", ID: doc.Key, Title: doc.Title,
			Author: strings.Join(doc.Authors, ", "), Publisher: first(doc.Publishers),
			PublishedDate: publishedDate, ISBN: first(doc.ISBNs),
			Language: first(doc.Languages), PageCount: doc.PageCount,
			Categories: doc.Categories, CoverURL: coverURL,
		})
	}
	return results, nil
}

func (s *Service) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create metadata request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	response, err := s.client.Do(request)
	if err != nil {
		return fmt.Errorf("request book metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("request book metadata: unexpected status %d", response.StatusCode)
	}
	if response.ContentLength > maxResponseSize {
		return errors.New("metadata response exceeds the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxResponseSize+1))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode book metadata: %w", err)
	}
	return nil
}

func validateCoverURL(provider, rawURL string) (*url.URL, error) {
	coverURL, err := url.Parse(rawURL)
	if err != nil || coverURL.Scheme != "https" || coverURL.User != nil || coverURL.Port() != "" {
		return nil, errors.New("invalid book cover URL")
	}
	host := strings.ToLower(coverURL.Hostname())
	switch host {
	case "books.google.com", "books.googleusercontent.com":
		if provider != "google_books" || coverURL.Path != "/books/content" {
			return nil, errors.New("invalid Google Books cover URL")
		}
	case "covers.openlibrary.org":
		if provider != "open_library" || !strings.HasPrefix(coverURL.Path, "/b/id/") || !strings.HasSuffix(coverURL.Path, ".jpg") {
			return nil, errors.New("invalid Open Library cover URL")
		}
		for _, r := range strings.TrimSuffix(strings.TrimPrefix(coverURL.Path, "/b/id/"), ".jpg") {
			if r < '0' || r > '9' {
				return nil, errors.New("invalid Open Library cover URL")
			}
		}
	default:
		return nil, errors.New("unsupported book cover host")
	}
	if net.ParseIP(host) != nil {
		return nil, errors.New("invalid book cover host")
	}
	return coverURL, nil
}

func first(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
