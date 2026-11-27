package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesExistingBooksTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`
		CREATE TABLE books (
			id INTEGER PRIMARY KEY,
			owner_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			author TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL,
			file_key TEXT NOT NULL,
			file_size INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO books (id, owner_id, title, filename, file_key, file_size, created_at, updated_at)
		VALUES (1, 1, 'Legacy book', 'legacy.epub', '0123456789abcdef0123456789abcdef.book', 10,
			'2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	defer store.Close()
	book, err := store.FindBook(context.Background(), 1)
	if err != nil {
		t.Fatalf("read migrated book: %v", err)
	}
	if book.Title != "Legacy book" || book.Publisher != "" || book.CoverKey != "" || book.HasCover {
		t.Fatalf("unexpected migrated book: %+v", book)
	}
}
