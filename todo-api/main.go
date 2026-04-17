package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flow-verify-round2/todo-api/internal/handlers"
	"github.com/flow-verify-round2/todo-api/internal/middleware"
	"github.com/flow-verify-round2/todo-api/internal/storage"
)

const defaultPort = "9090"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	store := storage.New()
	h := handlers.New(store)

	mux := http.NewServeMux()
	h.Register(mux)

	handler := middleware.Recover(middleware.Logging(mux))

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("http shutdown error: %v", err)
		}
		close(idleConnsClosed)
	}()

	log.Printf("todo-api listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server failed: %v", err)
	}

	<-idleConnsClosed
	log.Println("todo-api stopped")
}
