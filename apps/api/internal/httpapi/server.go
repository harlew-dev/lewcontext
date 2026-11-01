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
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"net/http"
	"sync/atomic"
)

type Server struct {
	// Repo is the database: every query and event the API runs is a method on
	// it (internal/store), and controllers reach it as h.Repo. Named Repo, not
	// Store, so it doesn't clash with a controller's Store action.
	Repo   *store.Store
	Config config.Config
	// Pages serves the web app (internal/web); nil serves the API only.
	Pages     WebPages
	dummyHash string
	// keys seal MFA secrets, digest recovery codes and sign the admin
	// assets pass (APP_KEY).
	keys      *auth.Keys
	authSlots chan struct{}
	// memoryLimits counts the limits kept in memory (api-read:per-user).
	memoryLimits *ratelimit.Memory
	// adminRefusals records addresses outside ADMIN_ALLOWED_IPS, each once
	// per window, through a bounded queue and one worker (admin_access.go).
	adminRefusals *adminRefusals
	// Summaries bound the admin dashboard's aggregates (summaries.go).
	Summaries *Summaries
	// ipBlocks are the live IP blocks, reloaded every IPBlocksReload, and
	// what they refused (ip_blocks.go).
	ipBlocks ipBlocks
	// draining is set once shutdown has begun (Drain): readiness then
	// answers 503.
	draining atomic.Bool
	// Metrics counts every request by route (observe.Requests): the
	// process's registry (observe.Metrics) unless a test sets its own; nil
	// counts nothing.
	Metrics *observe.Registry
}

// WebPages serves the built web app (web.Pages): its pages and files, and
// the admin build's shell and files once their key or pass is checked.
type WebPages interface {
	http.Handler
	AdminBuilt() bool
	ServeAdmin(w http.ResponseWriter, r *http.Request)
	ServeAdminFile(w http.ResponseWriter, r *http.Request, name string) bool
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
	server := &Server{Repo: s, Config: c, dummyHash: dummy, keys: keys, authSlots: make(chan struct{}, 4), memoryLimits: ratelimit.NewMemory(), adminRefusals: newAdminRefusals(), Summaries: newSummaries(), Metrics: observe.Metrics}
	go server.runAdminRefusals()
	return server, nil
}

// DummyHash is a password hash no account has, checked when an email is
// unknown, so the answer takes as long as for a real account.
func (s *Server) DummyHash() string {
	return s.dummyHash
}

// EmailPseudonym is what events keep in place of email once they're 30
// days old (auth.Keys.EmailPseudonym, keyed by APP_KEY): erasure and the
// admin Events search match old events by it.
func (s *Server) EmailPseudonym(email string) string {
	return s.keys.EmailPseudonym(email)
}
