package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrEmailExists        = storage.ErrEmailExists
	ErrInvalidCredentials = errors.New("invalid credentials")
)

const SessionLifetime = 7 * 24 * time.Hour

type Service struct {
	store *storage.Store
}

func NewService(store *storage.Store) *Service {
	return &Service{store: store}
}

func (s *Service) Register(ctx context.Context, email, password string) (storage.User, string, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return storage.User{}, "", err
	}
	if len(password) < 8 || len(password) > 72 {
		return storage.User{}, "", ErrInvalidInput
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return storage.User{}, "", fmt.Errorf("hash password: %w", err)
	}
	user, err := s.store.CreateUser(ctx, email, string(passwordHash))
	if err != nil {
		return storage.User{}, "", err
	}
	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		return storage.User{}, "", err
	}
	return user, token, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (storage.User, string, error) {
	email, err := normalizeEmail(email)
	if err != nil || len(password) > 72 {
		return storage.User{}, "", ErrInvalidCredentials
	}
	user, passwordHash, err := s.store.FindUserByEmail(ctx, email)
	if err != nil {
		return storage.User{}, "", ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return storage.User{}, "", ErrInvalidCredentials
	}
	token, err := s.createSession(ctx, user.ID)
	if err != nil {
		return storage.User{}, "", err
	}
	return user, token, nil
}

func (s *Service) UserForToken(ctx context.Context, token string) (storage.User, error) {
	if token == "" || len(token) > 128 {
		return storage.User{}, ErrInvalidCredentials
	}
	return s.store.FindSessionUser(ctx, hashToken(token), time.Now())
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" || len(token) > 128 {
		return nil
	}
	return s.store.DeleteSession(ctx, hashToken(token))
}

func (s *Service) createSession(ctx context.Context, userID int64) (string, error) {
	randomToken := make([]byte, 32)
	if _, err := rand.Read(randomToken); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(randomToken)
	if err := s.store.CreateSession(ctx, hashToken(token), userID, time.Now().Add(SessionLifetime)); err != nil {
		return "", err
	}
	return token, nil
}

func normalizeEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 254 {
		return "", ErrInvalidInput
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", ErrInvalidInput
	}
	return email, nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func constantTimeEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
