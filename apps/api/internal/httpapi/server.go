package httpapi

import (
	"encoding/json"
	"errors"
	"forma/api/internal/auth"
	"forma/api/internal/config"
	"forma/api/internal/store"
	"io"
	"log/slog"
	"net/http"
)

type Server struct {
	Store  *store.Store
	Config config.Config
	// Pages serves the web app (internal/web); nil serves the API only.
	Pages     http.Handler
	dummyHash string
	authSlots chan struct{}
}

func New(s *store.Store, c config.Config) (*Server, error) {
	dummy, err := auth.HashPassword("timing-comparison-placeholder-not-an-account")
	if err != nil {
		return nil, err
	}
	return &Server{Store: s, Config: c, dummyHash: dummy, authSlots: make(chan struct{}, 4)}, nil
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}
func internalError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	fail(w, 500, "Something went wrong. Please try again.")
}
func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		fail(w, 400, "Invalid request body.")
		return false
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		fail(w, 400, "Expected one JSON object.")
		return false
	}
	return true
}
