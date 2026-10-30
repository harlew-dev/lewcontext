package main

import (
	"context"
	"errors"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"lewcontext/api/internal/web"
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
	// Listeners register on the bus here, before anything records an event:
	//   events.Listen(bus, "mail.password_changed", mail.PasswordChanged)
	bus := events.NewBus()
	repository := &store.Store{DB: db, Listeners: bus}
	api, err := httpapi.New(repository, c)
	if err != nil {
		return err
	}
	if err := servePages(api, c); err != nil {
		return err
	}
	server := &http.Server{Addr: c.Address, Handler: api.Routes(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
	failures := make(chan error, 1)
	go func() {
		slog.Info("listening", "address", c.Address, "pages", api.Pages != nil)
		failures <- server.ListenAndServe()
	}()
	// Delivers recorded events to their listeners until shutdown.
	worker := &events.Worker{Bus: bus, Queue: repository}
	delivering := make(chan struct{})
	go func() {
		defer close(delivering)
		worker.Run(ctx)
	}()
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
			err := server.Shutdown(shutdown)
			// Let the worker finish its batch; anything unfinished is
			// retried when its lease ends.
			select {
			case <-delivering:
			case <-shutdown.Done():
			}
			return err
		case err := <-failures:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	}
}

// servePages adds the embedded web build. Production requires it; in
// development the API can run alone while Vite serves the pages (make web).
func servePages(api *httpapi.Server, c config.Config) error {
	pages, err := web.New(web.Files(), c.Production)
	if errors.Is(err, web.ErrNotBuilt) && !c.Production {
		slog.Info("serving the API only; the web app isn't built into this binary (make serve)")
		return nil
	}
	if err != nil {
		return err
	}
	api.Pages = pages
	return nil
}
