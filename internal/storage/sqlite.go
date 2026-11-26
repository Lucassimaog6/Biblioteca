package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

var ErrEmailExists = errors.New("email already exists")

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
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
	`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("initialize SQLite schema: %w", err)
	}
	return nil
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
