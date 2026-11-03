package httpapi

import (
	"context"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnonymousApiLimitsRecordOneCrossingForInFlightFailures(t *testing.T) {
	for _, limit := range []ratelimit.Limit{ApiKeyAuthPerIP, ApiKeyIngressPerIP} {
		t.Run(limit.Name, func(t *testing.T) {
			s := budgetServer(t)
			for range limit.Block.At() - 1 {
				s.apiAnonymous.Count(limit.IPKey("127.0.0.1"), limit.Window, true)
			}
			// Two failures may both pass the pre-auth check before either
			// completes. Count both completions: only one may emit the event.
			h := s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if s.anonymousApiLimit(w, r, limit, true) {
					w.WriteHeader(http.StatusOK)
				}
			}))
			apitest.Call(t, h, "GET", "/api/v1/example", "", "", nil, 200)
			refused := apitest.Call(t, h, "GET", "/api/v1/example", "", "", nil, 429)
			apitest.RetryWithin(t, refused, limit.Window)
			apitest.Reached(t, s.Repo.DB, limit.Name+":block")
		})
	}
}

// The in-memory budgets are tested through the middleware, since only this
// package can fill the counter (memoryLimits). Which routes are metered is
// the router's: its tests go through every route.

// ok answers 200 to anything that gets through.
var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })

// budgetServer is a Server on a migrated database of its own.
func budgetServer(t *testing.T) *Server {
	t.Helper()
	db := apitest.Database(t)
	return apitest.Must(New(&store.Store{DB: db}, apitest.Config()))(t)
}

// Reads are counted in memory, per process.
func TestReadBudget(t *testing.T) {
	s := budgetServer(t)
	db := s.Repo.DB
	h := s.ClientAddress(s.RequireSession(ok))
	f := factory.TestFaker(t)
	session, other := apitest.SignedIn(t, db, f), apitest.SignedIn(t, db, f)
	var publicID string
	apitest.Must(0, db.NewSelect().TableExpr("users").Column("public_id").Where("id = ?", session.UserID).Scan(context.Background(), &publicID))(t)
	for range 999 {
		s.memoryLimits.Count(APIReadPerUser.Key(publicID), APIReadPerUser.Window)
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 200)
	refused := apitest.Call(t, h, "GET", "/api/workspaces", "", "", session.Cookie(), 429)
	apitest.RetryWithin(t, refused, 5*time.Minute)
	apitest.Call(t, h, "GET", "/api/workspaces", "", "", session.Cookie(), 429)
	if e := apitest.Reached(t, db, "api-read:per-user:block"); e.Payload["user"] != publicID {
		t.Fatalf("event: %+v", e)
	}
	// Writes and other users keep their own budgets.
	apitest.Call(t, h, "POST", "/api/workspaces", "{}", session.CSRFToken, session.Cookie(), 200)
	apitest.Call(t, h, "GET", "/api/workspaces", "", "", other.Cookie(), 200)
	if n := apitest.Must(db.NewSelect().TableExpr("rate_limits").Where("key LIKE ?", "api-read:%").Count(context.Background()))(t); n != 0 {
		t.Fatalf("reads written to the database: %d", n)
	}
}

// The admin read budget is the admin's own, in memory.
func TestAdminReadBudget(t *testing.T) {
	s := budgetServer(t)
	db := s.Repo.DB
	h := s.ClientAddress(s.RequireAdminKey(s.RequireAdminSession(ok)))
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Admins.State(factory.AdminFor(admin.ID)).CreateOne(ctx, db, f))(t)
	session := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(admin.ID)).CreateOne(ctx, db, f))(t)
	for range 1000 {
		s.memoryLimits.Count(AdminReadPerUser.Key(admin.PublicID), AdminReadPerUser.Window)
	}
	r := httptest.NewRequest("GET", "/api/admin/users", nil)
	r.Header.Set("X-Admin-Key", factory.AdminRouteKey)
	r.RemoteAddr = "127.0.0.1:1234"
	r.AddCookie(session.Cookie())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 429 || w.Header().Get("Retry-After") == "" {
		t.Fatalf("over the budget: %d, Retry-After %q", w.Code, w.Header().Get("Retry-After"))
	}
	if e := apitest.OnlyEvent(t, db, "rate_limit.reached"); e.Payload["limit"] != "admin-read:per-user:block" || e.Payload["user"] != admin.PublicID {
		t.Fatalf("rate_limit.reached: %+v", e)
	}
}
