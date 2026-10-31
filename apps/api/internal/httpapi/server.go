// Package httpapi is the API's core: the Server and its dependencies, the
// middleware every route runs through, JSON answers, rate limits, sessions and
// second factors. Controllers live in endpoints/<area>, one package per area,
// and router registers every route. Areas import this package; it imports
// none of them.
package httpapi

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/store"
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

// DummyHash is a password hash no account has, checked when an email is
// unknown, so the answer takes as long as for a real account.
func (s *Server) DummyHash() string {
	return s.dummyHash
}
