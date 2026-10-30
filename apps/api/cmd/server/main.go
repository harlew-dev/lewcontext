package main

import (
	"context"
	"errors"
	"forma/api/internal/config"
	"forma/api/internal/database"
	"forma/api/internal/httpapi"
	"forma/api/internal/store"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := database.Open(startup, c.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	repository := &store.Store{DB: db}
	api, err := httpapi.New(repository, c)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: c.Address, Handler: api.Routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	failures := make(chan error, 1)
	go func() { slog.Info("API listening", "address", c.Address); failures <- server.ListenAndServe() }()
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			cleanup, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := repository.Cleanup(cleanup)
			cancel()
			if err != nil {
				slog.Error("session cleanup failed", "error", err)
			}
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return server.Shutdown(shutdown)
		case err := <-failures:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	}
}
