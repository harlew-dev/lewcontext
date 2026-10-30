package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"forma/api/internal/auth"
	"forma/api/internal/config"
	"forma/api/internal/database"
	"forma/api/internal/database/dbtest"
	"forma/api/internal/factory"
	"forma/api/internal/store"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Each run owns a unique schema; existing application data is never reset.
func integrationServer(t *testing.T) (*Server, *bun.DB) {
	t.Helper()
	db := dbtest.Open(t)
	ctx := context.Background()
	must(database.Migrate(ctx, db))(t)
	if group, err := database.Migrate(ctx, db); err != nil || !group.IsZero() {
		t.Fatal("migration is not idempotent", group, err)
	}
	server := must(New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000"}))(t)
	return server, db
}

type response struct {
	User store.User `json:"user"`
	CSRF string     `json:"csrfToken"`
}

const testUserAgent = "forma-integration-test/1.0"

func call(t *testing.T, h http.Handler, method, path, body, csrf string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("User-Agent", testUserAgent)
	r.RemoteAddr = "127.0.0.1:1234"
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: want %d got %d: %s", method, path, want, w.Code, w.Body.String())
	}
	return w
}

// signedIn creates a fake user with a live session, without the rate-limited
// auth endpoints. The session carries the cookie and CSRF token.
func signedIn(t *testing.T, db bun.IDB, f *gofakeit.Faker) factory.Session {
	t.Helper()
	user := must(factory.Users.CreateOne(context.Background(), db, f))(t)
	return must(factory.SessionFor(context.Background(), db, f, user.ID))(t)
}

// must returns v, or fails the test with err. Go only lets a two-result call
// fill a function's whole argument list, so it takes the call's results and
// returns a function of t:
//
//	user := must(factory.Users.CreateOne(ctx, db, f))(t)
func must[T any](v T, err error) func(testing.TB) T {
	return func(t testing.TB) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// idOf is the internal ID of the record in table ("users", "workspaces",
// "projects") with publicID.
func idOf(t *testing.T, db bun.IDB, table, publicID string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(context.Background(), "SELECT id FROM ? WHERE public_id = ?", bun.Ident(table), publicID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// emailOf is the user's email address.
func emailOf(t *testing.T, db bun.IDB, userID int64) string {
	t.Helper()
	var email string
	if err := db.QueryRowContext(context.Background(), "SELECT email FROM users WHERE id = ?", userID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// ownedWorkspace creates a workspace owned by userID.
func ownedWorkspace(t *testing.T, db bun.IDB, f *gofakeit.Faker, userID int64) factory.Workspace {
	t.Helper()
	return must(factory.WorkspaceFor(context.Background(), db, f, userID))(t)
}

// workspacePath is a path inside a workspace, e.g. workspacePath(id, "/projects").
func workspacePath(publicID, rest string) string {
	return "/api/workspaces/" + publicID + rest
}

// numericID finds an "id" that is a number, i.e. a leaked internal ID.
var numericID = regexp.MustCompile(`"id":\s*\d`)

func TestAuthenticationAndProjectIsolation(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	register := func(email string) (response, *http.Cookie) {
		w := call(t, h, "POST", "/api/auth/register", fmt.Sprintf(`{"name":"Test User","email":%q,"password":"correct horse battery staple"}`, email), "", nil, 201)
		var res response
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res, w.Result().Cookies()[0]
	}
	one, cookie := register("one@example.com")
	two := signedIn(t, db, f)
	otherCookie := two.Cookie()
	call(t, h, "POST", "/api/auth/register", `{"name":"Again","email":"one@example.com","password":"correct horse battery staple"}`, "", nil, 409)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie flags missing")
	}
	var stored string
	if err := db.QueryRowContext(ctx, "SELECT s.token_hash FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.public_id=?", one.User.PublicID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == cookie.Value || stored != auth.Digest(cookie.Value) {
		t.Fatal("raw session stored")
	}
	me := call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)
	if !strings.HasPrefix(one.User.PublicID, "usr_") || !strings.Contains(me.Body.String(), one.User.PublicID) || numericID.MatchString(me.Body.String()) {
		t.Fatalf("user not identified by public ID: %s", me.Body.String())
	}
	// A new account starts with no workspace; onboarding creates the first.
	empty := call(t, h, "GET", "/api/workspaces", "", "", cookie, 200)
	if !strings.Contains(empty.Body.String(), `"data":[]`) || !strings.Contains(empty.Body.String(), `"items":0`) {
		t.Fatalf("new account has workspaces: %s", empty.Body.String())
	}
	call(t, h, "POST", "/api/workspaces", `{"name":"Mine"}`, "", cookie, 403)
	var workspace store.Workspace
	_ = json.Unmarshal(call(t, h, "POST", "/api/workspaces", `{"name":"  Mine  "}`, one.CSRF, cookie, 201).Body.Bytes(), &workspace)
	if !validPublicID("wsp", workspace.PublicID) || workspace.Name != "Mine" || workspace.Role != store.RoleOwner {
		t.Fatalf("created workspace not returned: %+v", workspace)
	}
	projects := workspacePath(workspace.PublicID, "/projects")
	otherProjects := workspacePath(ownedWorkspace(t, db, f, two.UserID).PublicID, "/projects")
	call(t, h, "GET", projects, "", "", nil, 401)
	body := `{"name":"Private project","description":"Owned by one","status":"Planned","color":"violet"}`
	call(t, h, "POST", projects, body, "", cookie, 403)
	created := call(t, h, "POST", projects, body, one.CSRF, cookie, 201)
	if numericID.MatchString(created.Body.String()) {
		t.Fatalf("internal ID leaked: %s", created.Body.String())
	}
	var project store.Project
	_ = json.Unmarshal(created.Body.Bytes(), &project)
	if !validPublicID("prj", project.PublicID) || project.Name != "Private project" || project.Status != "Planned" || project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
		t.Fatalf("created project not returned: %+v", project)
	}
	var owned store.Page[store.Project]
	_ = json.Unmarshal(call(t, h, "GET", projects, "", "", cookie, 200).Body.Bytes(), &owned)
	if len(owned.Data) != 1 || owned.Data[0].PublicID != project.PublicID || owned.Data[0].Description != "Owned by one" || owned.Pages.Items != 1 {
		t.Fatalf("owner list wrong: %+v", owned)
	}
	// Another user can't reach the workspace at all, and their own is empty.
	call(t, h, "GET", projects, "", "", otherCookie, 404)
	call(t, h, "POST", projects, body, two.CSRFToken, otherCookie, 404)
	list := call(t, h, "GET", otherProjects, "", "", otherCookie, 200)
	if !strings.Contains(list.Body.String(), `"data":[]`) || !strings.Contains(list.Body.String(), `"items":0`) {
		t.Fatalf("cross-user project leak: %s", list.Body.String())
	}
	path := projects + "/" + project.PublicID
	call(t, h, "GET", path, "", "", cookie, 200)
	call(t, h, "GET", path, "", "", otherCookie, 404)
	call(t, h, "PATCH", path, body, two.CSRFToken, otherCookie, 404)
	// The project ID under the other user's own workspace is still not theirs.
	call(t, h, "GET", otherProjects+"/"+project.PublicID, "", "", otherCookie, 404)
	call(t, h, "PATCH", otherProjects+"/"+project.PublicID, body, two.CSRFToken, otherCookie, 404)
	updated := strings.Replace(body, "Planned", "Completed", 1)
	var saved store.Project
	_ = json.Unmarshal(call(t, h, "PATCH", path, updated, one.CSRF, cookie, 200).Body.Bytes(), &saved)
	if saved.PublicID != project.PublicID || saved.Status != "Completed" || !saved.CreatedAt.Equal(project.CreatedAt) || saved.UpdatedAt.Before(project.UpdatedAt) {
		t.Fatalf("updated project not returned: %+v", saved)
	}
	call(t, h, "PATCH", projects+"/prj_00000000000000", body, one.CSRF, cookie, 404)
	// Old routes and numeric IDs are gone.
	call(t, h, "GET", "/api/projects", "", "", cookie, 404)
	call(t, h, "GET", projects+"/1", "", "", cookie, 404)
	call(t, h, "POST", "/api/auth/login", `{"email":"one@example.com","password":"wrong password long"}`, "", nil, 401)
	login := call(t, h, "POST", "/api/auth/login", `{"email":"one@example.com","password":"correct horse battery staple"}`, "", cookie, 200)
	rotated := login.Result().Cookies()[0]
	if rotated.Value == cookie.Value {
		t.Fatal("session not rotated")
	}
	call(t, h, "GET", "/api/auth/me", "", "", cookie, 401)
	var signed response
	_ = json.Unmarshal(login.Body.Bytes(), &signed)
	secondLogin := call(t, h, "POST", "/api/auth/login", `{"email":"one@example.com","password":"correct horse battery staple"}`, "", nil, 200)
	secondCookie := secondLogin.Result().Cookies()[0]
	call(t, h, "DELETE", "/api/auth/sessions", "", signed.CSRF, rotated, 204)
	call(t, h, "GET", "/api/auth/me", "", "", secondCookie, 401)
	call(t, h, "POST", "/api/auth/logout", "{}", signed.CSRF, rotated, 204)
	call(t, h, "GET", "/api/auth/me", "", "", rotated, 401)
	for _, state := range []func(*factory.Session){factory.Idle, factory.Expired} {
		stale := must(factory.Sessions.State(func(s *factory.Session) { s.UserID = two.UserID }).State(state).CreateOne(ctx, db, f))(t)
		call(t, h, "GET", "/api/auth/me", "", "", stale.Cookie(), 401)
	}
	capped := must(factory.Users.CreateOne(ctx, db, f))(t)
	first := must(factory.SessionFor(ctx, db, f, capped.ID))(t)
	for i := 0; i < 5; i++ {
		call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, capped.Email, factory.DefaultPassword), "", nil, 200)
	}
	call(t, h, "GET", "/api/auth/me", "", "", first.Cookie(), 401)
	var sessions int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE user_id=?", capped.ID).Scan(&sessions); err != nil || sessions != 5 {
		t.Fatalf("session cap: want 5 sessions, got %d (%v)", sessions, err)
	}
	for i := 0; i < 10; i++ {
		call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 401)
	}
	call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 429)
}

// The 12-character minimum applies to sign-up; an account with a shorter
// password, such as the development seed's, still signs in.
func TestPasswordMinimumOnlyAtSignUp(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	call(t, h, "POST", "/api/auth/register", `{"name":"Short","email":"short@example.com","password":"password"}`, "", nil, 422)
	hash := must(auth.HashPassword("password"))(t)
	seeded := must(factory.Users.State(factory.WithPasswordHash(hash)).CreateOne(ctx, db, factory.TestFaker(t)))(t)
	call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"password"}`, seeded.Email), "", nil, 200)
	call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"passwore"}`, seeded.Email), "", nil, 401)
}

// Prefilled counters reach the limits without making every attempt. Each
// test starts with no counters, since a request creates its own.
func TestAccountRateLimit(t *testing.T) {
	s, db := integrationServer(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := must(factory.Users.CreateOne(ctx, db, f))(t)
	must(factory.AuthAttempts.State(factory.ForAccount(user.Email)).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	login := fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword)
	locked := call(t, s.Routes(), "POST", "/api/auth/login", login, "", nil, 429)
	if locked.Header().Get("Retry-After") != "900" {
		t.Fatal("rate-limited login without Retry-After")
	}
}
func TestIPRateLimit(t *testing.T) {
	s, db := integrationServer(t)
	f := factory.TestFaker(t)
	must(factory.AuthAttempts.State(factory.ForIP("127.0.0.1")).State(factory.Exhausted(60)).CreateOne(context.Background(), db, f))(t)
	call(t, s.Routes(), "POST", "/api/auth/register", `{"name":"New","email":"new@example.com","password":"correct horse battery staple"}`, "", nil, 429)
}
