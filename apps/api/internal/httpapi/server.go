package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
)

type Server struct {
	Repo   *store.Store
	Config config.Config
	// Pages serves the web app (internal/web); nil serves the API only.
	Pages     WebPages
	dummyHash string
	// keys seal MFA secrets and digest recovery codes (APP_KEY).
	keys      *auth.Keys
	authSlots chan struct{}
	// memoryLimits counts the limits kept in memory (api-read:per-user).
	memoryLimits *ratelimit.Memory
}

// WebPages serves the built web app (web.Pages): its pages and files, and
// the app shell for admin pages once their key is checked.
type WebPages interface {
	http.Handler
	ServeAdmin(w http.ResponseWriter, r *http.Request)
}

func New(s *store.Store, c config.Config) (*Server, error) {
	dummy, err := auth.HashPassword("timing-comparison-placeholder-not-an-account")
	if err != nil {
		return nil, err
	}
	keys, err := auth.NewKeys(c.AppKey)
	if err != nil {
		return nil, err
	}
	return &Server{Repo: s, Config: c, dummyHash: dummy, keys: keys, authSlots: make(chan struct{}, 4), memoryLimits: ratelimit.NewMemory()}, nil
}
func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func Fail(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

// FailField is Fail for an error about one field of the request body, so the
// web app can show it next to that field.
func FailField(w http.ResponseWriter, status int, field, message string) {
	JSON(w, status, map[string]string{"error": message, "field": field})
}
func InternalError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	Fail(w, 500, "Something went wrong. Please try again.")
}

// DecodeValid decodes the body into a T and checks it with valid (which may
// also normalise it, e.g. trim), answering 400 or 422 with message when either
// fails.
func DecodeValid[T any](w http.ResponseWriter, r *http.Request, valid func(*T) bool, message string) (T, bool) {
	var in T
	if !Decode(w, r, &in) {
		return in, false
	}
	if !valid(&in) {
		Fail(w, 422, message)
		return in, false
	}
	return in, true
}
func Decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		Fail(w, 400, "Invalid request body.")
		return false
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		Fail(w, 400, "Expected one JSON object.")
		return false
	}
	return true
}
