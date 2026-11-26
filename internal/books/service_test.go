package books

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

func TestCreateListAndUpdateBooks(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "books.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	directory := filepath.Join(t.TempDir(), "files")
	service, err := NewService(store, directory)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := store.CreateUser(ctx, "owner@example.com", "password hash")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateUser(ctx, "other@example.com", "password hash")
	if err != nil {
		t.Fatal(err)
	}

	book, err := service.Create(ctx, owner.ID, Metadata{
		Title: "  A Book  ", Author: " Author ", Description: " Description ",
	}, `C:\uploads\sample.epub`, strings.NewReader("original EPUB data"))
	if err != nil {
		t.Fatalf("create book: %v", err)
	}
	if book.Filename != "sample.epub" || !book.CanEdit || book.Title != "A Book" {
		t.Fatalf("unexpected created book: %+v", book)
	}
	list, err := service.List(ctx, other.ID)
	if err != nil {
		t.Fatalf("list books: %v", err)
	}
	if len(list) != 1 || list[0].CanEdit {
		t.Fatalf("other account should see the catalog but not own the book: %+v", list)
	}

	if _, err := service.Update(ctx, book.ID, other.ID, Metadata{Title: "Changed"}, "", nil); !errors.Is(err, storage.ErrBookNotOwned) {
		t.Fatalf("non-owner update error = %v, want ErrBookNotOwned", err)
	}

	oldPath := filepath.Join(directory, book.FileKey)
	updated, err := service.Update(ctx, book.ID, owner.ID, Metadata{
		Title: "Updated Book", Author: "New Author", Description: "New description",
	}, "new-book.pdf", strings.NewReader("replacement PDF data"))
	if err != nil {
		t.Fatalf("replace book: %v", err)
	}
	if updated.Filename != "new-book.pdf" || updated.Title != "Updated Book" || updated.FileSize != int64(len("replacement PDF data")) {
		t.Fatalf("unexpected updated book: %+v", updated)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old file should be removed after replacement, stat error = %v", err)
	}

	storedBook, file, err := service.OpenFile(ctx, book.ID)
	if err != nil {
		t.Fatalf("open replacement: %v", err)
	}
	defer file.Close()
	if storedBook.FileKey != updated.FileKey {
		t.Fatalf("open file key = %q, want %q", storedBook.FileKey, updated.FileKey)
	}
	contents, err := os.ReadFile(filepath.Join(directory, storedBook.FileKey))
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "replacement PDF data" {
		t.Fatalf("replacement contents = %q", contents)
	}
}

func TestCreateRejectsEmptyFileAndInvalidMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "books.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := NewService(store, filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser(ctx, "reader@example.com", "password hash")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(ctx, user.ID, Metadata{Title: "   "}, "book.epub", strings.NewReader("data")); !errors.Is(err, ErrInvalidBook) {
		t.Fatalf("empty title error = %v, want ErrInvalidBook", err)
	}
	if _, err := service.Create(ctx, user.ID, Metadata{Title: "Book"}, "book.epub", strings.NewReader("")); !errors.Is(err, ErrInvalidBook) {
		t.Fatalf("empty file error = %v, want ErrInvalidBook", err)
	}
}
