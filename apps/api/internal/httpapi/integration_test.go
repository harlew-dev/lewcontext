package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
	"testing"
	"time"
)

// server is a Server on a migrated database of its own; router.New(s) is
// its handler.
func server(t *testing.T) (*httpapi.Server, *bun.DB) {
	t.Helper()
	db := apitest.Database(t)
	return apitest.Must(httpapi.New(&store.Store{DB: db}, apitest.Config()))(t), db
}

func TestAuthenticationAndProjectIsolation(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	register := func(email string) (apitest.SessionResponse, *http.Cookie) {
		w := apitest.Call(t, h, "POST", "/api/auth/register", fmt.Sprintf(`{"name":"Test User","email":%q,"password":"correct horse battery staple"}`, email), "", nil, 201)
		var res apitest.SessionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res, w.Result().Cookies()[0]
	}
	one, cookie := register("one@example.com")
	two := apitest.SignedIn(t, db, f)
	otherCookie := two.Cookie()
	apitest.Call(t, h, "POST", "/api/auth/register", `{"name":"Again","email":"one@example.com","password":"correct horse battery staple"}`, "", nil, 409)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie flags missing")
	}
	var stored string
	err := db.NewSelect().TableExpr("sessions AS s").Column("s.token_hash").
		Join("JOIN users AS u ON u.id = s.user_id").
		Where("u.public_id = ?", one.User.PublicID).
		Scan(ctx, &stored)
	if err != nil {
		t.Fatal(err)
	}
	if stored == cookie.Value || stored != auth.Digest(cookie.Value) {
		t.Fatal("raw session stored")
	}
	me := apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)
	if !strings.HasPrefix(one.User.PublicID, "usr_") || !strings.Contains(me.Body.String(), one.User.PublicID) || apitest.NumericID.MatchString(me.Body.String()) {
		t.Fatalf("user not identified by public ID: %s", me.Body.String())
	}
	// A new account starts with no workspace; onboarding creates the first.
	empty := apitest.Call(t, h, "GET", "/api/workspaces", "", "", cookie, 200)
	if !strings.Contains(empty.Body.String(), `"data":[]`) || !strings.Contains(empty.Body.String(), `"items":0`) {
		t.Fatalf("new account has workspaces: %s", empty.Body.String())
	}
	apitest.Call(t, h, "POST", "/api/workspaces", `{"name":"Mine"}`, "", cookie, 403)
	var workspace store.Workspace
	_ = json.Unmarshal(apitest.Call(t, h, "POST", "/api/workspaces", `{"name":"  Mine  "}`, one.CSRF, cookie, 201).Body.Bytes(), &workspace)
	if !input.PublicID("wsp", workspace.PublicID) || workspace.Name != "Mine" || workspace.Role != auth.RoleOwner {
		t.Fatalf("created workspace not returned: %+v", workspace)
	}
	projects := apitest.WorkspacePath(workspace.PublicID, "/projects")
	otherProjects := apitest.WorkspacePath(apitest.OwnedWorkspace(t, db, f, two.UserID).PublicID, "/projects")
	apitest.Call(t, h, "GET", projects, "", "", nil, 401)
	body := `{"name":"Private project","description":"Owned by one","status":"Planned","color":"violet"}`
	apitest.Call(t, h, "POST", projects, body, "", cookie, 403)
	created := apitest.Call(t, h, "POST", projects, body, one.CSRF, cookie, 201)
	if apitest.NumericID.MatchString(created.Body.String()) {
		t.Fatalf("internal ID leaked: %s", created.Body.String())
	}
	var project store.Project
	_ = json.Unmarshal(created.Body.Bytes(), &project)
	if !input.PublicID("prj", project.PublicID) || project.Name != "Private project" || project.Status != "Planned" || project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
		t.Fatalf("created project not returned: %+v", project)
	}
	var owned store.Page[store.Project]
	_ = json.Unmarshal(apitest.Call(t, h, "GET", projects, "", "", cookie, 200).Body.Bytes(), &owned)
	if len(owned.Data) != 1 || owned.Data[0].PublicID != project.PublicID || owned.Data[0].Description != "Owned by one" || owned.Pages.Items != 1 {
		t.Fatalf("owner list wrong: %+v", owned)
	}
	// Another user can't reach the workspace at all, and their own is empty.
	apitest.Call(t, h, "GET", projects, "", "", otherCookie, 404)
	apitest.Call(t, h, "POST", projects, body, two.CSRFToken, otherCookie, 404)
	list := apitest.Call(t, h, "GET", otherProjects, "", "", otherCookie, 200)
	if !strings.Contains(list.Body.String(), `"data":[]`) || !strings.Contains(list.Body.String(), `"items":0`) {
		t.Fatalf("cross-user project leak: %s", list.Body.String())
	}
	path := projects + "/" + project.PublicID
	apitest.Call(t, h, "GET", path, "", "", cookie, 200)
	apitest.Call(t, h, "GET", path, "", "", otherCookie, 404)
	apitest.Call(t, h, "PATCH", path, body, two.CSRFToken, otherCookie, 404)
	// The project ID under the other user's own workspace is still not theirs.
	apitest.Call(t, h, "GET", otherProjects+"/"+project.PublicID, "", "", otherCookie, 404)
	apitest.Call(t, h, "PATCH", otherProjects+"/"+project.PublicID, body, two.CSRFToken, otherCookie, 404)
	updated := strings.Replace(body, "Planned", "Completed", 1)
	var saved store.Project
	_ = json.Unmarshal(apitest.Call(t, h, "PATCH", path, updated, one.CSRF, cookie, 200).Body.Bytes(), &saved)
	if saved.PublicID != project.PublicID || saved.Status != "Completed" || !saved.CreatedAt.Equal(project.CreatedAt) || saved.UpdatedAt.Before(project.UpdatedAt) {
		t.Fatalf("updated project not returned: %+v", saved)
	}
	apitest.Call(t, h, "PATCH", projects+"/prj_00000000000000", body, one.CSRF, cookie, 404)
	// Old routes and numeric IDs are gone.
	apitest.Call(t, h, "GET", "/api/projects", "", "", cookie, 404)
	apitest.Call(t, h, "GET", projects+"/1", "", "", cookie, 404)
	apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"one@example.com","password":"wrong password long"}`, "", nil, 401)
	login := apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"one@example.com","password":"correct horse battery staple"}`, "", cookie, 200)
	rotated := login.Result().Cookies()[0]
	if rotated.Value == cookie.Value {
		t.Fatal("session not rotated")
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 401)
	var signed apitest.SessionResponse
	_ = json.Unmarshal(login.Body.Bytes(), &signed)
	secondLogin := apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"one@example.com","password":"correct horse battery staple"}`, "", nil, 200)
	secondCookie := secondLogin.Result().Cookies()[0]
	apitest.Call(t, h, "DELETE", "/api/auth/sessions", "", signed.CSRF, rotated, 204)
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", secondCookie, 401)
	apitest.Call(t, h, "POST", "/api/auth/logout", "{}", signed.CSRF, rotated, 204)
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", rotated, 401)
	for _, state := range []func(*factory.Session){factory.Idle, factory.Expired} {
		stale := apitest.Must(factory.Sessions.State(func(s *factory.Session) { s.UserID = two.UserID }).State(state).CreateOne(ctx, db, f))(t)
		apitest.Call(t, h, "GET", "/api/auth/me", "", "", stale.Cookie(), 401)
	}
	capped := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	first := apitest.Must(factory.SessionFor(ctx, db, f, capped.ID))(t)
	for i := 0; i < 5; i++ {
		apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, capped.Email, factory.DefaultPassword), "", nil, 200)
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", first.Cookie(), 401)
	sessions, err := db.NewSelect().TableExpr("sessions").Where("user_id = ?", capped.ID).Count(ctx)
	if err != nil || sessions != 5 {
		t.Fatalf("session cap: want 5 sessions, got %d (%v)", sessions, err)
	}
	for i := 0; i < 10; i++ {
		apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 401)
	}
	apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 429)
}

// The 12-character minimum applies to sign-up; an account with a shorter
// password, such as the development seed's, still signs in.
func TestPasswordMinimumOnlyAtSignUp(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	apitest.Call(t, h, "POST", "/api/auth/register", `{"name":"Short","email":"short@example.com","password":"password"}`, "", nil, 422)
	hash := apitest.Must(auth.HashPassword("password"))(t)
	seeded := apitest.Must(factory.Users.State(factory.WithPasswordHash(hash)).CreateOne(ctx, db, factory.TestFaker(t)))(t)
	apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"password"}`, seeded.Email), "", nil, 200)
	apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"passwore"}`, seeded.Email), "", nil, 401)
}

// Prefilled counters reach the limits without making every attempt. Each
// test starts with no counters, since a request creates its own.
func TestAccountRateLimit(t *testing.T) {
	s, db := server(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.RateLimits.State(factory.ForAccount(user.Email)).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	login := fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword)
	locked := apitest.Call(t, router.New(s), "POST", "/api/auth/login", login, "", nil, 429)
	apitest.RetryWithin(t, locked, 15*time.Minute)
}
func TestIPRateLimit(t *testing.T) {
	s, db := server(t)
	f := factory.TestFaker(t)
	apitest.Must(factory.RateLimits.State(factory.ForIP("127.0.0.1")).State(factory.Exhausted(60)).CreateOne(context.Background(), db, f))(t)
	apitest.Call(t, router.New(s), "POST", "/api/auth/register", `{"name":"New","email":"new@example.com","password":"correct horse battery staple"}`, "", nil, 429)
}
