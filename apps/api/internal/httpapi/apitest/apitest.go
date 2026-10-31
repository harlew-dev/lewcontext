// Package apitest holds the helpers the API's integration tests share:
// a migrated database, requests sent as the web app sends them, signed-in
// users and their workspaces, and lookups. Only tests import it. It imports
// neither httpapi nor its router, so tests inside either can use it.
package apitest

import (
	"context"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Origin is the web app's origin in every test request and Config.
const Origin = "http://localhost:3000"

// UserAgent is the User-Agent of every test request.
const UserAgent = "lewcontext-integration-test/1.0"

// SessionResponse is the session in an answer shaped like /api/auth/me.
type SessionResponse struct {
	User store.User `json:"user"`
	CSRF string     `json:"csrfToken"`
}

// Database is a migrated database in a schema of its own, checking that a
// second migrate does nothing. Existing application data is never reset.
func Database(t *testing.T) *bun.DB {
	t.Helper()
	db := dbtest.Open(t)
	ctx := context.Background()
	Must(database.Migrate(ctx, db))(t)
	if group, err := database.Migrate(ctx, db); err != nil || !group.IsZero() {
		t.Fatal("migration is not idempotent", group, err)
	}
	return db
}

// Config is the configuration integration tests start a server with: the
// test origin, the factories' APP_KEY and admin on (factory.AdminRouteKey).
func Config() config.Config {
	return config.Config{Origin: Origin, AppKey: factory.AppKey, AdminRouteKey: factory.AdminRouteKey}
}

// Send makes a request as the web app would and returns the response. It
// doesn't fail the test, so goroutines can use it.
func Send(h http.Handler, method, path, body, csrf string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", Origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("User-Agent", UserAgent)
	r.RemoteAddr = "127.0.0.1:1234"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Call is Send, failing the test unless the status is want.
func Call(t *testing.T, h http.Handler, method, path, body, csrf string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
	t.Helper()
	w := Send(h, method, path, body, csrf, cookie)
	if w.Code != want {
		t.Fatalf("%s %s: want %d got %d: %s", method, path, want, w.Code, w.Body.String())
	}
	return w
}

// SignedIn creates a fake user with a live session, without the rate-limited
// auth endpoints. The session carries the cookie and CSRF token.
func SignedIn(t *testing.T, db bun.IDB, f *gofakeit.Faker) factory.Session {
	t.Helper()
	user := Must(factory.Users.CreateOne(context.Background(), db, f))(t)
	return Must(factory.SessionFor(context.Background(), db, f, user.ID))(t)
}

// Must returns v, or fails the test with err. Go only lets a two-result call
// fill a function's whole argument list, so it takes the call's results and
// returns a function of t:
//
//	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
func Must[T any](v T, err error) func(testing.TB) T {
	return func(t testing.TB) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// IDOf is the internal ID of the record in table ("users", "workspaces",
// "projects") with publicID.
func IDOf(t *testing.T, db bun.IDB, table, publicID string) int64 {
	t.Helper()
	var id int64
	if err := db.NewSelect().TableExpr("?", bun.Ident(table)).Column("id").Where("public_id = ?", publicID).Scan(context.Background(), &id); err != nil {
		t.Fatal(err)
	}
	return id
}

// EmailOf is the user's email address.
func EmailOf(t *testing.T, db bun.IDB, userID int64) string {
	t.Helper()
	var email string
	if err := db.NewSelect().TableExpr("users").Column("email").Where("id = ?", userID).Scan(context.Background(), &email); err != nil {
		t.Fatal(err)
	}
	return email
}

// OwnedWorkspace creates a workspace owned by userID.
func OwnedWorkspace(t *testing.T, db bun.IDB, f *gofakeit.Faker, userID int64) factory.Workspace {
	t.Helper()
	return Must(factory.WorkspaceFor(context.Background(), db, f, userID))(t)
}

// WorkspacePath is a path inside a workspace, e.g. WorkspacePath(id, "/projects").
func WorkspacePath(publicID, rest string) string {
	return "/api/workspaces/" + publicID + rest
}
