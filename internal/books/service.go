package books

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

var (
	ErrInvalidBook  = errors.New("invalid book")
	ErrFileTooLarge = errors.New("book file exceeds the upload limit")
)

const MaxUploadSize int64 = 100 << 20

type Metadata struct {
	Title         string
	Author        string
	Description   string
	Publisher     string
	PublishedDate string
	ISBN          string
	Language      string
	PageCount     int
	Categories    []string
	MetadataSource string
	MetadataID    string
	CoverURL      string
	CoverImage    []byte
	CoverChanged  bool
}

type Service struct {
	store *storage.Store
	dir   string
}

func NewService(store *storage.Store, directory string) (*Service, error) {
	absoluteDir, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve book storage directory: %w", err)
	}
	if err := os.MkdirAll(absoluteDir, 0700); err != nil {
		return nil, fmt.Errorf("create book storage directory: %w", err)
	}
	return &Service{store: store, dir: absoluteDir}, nil
}

func (s *Service) List(ctx context.Context, viewerID int64) ([]storage.Book, error) {
	return s.store.ListBooks(ctx, viewerID)
}

func (s *Service) Get(ctx context.Context, id, viewerID int64) (storage.Book, error) {
	book, err := s.store.FindBook(ctx, id)
	if err != nil {
		return storage.Book{}, err
	}
	book.CanEdit = book.OwnerID == viewerID
	return book, nil
}

func (s *Service) OpenFile(ctx context.Context, id int64) (storage.Book, *os.File, error) {
	book, err := s.store.FindBook(ctx, id)
	if err != nil {
		return storage.Book{}, nil, err
	}
	path, err := s.filePath(book.FileKey)
	if err != nil {
		return storage.Book{}, nil, fmt.Errorf("resolve stored book file: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return storage.Book{}, nil, fmt.Errorf("open stored book file: %w", err)
	}
	return book, file, nil
}

func (s *Service) OpenCover(ctx context.Context, id int64) (storage.Book, *os.File, string, error) {
	book, err := s.store.FindBook(ctx, id)
	if err != nil {
		return storage.Book{}, nil, "", err
	}
	if book.CoverKey == "" {
		return storage.Book{}, nil, "", os.ErrNotExist
	}
	path, err := s.coverPath(book.CoverKey)
	if err != nil {
		return storage.Book{}, nil, "", fmt.Errorf("resolve stored book cover: %w", err)
	}
	file, err := os.Open(path)
	if err != nil {
		return storage.Book{}, nil, "", fmt.Errorf("open stored book cover: %w", err)
	}
	contentType := "image/jpeg"
	switch filepath.Ext(book.CoverKey) {
	case ".png":
		contentType = "image/png"
	case ".webp":
		contentType = "image/webp"
	}
	return book, file, contentType, nil
}

func (s *Service) Create(ctx context.Context, ownerID int64, metadata Metadata, filename string, source io.Reader) (storage.Book, error) {
	metadata, err := validateMetadata(metadata)
	if err != nil {
		return storage.Book{}, err
	}
	filename, err = sanitizeFilename(filename)
	if err != nil {
		return storage.Book{}, err
	}
	key, size, err := s.writeFile(source)
	if err != nil {
		return storage.Book{}, err
	}
	coverKey := ""
	if metadata.CoverChanged && len(metadata.CoverImage) > 0 {
		coverKey, err = s.writeCover(metadata.CoverImage)
		if err != nil {
			s.removeFile(key)
			return storage.Book{}, err
		}
	}
	book, err := s.store.CreateBook(ctx, storage.Book{
		OwnerID: ownerID, Title: metadata.Title, Author: metadata.Author, Description: metadata.Description,
		Publisher: metadata.Publisher, PublishedDate: metadata.PublishedDate, ISBN: metadata.ISBN,
		Language: metadata.Language, PageCount: metadata.PageCount, Categories: metadata.Categories,
		MetadataSource: metadata.MetadataSource, MetadataID: metadata.MetadataID,
		Filename: filename, FileKey: key, CoverKey: coverKey, FileSize: size,
	})
	if err != nil {
		s.removeFile(key)
		s.removeCover(coverKey)
		return storage.Book{}, err
	}
	book.CanEdit = true
	return book, nil
}

func (s *Service) Update(ctx context.Context, id, ownerID int64, metadata Metadata, filename string, source io.Reader) (storage.Book, error) {
	metadata, err := validateMetadata(metadata)
	if err != nil {
		return storage.Book{}, err
	}
	book, err := s.store.FindBookForEdit(ctx, id, ownerID)
	if err != nil {
		return storage.Book{}, err
	}
	expectedFileKey := book.FileKey
	replacementKey := ""
	expectedCoverKey := book.CoverKey
	replacementCoverKey := book.CoverKey
	if source != nil {
		filename, err = sanitizeFilename(filename)
		if err != nil {
			return storage.Book{}, err
		}
		replacementKey, book.FileSize, err = s.writeFile(source)
		if err != nil {
			return storage.Book{}, err
		}
		book.Filename = filename
		book.FileKey = replacementKey
	}
	if metadata.CoverChanged {
		replacementCoverKey = ""
		if len(metadata.CoverImage) > 0 {
			replacementCoverKey, err = s.writeCover(metadata.CoverImage)
			if err != nil {
				if replacementKey != "" {
					s.removeFile(replacementKey)
				}
				return storage.Book{}, err
			}
		}
		book.CoverKey = replacementCoverKey
	}
	book.Title = metadata.Title
	book.Author = metadata.Author
	book.Description = metadata.Description
	book.Publisher = metadata.Publisher
	book.PublishedDate = metadata.PublishedDate
	book.ISBN = metadata.ISBN
	book.Language = metadata.Language
	book.PageCount = metadata.PageCount
	book.Categories = metadata.Categories
	book.MetadataSource = metadata.MetadataSource
	book.MetadataID = metadata.MetadataID
	updated, err := s.store.UpdateBook(ctx, book, expectedFileKey)
	if err != nil {
		if replacementKey != "" {
			s.removeFile(replacementKey)
		}
		if replacementCoverKey != expectedCoverKey {
			s.removeCover(replacementCoverKey)
		}
		return storage.Book{}, err
	}
	if replacementKey != "" {
		s.removeFile(expectedFileKey)
	}
	if metadata.CoverChanged && expectedCoverKey != "" && expectedCoverKey != replacementCoverKey {
		s.removeCover(expectedCoverKey)
	}
	updated.CanEdit = true
	return updated, nil
}

func validateMetadata(metadata Metadata) (Metadata, error) {
	metadata.Title = strings.TrimSpace(metadata.Title)
	metadata.Author = strings.TrimSpace(metadata.Author)
	metadata.Description = strings.TrimSpace(metadata.Description)
	metadata.Publisher = strings.TrimSpace(metadata.Publisher)
	metadata.PublishedDate = strings.TrimSpace(metadata.PublishedDate)
	metadata.ISBN = strings.TrimSpace(metadata.ISBN)
	metadata.Language = strings.TrimSpace(metadata.Language)
	metadata.MetadataSource = strings.TrimSpace(metadata.MetadataSource)
	metadata.MetadataID = strings.TrimSpace(metadata.MetadataID)
	for index := range metadata.Categories {
		metadata.Categories[index] = strings.TrimSpace(metadata.Categories[index])
	}
	if !utf8.ValidString(metadata.Title) || !utf8.ValidString(metadata.Author) || !utf8.ValidString(metadata.Description) ||
		len(metadata.Title) == 0 || len(metadata.Title) > 250 ||
		len(metadata.Author) > 250 || len(metadata.Description) > 5000 ||
		len(metadata.Publisher) > 250 || len(metadata.PublishedDate) > 50 ||
		len(metadata.ISBN) > 32 || len(metadata.Language) > 50 ||
		metadata.PageCount < 0 || metadata.PageCount > 100000 ||
		len(metadata.MetadataSource) > 32 || len(metadata.MetadataID) > 500 ||
		len(metadata.CoverURL) > 2048 ||
		len(metadata.Categories) > 50 {
		return Metadata{}, ErrInvalidBook
	}
	for _, value := range []string{metadata.Publisher, metadata.PublishedDate, metadata.ISBN, metadata.Language, metadata.MetadataSource, metadata.MetadataID, metadata.CoverURL} {
		if !utf8.ValidString(value) {
			return Metadata{}, ErrInvalidBook
		}
	}
	for _, category := range metadata.Categories {
		if !utf8.ValidString(category) || len(category) > 100 {
			return Metadata{}, ErrInvalidBook
		}
	}
	if len(metadata.CoverImage) > 5<<20 {
		return Metadata{}, ErrInvalidBook
	}
	return metadata, nil
}

func sanitizeFilename(filename string) (string, error) {
	filename = strings.TrimSpace(filepath.Base(strings.ReplaceAll(filename, `\`, "/")))
	if filename == "" || filename == "." || filename == ".." || !utf8.ValidString(filename) || len(filename) > 255 {
		return "", ErrInvalidBook
	}
	return filename, nil
}

func (s *Service) writeFile(source io.Reader) (string, int64, error) {
	if source == nil {
		return "", 0, ErrInvalidBook
	}
	temporary, err := os.CreateTemp(s.dir, ".upload-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temporary book file: %w", err)
	}
	temporaryName := temporary.Name()
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = os.Remove(temporaryName)
	}()

	size, err := io.Copy(temporary, io.LimitReader(source, MaxUploadSize+1))
	if err != nil {
		return "", 0, fmt.Errorf("write temporary book file: %w", err)
	}
	if size == 0 {
		return "", 0, ErrInvalidBook
	}
	if size > MaxUploadSize {
		return "", 0, ErrFileTooLarge
	}
	if err := temporary.Sync(); err != nil {
		return "", 0, fmt.Errorf("sync temporary book file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return "", 0, fmt.Errorf("close temporary book file: %w", err)
	}
	closed = true

	var randomKey [16]byte
	if _, err := rand.Read(randomKey[:]); err != nil {
		return "", 0, fmt.Errorf("generate book file key: %w", err)
	}
	key := hex.EncodeToString(randomKey[:]) + ".book"
	destination, err := s.filePath(key)
	if err != nil {
		return "", 0, err
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return "", 0, fmt.Errorf("store book file: %w", err)
	}
	return key, size, nil
}

func (s *Service) removeFile(key string) {
	path, err := s.filePath(key)
	if err == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove replaced book file: %v", err)
		}
	}
}

func (s *Service) writeCover(image []byte) (string, error) {
	if len(image) == 0 || len(image) > 5<<20 {
		return "", ErrInvalidBook
	}
	extension := ""
	switch contentType := http.DetectContentType(image); contentType {
	case "image/jpeg":
		extension = ".jpg"
	case "image/png":
		extension = ".png"
	case "image/webp":
		extension = ".webp"
	default:
		return "", ErrInvalidBook
	}
	var randomKey [16]byte
	if _, err := rand.Read(randomKey[:]); err != nil {
		return "", fmt.Errorf("generate book cover key: %w", err)
	}
	key := hex.EncodeToString(randomKey[:]) + extension
	path, err := s.coverPath(key)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, image, 0600); err != nil {
		return "", fmt.Errorf("store book cover: %w", err)
	}
	return key, nil
}

func (s *Service) removeCover(key string) {
	path, err := s.coverPath(key)
	if err == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("remove replaced book cover: %v", err)
		}
	}
}

func (s *Service) filePath(key string) (string, error) {
	if len(key) != 37 || !strings.HasSuffix(key, ".book") {
		return "", ErrInvalidBook
	}

	func (s *Service) coverPath(key string) (string, error) {
		if len(key) < 36 || len(key) > 37 {
			return "", ErrInvalidBook
		}
		if _, err := hex.DecodeString(key[:32]); err != nil {
			return "", ErrInvalidBook
		}
		switch key[32:] {
		case ".jpg", ".png", ".webp":
		default:
			return "", ErrInvalidBook
		}
		path := filepath.Join(s.dir, key)
		relative, err := filepath.Rel(s.dir, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", ErrInvalidBook
		}
		return path, nil
	}
	if _, err := hex.DecodeString(key[:32]); err != nil {
		return "", ErrInvalidBook
	}
	path := filepath.Join(s.dir, key)
	relative, err := filepath.Rel(s.dir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidBook
	}
	return path, nil
}
