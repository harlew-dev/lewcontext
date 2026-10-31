package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"strings"
	"sync"
	"testing"
	"time"
)

const newWorkspace = `{"name":"Another"}`
const newProject = `{"name":"Launch","description":"","status":"Planned","color":"blue"}`

// Signed-in writes share a budget per user, in the database; another user on
// the same IP keeps theirs.
func TestWriteBudget(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	spent, other := apitest.SignedIn(t, db, f), apitest.SignedIn(t, db, f)
	publicID := apitest.PublicIDOf(t, s.Repo.DB, spent.UserID)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("api:per-user", publicID)).State(factory.Exhausted(300)).CreateOne(ctx, db, f))(t)
	refused := apitest.Call(t, h, "POST", "/api/workspaces", newWorkspace, spent.CSRFToken, spent.Cookie(), 429)
	apitest.RetryWithin(t, refused, 15*time.Minute)
	if !strings.Contains(refused.Body.String(), "Too many requests. Try again in") {
		t.Fatalf("refusal: %s", refused.Body.String())
	}
	apitest.Call(t, h, "PATCH", "/api/auth/me", `{"name":"Renamed"}`, spent.CSRFToken, spent.Cookie(), 429)
	e := apitest.Reached(t, s.Repo.DB, "api:per-user:block")
	if e.Payload["user"] != publicID || !apitest.Is(e.UserID, spent.UserID) || e.Payload["window"] != float64(900) {
		t.Fatalf("event: %+v", e)
	}
	// Reads have their own budget, and other users are untouched.
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", spent.Cookie(), 200)
	apitest.Call(t, h, "POST", "/api/workspaces", newWorkspace, other.CSRFToken, other.Cookie(), 201)
}

// With the write budget used up, the owner can still sign the session out,
// revoke the others and change the password.
func TestRecoveryRoutesAreUnmetered(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, session.UserID)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("api:per-user", apitest.PublicIDOf(t, s.Repo.DB, session.UserID))).State(factory.Exhausted(300)).CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "POST", apitest.WorkspacePath(workspace.PublicID, "/projects"), newProject, session.CSRFToken, session.Cookie(), 429)
	apitest.Call(t, h, "DELETE", "/api/auth/sessions", "", session.CSRFToken, session.Cookie(), 204)
	apitest.Call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword), session.CSRFToken, session.Cookie(), 204)
	changed := apitest.Call(t, h, "PATCH", "/api/auth/password", fmt.Sprintf(`{"currentPassword":%q,"newPassword":"a brand new passphrase"}`, factory.DefaultPassword), session.CSRFToken, session.Cookie(), 200)
	var signed apitest.SessionResponse
	if err := json.Unmarshal(changed.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	apitest.Call(t, h, "POST", "/api/auth/logout", "{}", signed.CSRF, changed.Result().Cookies()[0], 204)
}

// A request without the user's CSRF token is refused before it counts, so
// another site can't spend someone's budget.
func TestBudgetCountsAfterCSRF(t *testing.T) {
	s, db := server(t)
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	for range 3 {
		apitest.Call(t, router.New(s), "POST", "/api/workspaces", newWorkspace, "wrong", session.Cookie(), 403)
	}
	if n := apitest.CountRateLimits(t, s.Repo.DB, "api:per-user:%"); n != 0 {
		t.Fatalf("forged requests counted: %d", n)
	}
}

// Creating workspaces and projects has a limit of its own per user.
func TestCreateLimits(t *testing.T) {
	ctx := context.Background()
	t.Run("workspaces", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		session, other := apitest.SignedIn(t, db, f), apitest.SignedIn(t, db, f)
		apitest.Must(factory.RateLimits.State(factory.ForLimit("workspace:per-user", apitest.PublicIDOf(t, s.Repo.DB, session.UserID))).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
		refused := apitest.Call(t, router.New(s), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 429)
		apitest.RetryWithin(t, refused, time.Hour)
		if e := apitest.Reached(t, s.Repo.DB, "workspace:per-user:block"); !apitest.Is(e.UserID, session.UserID) || e.Payload["workspace"] != nil {
			t.Fatalf("event: %+v", e)
		}
		apitest.Call(t, router.New(s), "POST", "/api/workspaces", newWorkspace, other.CSRFToken, other.Cookie(), 201)
	})
	t.Run("projects, across workspaces", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		session := apitest.SignedIn(t, db, f)
		first, second := apitest.OwnedWorkspace(t, db, f, session.UserID), apitest.OwnedWorkspace(t, db, f, session.UserID)
		apitest.Must(factory.RateLimits.State(factory.ForLimit("project:per-user", apitest.PublicIDOf(t, s.Repo.DB, session.UserID))).State(factory.Exhausted(99)).CreateOne(ctx, db, f))(t)
		apitest.Call(t, router.New(s), "POST", apitest.WorkspacePath(first.PublicID, "/projects"), newProject, session.CSRFToken, session.Cookie(), 201)
		apitest.Call(t, router.New(s), "POST", apitest.WorkspacePath(second.PublicID, "/projects"), newProject, session.CSRFToken, session.Cookie(), 429)
		e := apitest.Reached(t, s.Repo.DB, "project:per-user:block")
		if e.Payload["workspace"] != second.PublicID || !apitest.Is(e.WorkspaceID, second.ID) {
			t.Fatalf("event: %+v", e)
		}
		// Editing has only the budget.
		project := apitest.Must(factory.ProjectsIn(first.ID).CreateOne(ctx, db, f))(t)
		apitest.Call(t, router.New(s), "PATCH", apitest.WorkspacePath(first.PublicID, "/projects/"+project.PublicID), newProject, session.CSRFToken, session.Cookie(), 200)
	})
}
func TestRegistrationLimits(t *testing.T) {
	ctx := context.Background()
	register := func(email string) string {
		return fmt.Sprintf(`{"name":"New","email":%q,"password":"correct horse battery staple"}`, email)
	}
	t.Run("per IP, before the global limit", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		apitest.Must(factory.RateLimits.State(factory.ForLimit("register:per-ip", "127.0.0.1")).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
		refused := apitest.Call(t, router.New(s), "POST", "/api/auth/register", register("new@example.com"), "", nil, 429)
		apitest.RetryWithin(t, refused, time.Hour)
		apitest.Reached(t, s.Repo.DB, "register:per-ip:block")
		if n := apitest.CountRateLimits(t, s.Repo.DB, "register:global"); n != 0 {
			t.Fatal("a request refused per IP spent the global limit")
		}
		// A malformed body doesn't count either.
		apitest.Call(t, router.New(s), "POST", "/api/auth/register", register("not an email"), "", nil, 422)
		if n := apitest.CountRateLimits(t, s.Repo.DB, "register:%"); n != 1 {
			t.Fatalf("%d register counters", n)
		}
	})
	t.Run("global", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		apitest.Must(factory.RateLimits.State(factory.ForLimit("register:global", "")).State(factory.Exhausted(1000)).CreateOne(ctx, db, f))(t)
		busy := apitest.Call(t, router.New(s), "POST", "/api/auth/register", register("new@example.com"), "", nil, 503)
		apitest.RetryWithin(t, busy, time.Hour)
		if !strings.Contains(busy.Body.String(), "Sign-up is busy. Try again later.") {
			t.Fatalf("busy: %s", busy.Body.String())
		}
		apitest.Reached(t, s.Repo.DB, "register:global:block")
		// Signing in isn't affected.
		user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
		apitest.Call(t, router.New(s), "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword), "", nil, 200)
	})
}

// RATE_LIMITS=off allows everything and counts nothing; quotas stay.
func TestRateLimitsOff(t *testing.T) {
	db := dbtest.Open(t)
	ctx := context.Background()
	apitest.Must(database.Migrate(ctx, db))(t)
	s := apitest.Must(httpapi.New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000", AppKey: factory.AppKey, RateLimits: config.RateLimitsOff}))(t)
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("workspace:per-user", apitest.PublicIDOf(t, s.Repo.DB, session.UserID))).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	apitest.Call(t, router.New(s), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 201)
	if n := apitest.CountRateLimits(t, s.Repo.DB, "%"); n != 1 {
		t.Fatalf("counted with limits off: %d counters", n)
	}
	for range store.MaxOwnedWorkspaces - 1 {
		apitest.OwnedWorkspace(t, db, f, session.UserID)
	}
	apitest.Call(t, router.New(s), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 422)
}
func TestQuotas(t *testing.T) {
	ctx := context.Background()
	t.Run("owned workspaces", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		session := apitest.SignedIn(t, db, f)
		for range store.MaxOwnedWorkspaces {
			apitest.OwnedWorkspace(t, db, f, session.UserID)
		}
		// Belonging to someone else's workspace doesn't count.
		others := apitest.OwnedWorkspace(t, db, f, apitest.SignedIn(t, db, f).UserID)
		apitest.Must(factory.MemberOf(ctx, db, f, session.UserID, others.ID, "admin"))(t)
		refused := apitest.Call(t, router.New(s), "POST", "/api/workspaces", newWorkspace, session.CSRFToken, session.Cookie(), 422)
		if !strings.Contains(refused.Body.String(), `"error":"You can own up to 20 workspaces."`) || strings.Contains(refused.Body.String(), "field") {
			t.Fatalf("refusal: %s", refused.Body.String())
		}
		if n := len(apitest.EventsNamed(t, db, "workspace.created")); n != 0 {
			t.Fatalf("%d workspace.created events", n)
		}
	})
	t.Run("projects in a workspace", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		session := apitest.SignedIn(t, db, f)
		workspace := apitest.OwnedWorkspace(t, db, f, session.UserID)
		apitest.Must(factory.ProjectsIn(workspace.ID).Create(ctx, db, f, store.MaxWorkspaceProjects-1))(t)
		path := apitest.WorkspacePath(workspace.PublicID, "/projects")
		// Two creates at 999 make exactly one: the workspace lock serialises them.
		statuses := make([]int, 2)
		var wg sync.WaitGroup
		for i := range statuses {
			wg.Go(func() {
				statuses[i] = apitest.Send(router.New(s), "POST", path, newProject, session.CSRFToken, session.Cookie()).Code
			})
		}
		wg.Wait()
		if !(statuses[0] == 201 && statuses[1] == 422 || statuses[0] == 422 && statuses[1] == 201) {
			t.Fatalf("concurrent creates at 999: %v", statuses)
		}
		refused := apitest.Call(t, router.New(s), "POST", path, newProject, session.CSRFToken, session.Cookie(), 422)
		if !strings.Contains(refused.Body.String(), "A workspace can have up to 1,000 projects.") {
			t.Fatalf("refusal: %s", refused.Body.String())
		}
		n, err := db.NewSelect().TableExpr("projects").Where("workspace_id = ?", workspace.ID).Count(ctx)
		if err != nil || n != int64(store.MaxWorkspaceProjects) {
			t.Fatalf("%d projects (%v)", n, err)
		}
	})
}
