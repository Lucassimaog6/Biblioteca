package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Lucassimaog6/Biblioteca/internal/auth"
	"github.com/Lucassimaog6/Biblioteca/internal/books"
	"github.com/Lucassimaog6/Biblioteca/internal/httpapi"
	"github.com/Lucassimaog6/Biblioteca/internal/storage"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	address := envOrDefault("BIBLIOTECA_ADDR", ":8080")
	databasePath := envOrDefault("BIBLIOTECA_DB_PATH", "data/biblioteca.db")
	booksDirectory := envOrDefault("BIBLIOTECA_BOOKS_DIR", "data/books")
	secureCookies := os.Getenv("BIBLIOTECA_COOKIE_SECURE") == "1"

	store, err := storage.Open(context.Background(), databasePath)
	if err != nil {
		return err
	}
	defer store.Close()

	bookService, err := books.NewService(store, booksDirectory)
	if err != nil {
		return err
	}
	handler, err := httpapi.New(auth.NewService(store), bookService, secureCookies)
	if err != nil {
		return fmt.Errorf("configure HTTP handler: %w", err)
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("Biblioteca listening on %s", address)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
