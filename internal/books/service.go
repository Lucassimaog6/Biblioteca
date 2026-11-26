package books

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
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
	Title       string
	Author      string
	Description string
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
	book, err := s.store.CreateBook(ctx, storage.Book{
		OwnerID:     ownerID,
		Title:       metadata.Title,
		Author:      metadata.Author,
		Description: metadata.Description,
		Filename:    filename,
		FileKey:     key,
		FileSize:    size,
	})
	if err != nil {
		s.removeFile(key)
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
	book.Title = metadata.Title
	book.Author = metadata.Author
	book.Description = metadata.Description
	updated, err := s.store.UpdateBook(ctx, book, expectedFileKey)
	if err != nil {
		if replacementKey != "" {
			s.removeFile(replacementKey)
		}
		return storage.Book{}, err
	}
	if replacementKey != "" {
		s.removeFile(expectedFileKey)
	}
	updated.CanEdit = true
	return updated, nil
}

func validateMetadata(metadata Metadata) (Metadata, error) {
	metadata.Title = strings.TrimSpace(metadata.Title)
	metadata.Author = strings.TrimSpace(metadata.Author)
	metadata.Description = strings.TrimSpace(metadata.Description)
	if !utf8.ValidString(metadata.Title) || !utf8.ValidString(metadata.Author) || !utf8.ValidString(metadata.Description) ||
		len(metadata.Title) == 0 || len(metadata.Title) > 250 ||
		len(metadata.Author) > 250 || len(metadata.Description) > 5000 {
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

func (s *Service) filePath(key string) (string, error) {
	if len(key) != 37 || !strings.HasSuffix(key, ".book") {
		return "", ErrInvalidBook
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
