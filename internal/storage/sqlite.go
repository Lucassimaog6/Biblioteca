package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var (
	ErrEmailExists  = errors.New("email already exists")
	ErrBookNotFound = errors.New("book not found")
	ErrBookNotOwned = errors.New("book is not owned by user")
	ErrBookChanged  = errors.New("book changed during update")
)

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

type Book struct {
	ID            int64     `json:"id"`
	Title         string    `json:"title"`
	Author        string    `json:"author"`
	Description   string    `json:"description"`
	Publisher     string    `json:"publisher"`
	PublishedDate string    `json:"published_date"`
	ISBN          string    `json:"isbn"`
	Language      string    `json:"language"`
	PageCount     int       `json:"page_count"`
	Categories    []string  `json:"categories"`
	MetadataSource string   `json:"metadata_source"`
	MetadataID    string    `json:"metadata_id"`
	Filename      string    `json:"filename"`
	FileSize      int64     `json:"file_size"`
	HasCover      bool      `json:"has_cover"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	CanEdit       bool      `json:"can_edit"`
	OwnerID       int64     `json:"-"`
	FileKey       string    `json:"-"`
	CoverKey      string    `json:"-"`
}

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	databaseFile, err := os.OpenFile(absolutePath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create SQLite database file: %w", err)
	}
	if err := databaseFile.Close(); err != nil {
		return nil, fmt.Errorf("close SQLite database file: %w", err)
	}
	if err := os.Chmod(absolutePath, 0600); err != nil {
		return nil, fmt.Errorf("secure SQLite database file: %w", err)
	}

	databaseURL := url.URL{Scheme: "file", Path: filepath.ToSlash(absolutePath)}
	query := databaseURL.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	databaseURL.RawQuery = query.Encode()

	db, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to SQLite database: %w", err)
	}
	if err := store.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initialize(ctx context.Context) error {
	const schema = `
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS sessions (
			token_hash TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS sessions_expires_at ON sessions(expires_at);
		CREATE TABLE IF NOT EXISTS books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			owner_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			title TEXT NOT NULL,
			author TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			publisher TEXT NOT NULL DEFAULT '',
			published_date TEXT NOT NULL DEFAULT '',
			isbn TEXT NOT NULL DEFAULT '',
			language TEXT NOT NULL DEFAULT '',
			page_count INTEGER NOT NULL DEFAULT 0,
			categories TEXT NOT NULL DEFAULT '[]',
			metadata_source TEXT NOT NULL DEFAULT '',
			metadata_id TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL,
			file_key TEXT NOT NULL,
			cover_key TEXT NOT NULL DEFAULT '',
			file_size INTEGER NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		CREATE INDEX IF NOT EXISTS books_title ON books(title COLLATE NOCASE);
	`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("initialize SQLite schema: %w", err)
	}
	migrations := []struct {
		name       string
		definition string
	}{
		{"publisher", "TEXT NOT NULL DEFAULT ''"},
		{"published_date", "TEXT NOT NULL DEFAULT ''"},
		{"isbn", "TEXT NOT NULL DEFAULT ''"},
		{"language", "TEXT NOT NULL DEFAULT ''"},
		{"page_count", "INTEGER NOT NULL DEFAULT 0"},
		{"categories", "TEXT NOT NULL DEFAULT '[]'"},
		{"metadata_source", "TEXT NOT NULL DEFAULT ''"},
		{"metadata_id", "TEXT NOT NULL DEFAULT ''"},
		{"cover_key", "TEXT NOT NULL DEFAULT ''"},
	}
	for _, migration := range migrations {
		if err := s.addBookColumn(ctx, migration.name, migration.definition); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) addBookColumn(ctx context.Context, name, definition string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(books)`)
	if err != nil {
		return fmt.Errorf("inspect books schema: %w", err)
	}
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var columnName, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return fmt.Errorf("read books schema: %w", err)
		}
		if columnName == name {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate books schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close books schema: %w", err)
	}
	if found {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE books ADD COLUMN `+name+` `+definition); err != nil {
		return fmt.Errorf("add books column %s: %w", name, err)
	}
	return nil
}

func (s *Store) CreateBook(ctx context.Context, book Book) (Book, error) {
	now := time.Now().UTC()
	categories, err := json.Marshal(book.Categories)
	if err != nil {
		return Book{}, fmt.Errorf("encode book categories: %w", err)
	}
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO books (owner_id, title, author, description, publisher, published_date, isbn, language, page_count, categories,
		 metadata_source, metadata_id, filename, file_key, cover_key, file_size, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		book.OwnerID, book.Title, book.Author, book.Description, book.Publisher, book.PublishedDate, book.ISBN,
		book.Language, book.PageCount, string(categories), book.MetadataSource, book.MetadataID,
		book.Filename, book.FileKey, book.CoverKey, book.FileSize,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Book{}, fmt.Errorf("insert book: %w", err)
	}
	book.ID, err = result.LastInsertId()
	if err != nil {
		return Book{}, fmt.Errorf("get inserted book ID: %w", err)
	}
	book.CreatedAt = now
	book.UpdatedAt = now
	book.HasCover = book.CoverKey != ""
	return book, nil
}

func (s *Store) ListBooks(ctx context.Context, viewerID int64) ([]Book, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, owner_id, title, author, description, publisher, published_date, isbn, language, page_count, categories,
		 metadata_source, metadata_id, filename, file_key, cover_key, file_size, created_at, updated_at
		 FROM books ORDER BY title COLLATE NOCASE, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list books: %w", err)
	}
	defer rows.Close()

	books := make([]Book, 0)
	for rows.Next() {
		var book Book
		var createdAt, updatedAt, categories string
		if err := rows.Scan(
			&book.ID, &book.OwnerID, &book.Title, &book.Author, &book.Description,
			&book.Publisher, &book.PublishedDate, &book.ISBN, &book.Language, &book.PageCount, &categories,
			&book.MetadataSource, &book.MetadataID, &book.Filename, &book.FileKey, &book.CoverKey,
			&book.FileSize, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("read book row: %w", err)
		}
		if err := json.Unmarshal([]byte(categories), &book.Categories); err != nil {
			return nil, fmt.Errorf("decode book categories: %w", err)
		}
		book.HasCover = book.CoverKey != ""
		book.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			return nil, fmt.Errorf("parse book creation time: %w", err)
		}
		book.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse book update time: %w", err)
		}
		book.CanEdit = book.OwnerID == viewerID
		books = append(books, book)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate books: %w", err)
	}
	return books, nil
}

func (s *Store) FindBookForEdit(ctx context.Context, id, ownerID int64) (Book, error) {
	book, err := s.FindBook(ctx, id)
	if err != nil {
		return Book{}, err
	}
	if book.OwnerID != ownerID {
		return Book{}, ErrBookNotOwned
	}
	return book, nil
}

func (s *Store) FindBook(ctx context.Context, id int64) (Book, error) {
	var book Book
	var createdAt, updatedAt, categories string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, owner_id, title, author, description, publisher, published_date, isbn, language, page_count, categories,
		 metadata_source, metadata_id, filename, file_key, cover_key, file_size, created_at, updated_at
		 FROM books WHERE id = ?`,
		id,
	).Scan(
		&book.ID, &book.OwnerID, &book.Title, &book.Author, &book.Description,
		&book.Publisher, &book.PublishedDate, &book.ISBN, &book.Language, &book.PageCount, &categories,
		&book.MetadataSource, &book.MetadataID, &book.Filename, &book.FileKey, &book.CoverKey,
		&book.FileSize, &createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Book{}, ErrBookNotFound
	}
	if err != nil {
		return Book{}, fmt.Errorf("find book: %w", err)
	}
	if err := json.Unmarshal([]byte(categories), &book.Categories); err != nil {
		return Book{}, fmt.Errorf("decode book categories: %w", err)
	}
	book.HasCover = book.CoverKey != ""
	book.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Book{}, fmt.Errorf("parse book creation time: %w", err)
	}
	book.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Book{}, fmt.Errorf("parse book update time: %w", err)
	}
	return book, nil
}

func (s *Store) UpdateBook(ctx context.Context, book Book, expectedFileKey string) (Book, error) {
	now := time.Now().UTC()
	categories, err := json.Marshal(book.Categories)
	if err != nil {
		return Book{}, fmt.Errorf("encode book categories: %w", err)
	}
	result, err := s.db.ExecContext(ctx,
		`UPDATE books
		 SET title = ?, author = ?, description = ?, publisher = ?, published_date = ?, isbn = ?, language = ?,
		 page_count = ?, categories = ?, metadata_source = ?, metadata_id = ?, filename = ?, file_key = ?,
		 cover_key = ?, file_size = ?, updated_at = ?
		 WHERE id = ? AND owner_id = ? AND file_key = ?`,
		book.Title, book.Author, book.Description, book.Publisher, book.PublishedDate, book.ISBN, book.Language,
		book.PageCount, string(categories), book.MetadataSource, book.MetadataID, book.Filename, book.FileKey,
		book.CoverKey, book.FileSize, now.Format(time.RFC3339Nano), book.ID, book.OwnerID, expectedFileKey,
	)
	if err != nil {
		return Book{}, fmt.Errorf("update book: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Book{}, fmt.Errorf("check book update: %w", err)
	}
	if rowsAffected == 0 {
		return Book{}, ErrBookChanged
	}
	book.UpdatedAt = now
	book.HasCover = book.CoverKey != ""
	return book, nil
}

func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (User, error) {
	createdAt := time.Now().UTC()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO users (email, password_hash, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(email) DO NOTHING`,
		email, passwordHash, createdAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return User{}, fmt.Errorf("check user insert: %w", err)
	}
	if rowsAffected == 0 {
		return User{}, ErrEmailExists
	}
	id, err := result.LastInsertId()
	if err != nil {
		return User{}, fmt.Errorf("get inserted user ID: %w", err)
	}
	return User{ID: id, Email: email, CreatedAt: createdAt}, nil
}

func (s *Store) FindUserByEmail(ctx context.Context, email string) (User, string, error) {
	var user User
	var createdAt string
	var passwordHash string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`,
		email,
	).Scan(&user.ID, &user.Email, &passwordHash, &createdAt)
	if err != nil {
		return User{}, "", err
	}
	user.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return User{}, "", fmt.Errorf("parse user creation time: %w", err)
	}
	return user, passwordHash, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`,
		tokenHash, userID, expiresAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) FindSessionUser(ctx context.Context, tokenHash string, now time.Time) (User, error) {
	var user User
	var createdAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT users.id, users.email, users.created_at
		 FROM sessions
		 JOIN users ON users.id = sessions.user_id
		 WHERE sessions.token_hash = ? AND sessions.expires_at > ?`,
		tokenHash, now.Unix(),
	).Scan(&user.ID, &user.Email, &createdAt)
	if err != nil {
		return User{}, err
	}
	user.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return User{}, fmt.Errorf("parse user creation time: %w", err)
	}
	return user, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}
