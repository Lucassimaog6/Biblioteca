package auth

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

func TestRegisterLoginAndRevokeSession(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "users.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(store)

	user, token, err := service.Register(ctx, " Reader@Example.com ", "correct-horse")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.Email != "reader@example.com" {
		t.Fatalf("email = %q, want normalized address", user.Email)
	}
	if token == "" {
		t.Fatal("register returned an empty session token")
	}

	registeredUser, err := service.UserForToken(ctx, token)
	if err != nil {
		t.Fatalf("read session user: %v", err)
	}
	if registeredUser.ID != user.ID {
		t.Fatalf("session user ID = %d, want %d", registeredUser.ID, user.ID)
	}

	_, loginToken, err := service.Login(ctx, "READER@example.com", "correct-horse")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := service.UserForToken(ctx, loginToken); err != nil {
		t.Fatalf("read login session: %v", err)
	}

	if err := service.Logout(ctx, token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.UserForToken(ctx, token); err == nil {
		t.Fatal("revoked session still authenticates")
	}
}

func TestRegisterRejectsDuplicateAndInvalidAccounts(t *testing.T) {
	ctx := context.Background()
	store, err := storage.Open(ctx, filepath.Join(t.TempDir(), "users.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := NewService(store)

	if _, _, err := service.Register(ctx, "reader@example.com", "correct-horse"); err != nil {
		t.Fatalf("register first user: %v", err)
	}
	if _, _, err := service.Register(ctx, "READER@example.com", "another-password"); !errors.Is(err, ErrEmailExists) {
		t.Fatalf("duplicate registration error = %v, want ErrEmailExists", err)
	}
	if _, _, err := service.Register(ctx, "not-an-email", "correct-horse"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid email error = %v, want ErrInvalidInput", err)
	}
	if _, _, err := service.Register(ctx, "new@example.com", "short"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("short password error = %v, want ErrInvalidInput", err)
	}
	if _, _, err := service.Login(ctx, "reader@example.com", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("invalid login error = %v, want ErrInvalidCredentials", err)
	}
}
