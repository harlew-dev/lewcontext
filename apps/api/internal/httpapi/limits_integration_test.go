package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// retryWithin fails unless the refusal's Retry-After is the time left in a
// window of the given length: at least a second, at most the whole window.
func retryWithin(t *testing.T, w *httptest.ResponseRecorder, window time.Duration) {
	t.Helper()
	seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || seconds < 1 || seconds > int(window.Seconds()) {
		t.Fatalf("Retry-After %q for a %s window", w.Header().Get("Retry-After"), window)
	}
}

// reached is the one rate_limit.reached event, checked for its limit.
func reached(t *testing.T, s *Server, limit string) storedEvent {
	t.Helper()
	e := onlyEvent(t, s.Store.DB, "rate_limit.reached")
	if e.Payload["limit"] != limit || e.IP != "127.0.0.1" || e.Payload["resetsAt"] == nil {
		t.Fatalf("want %s: %+v", limit, e)
	}
	return e
}

const newWorkspace = `{"name":"Another"}`
const newProject = `{"name":"Launch","description":"","status":"Planned","color":"blue"}`

// Signed-in writes share a budget per user, in the database; another user on
// the same IP keeps theirs.
func TestWriteBudget(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	spent, other := signedIn(t, db, f), signedIn(t, db, f)
	publicID := publicIDOf(t, s, spent.UserID)
	must(factory.RateLimits.State(factory.ForLimit("api:per-user", publicID)).State(factory.Exhausted(300)).CreateOne(ctx, db, f))(t)
	refused := call(t, h, "POST", "/api/workspaces", newWorkspace, spent.CSRFToken, spent.Cookie(), 429)
	retryWithin(t, refused, 15*time.Minute)
	if !strings.Contains(refused.Body.String(), "Too many requests. Try again in") {
		t.Fatalf("refusal: %s", refused.Body.String())
	}
	call(t, h, "PATCH", "/api/auth/me", `{"name":"Renamed"}`, spent.CSRFToken, spent.Cookie(), 429)
	e := reached(t, s, "api:per-user:block")
	if e.Payload["user"] != publicID || !is(e.UserID, spent.UserID) || e.Payload["window"] != float64(900) {
		t.Fatalf("event: %+v", e)
	}
	// Reads have their own budget, and other users are untouched.
	call(t, h, "GET", "/api/auth/me", "", "", spent.Cookie(), 200)
	call(t, h, "POST", "/api/workspaces", newWorkspace, other.CSRFToken, other.Cookie(), 201)
}

// publicIDOf is the user's usr_ ID.
func publicIDOf(t *testing.T, s *Server, userID int64) string {
	t.Helper()
	var id string
	if err := s.Store.DB.NewSelect().TableExpr("users").Column("public_id").Where("id = ?", userID).Scan(context.Background(), &id); err != nil {
		t.Fatal(err)
	}
	return id
}

// With the write budget used up, the owner can still sign the session out,
// revoke the others and change the password.
func TestRecoveryRoutesAreUnmetered(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	workspace := ownedWorkspace(t, db, f, session.UserID)
	must(factory.RateLimits.State(factory.ForLimit("api:per-user", publicIDOf(t, s, session.UserID))).State(factory.Exhausted(300)).CreateOne(ctx, db, f))(t)
	call(t, h, "POST", workspacePath(workspace.PublicID, "/projects"), newProject, session.CSRFToken, session.Cookie(), 429)
	call(t, h, "DELETE", "/api/auth/sessions", "", session.CSRFToken, session.Cookie(), 204)
	call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword), session.CSRFToken, session.Cookie(), 204)
	changed := call(t, h, "PATCH", "/api/auth/password", fmt.Sprintf(`{"currentPassword":%q,"newPassword":"a brand new passphrase"}`, factory.DefaultPassword), session.CSRFToken, session.Cookie(), 200)
	var signed response
	if err := json.Unmarshal(changed.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	call(t, h, "POST", "/api/auth/logout", "{}", signed.CSRF, changed.Result().Cookies()[0], 204)
}

// A request without the user's CSRF token is refused before it counts, so
// another site can't spend someone's budget.
func TestBudgetCountsAfterCSRF(t *testing.T) {
	s, db := integrationServer(t)
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	for range 3 {
		call(t, s.Routes(), "POST", "/api/workspaces", newWorkspace, "wrong", session.Cookie(), 403)
	}
	if n := countRateLimits(t, s, "api:per-user:%"); n != 0 {
		t.Fatalf("forged requests counted: %d", n)
	}
}

// countRateLimits counts the counters whose key matches pattern.
func countRateLimits(t *testing.T, s *Server, pattern string) int {
	t.Helper()
	n, err := s.Store.DB.NewSelect().TableExpr("rate_limits").Where("key LIKE ?", pattern).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// Reads are counted in memory, per process.
func TestReadBudget(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	f := factory.TestFaker(t)
	session, other := signedIn(t, db, f), signedIn(t, db, f)
	publicID := publicIDOf(t, s, session.UserID)
	for range 999 {
		s.memoryLimits.Allow(apiReadPerUser.Key(publicID), apiReadPerUser.Max, apiReadPerUser.Window)
	}
	call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 200)
	refused := call(t, h, "GET", "/api/workspaces", "", "", session.Cookie(), 429)
	retryWithin(t, refused, 5*time.Minute)
	call(t, h, "GET", "/api/workspaces", "", "", session.Cookie(), 429)
	if e := reached(t, s, "api-read:per-user:block"); e.Payload["user"] != publicID {
		t.Fatalf("event: %+v", e)
	}
	// Writes and other users keep their own budgets.
	call(t, h, "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 201)
	call(t, h, "GET", "/api/workspaces", "", "", other.Cookie(), 200)
	if n := countRateLimits(t, s, "api-read:%"); n != 0 {
		t.Fatalf("reads written to the database: %d", n)
	}
}

// Creating workspaces and projects has a limit of its own per user.
func TestCreateLimits(t *testing.T) {
	ctx := context.Background()
	t.Run("workspaces", func(t *testing.T) {
		s, db := integrationServer(t)
		f := factory.TestFaker(t)
		session, other := signedIn(t, db, f), signedIn(t, db, f)
		must(factory.RateLimits.State(factory.ForLimit("workspace:per-user", publicIDOf(t, s, session.UserID))).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
		refused := call(t, s.Routes(), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 429)
		retryWithin(t, refused, time.Hour)
		if e := reached(t, s, "workspace:per-user:block"); !is(e.UserID, session.UserID) || e.Payload["workspace"] != nil {
			t.Fatalf("event: %+v", e)
		}
		call(t, s.Routes(), "POST", "/api/workspaces", newWorkspace, other.CSRFToken, other.Cookie(), 201)
	})
	t.Run("projects, across workspaces", func(t *testing.T) {
		s, db := integrationServer(t)
		f := factory.TestFaker(t)
		session := signedIn(t, db, f)
		first, second := ownedWorkspace(t, db, f, session.UserID), ownedWorkspace(t, db, f, session.UserID)
		must(factory.RateLimits.State(factory.ForLimit("project:per-user", publicIDOf(t, s, session.UserID))).State(factory.Exhausted(99)).CreateOne(ctx, db, f))(t)
		call(t, s.Routes(), "POST", workspacePath(first.PublicID, "/projects"), newProject, session.CSRFToken, session.Cookie(), 201)
		call(t, s.Routes(), "POST", workspacePath(second.PublicID, "/projects"), newProject, session.CSRFToken, session.Cookie(), 429)
		e := reached(t, s, "project:per-user:block")
		if e.Payload["workspace"] != second.PublicID || !is(e.WorkspaceID, second.ID) {
			t.Fatalf("event: %+v", e)
		}
		// Editing has only the budget.
		project := must(factory.ProjectsIn(first.ID).CreateOne(ctx, db, f))(t)
		call(t, s.Routes(), "PATCH", workspacePath(first.PublicID, "/projects/"+project.PublicID), newProject, session.CSRFToken, session.Cookie(), 200)
	})
}
func TestRegistrationLimits(t *testing.T) {
	ctx := context.Background()
	register := func(email string) string {
		return fmt.Sprintf(`{"name":"New","email":%q,"password":"correct horse battery staple"}`, email)
	}
	t.Run("per IP, before the global limit", func(t *testing.T) {
		s, db := integrationServer(t)
		f := factory.TestFaker(t)
		must(factory.RateLimits.State(factory.ForLimit("register:per-ip", "127.0.0.1")).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
		refused := call(t, s.Routes(), "POST", "/api/auth/register", register("new@example.com"), "", nil, 429)
		retryWithin(t, refused, time.Hour)
		reached(t, s, "register:per-ip:block")
		if n := countRateLimits(t, s, "register:global"); n != 0 {
			t.Fatal("a request refused per IP spent the global limit")
		}
		// A malformed body doesn't count either.
		call(t, s.Routes(), "POST", "/api/auth/register", register("not an email"), "", nil, 422)
		if n := countRateLimits(t, s, "register:%"); n != 1 {
			t.Fatalf("%d register counters", n)
		}
	})
	t.Run("global", func(t *testing.T) {
		s, db := integrationServer(t)
		f := factory.TestFaker(t)
		must(factory.RateLimits.State(factory.ForLimit("register:global", "")).State(factory.Exhausted(1000)).CreateOne(ctx, db, f))(t)
		busy := call(t, s.Routes(), "POST", "/api/auth/register", register("new@example.com"), "", nil, 503)
		retryWithin(t, busy, time.Hour)
		if !strings.Contains(busy.Body.String(), "Sign-up is busy. Try again later.") {
			t.Fatalf("busy: %s", busy.Body.String())
		}
		reached(t, s, "register:global:block")
		// Signing in isn't affected.
		user := must(factory.Users.CreateOne(ctx, db, f))(t)
		call(t, s.Routes(), "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword), "", nil, 200)
	})
}

// RATE_LIMITS=off allows everything and counts nothing; quotas stay.
func TestRateLimitsOff(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	must(database.Migrate(ctx, db))(t)
	s := must(New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000", AppKey: factory.AppKey, RateLimits: config.RateLimitsOff}))(t)
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	must(factory.RateLimits.State(factory.ForLimit("workspace:per-user", publicIDOf(t, s, session.UserID))).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	call(t, s.Routes(), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 201)
	if n := countRateLimits(t, s, "%"); n != 1 {
		t.Fatalf("counted with limits off: %d counters", n)
	}
	for range store.MaxOwnedWorkspaces - 1 {
		ownedWorkspace(t, db, f, session.UserID)
	}
	call(t, s.Routes(), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 422)
}
func TestQuotas(t *testing.T) {
	ctx := context.Background()
	t.Run("owned workspaces", func(t *testing.T) {
		s, db := integrationServer(t)
		f := factory.TestFaker(t)
		session := signedIn(t, db, f)
		for range store.MaxOwnedWorkspaces {
			ownedWorkspace(t, db, f, session.UserID)
		}
		// Belonging to someone else's workspace doesn't count.
		others := ownedWorkspace(t, db, f, signedIn(t, db, f).UserID)
		must(factory.MemberOf(ctx, db, f, session.UserID, others.ID, "admin"))(t)
		refused := call(t, s.Routes(), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 422)
		if !strings.Contains(refused.Body.String(), `"error":"You can own up to 20 workspaces."`) || strings.Contains(refused.Body.String(), "field") {
			t.Fatalf("refusal: %s", refused.Body.String())
		}
		if n := len(eventsNamed(t, db, "workspace.created")); n != 0 {
			t.Fatalf("%d workspace.created events", n)
		}
	})
	t.Run("projects in a workspace", func(t *testing.T) {
		s, db := integrationServer(t)
		f := factory.TestFaker(t)
		session := signedIn(t, db, f)
		workspace := ownedWorkspace(t, db, f, session.UserID)
		must(factory.ProjectsIn(workspace.ID).Create(ctx, db, f, store.MaxWorkspaceProjects-1))(t)
		path := workspacePath(workspace.PublicID, "/projects")
		// Two creates at 999 make exactly one: the workspace lock serialises them.
		statuses := make([]int, 2)
		var wg sync.WaitGroup
		for i := range statuses {
			wg.Go(func() {
				statuses[i] = send(s.Routes(), "POST", path, newProject, session.CSRFToken, session.Cookie()).Code
			})
		}
		wg.Wait()
		if !(statuses[0] == 201 && statuses[1] == 422 || statuses[0] == 422 && statuses[1] == 201) {
			t.Fatalf("concurrent creates at 999: %v", statuses)
		}
		refused := call(t, s.Routes(), "POST", path, newProject, session.CSRFToken, session.Cookie(), 422)
		if !strings.Contains(refused.Body.String(), "A workspace can have up to 1,000 projects.") {
			t.Fatalf("refusal: %s", refused.Body.String())
		}
		n, err := db.NewSelect().TableExpr("projects").Where("workspace_id = ?", workspace.ID).Count(ctx)
		if err != nil || n != int64(store.MaxWorkspaceProjects) {
			t.Fatalf("%d projects (%v)", n, err)
		}
	})
}
