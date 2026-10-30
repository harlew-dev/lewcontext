package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"forma/api/internal/auth"
	"forma/api/internal/config"
	"forma/api/internal/database"
	"forma/api/internal/database/dbtest"
	"forma/api/internal/store"
	"github.com/uptrace/bun"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Each run owns a unique schema; existing application data is never reset.
func integrationServer(t *testing.T) (*Server, *bun.DB) {
	t.Helper()
	db := dbtest.Open(t)
	ctx := context.Background()
	if _, err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if group, err := database.Migrate(ctx, db); err != nil || !group.IsZero() {
		t.Fatal("migration is not idempotent", group, err)
	}
	server, err := New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000"})
	if err != nil {
		t.Fatal(err)
	}
	return server, db
}

type response struct {
	User store.User `json:"user"`
	CSRF string     `json:"csrfToken"`
}

func call(t *testing.T, h http.Handler, method, path, body, csrf string, cookie *http.Cookie, want int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
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
func TestAuthenticationAndProjectIsolation(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	register := func(email string) (response, *http.Cookie) {
		w := call(t, h, "POST", "/api/auth/register", fmt.Sprintf(`{"name":"Test User","email":%q,"password":"correct horse battery staple"}`, email), "", nil, 201)
		var res response
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		return res, w.Result().Cookies()[0]
	}
	one, cookie := register("one@example.com")
	two, otherCookie := register("two@example.com")
	call(t, h, "POST", "/api/auth/register", `{"name":"Again","email":"one@example.com","password":"correct horse battery staple"}`, "", nil, 409)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie flags missing")
	}
	var stored string
	if err := db.QueryRowContext(ctx, "SELECT token_hash FROM sessions WHERE user_id=?", one.User.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == cookie.Value || stored != auth.Digest(cookie.Value) {
		t.Fatal("raw session stored")
	}
	call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)
	call(t, h, "GET", "/api/projects", "", "", nil, 401)
	body := `{"name":"Private project","description":"Owned by one","status":"Planned","color":"violet"}`
	call(t, h, "POST", "/api/projects", body, "", cookie, 403)
	created := call(t, h, "POST", "/api/projects", body, one.CSRF, cookie, 201)
	var project store.Project
	_ = json.Unmarshal(created.Body.Bytes(), &project)
	if project.ID < 1 || project.Name != "Private project" || project.Status != "Planned" || project.CreatedAt.IsZero() || project.UpdatedAt.IsZero() {
		t.Fatalf("created project not returned: %+v", project)
	}
	var owned store.Page[store.Project]
	_ = json.Unmarshal(call(t, h, "GET", "/api/projects", "", "", cookie, 200).Body.Bytes(), &owned)
	if len(owned.Data) != 1 || owned.Data[0].ID != project.ID || owned.Data[0].Description != "Owned by one" || owned.Pages.Items != 1 {
		t.Fatalf("owner list wrong: %+v", owned)
	}
	list := call(t, h, "GET", "/api/projects", "", "", otherCookie, 200)
	if !strings.Contains(list.Body.String(), `"data":[]`) || !strings.Contains(list.Body.String(), `"items":0`) {
		t.Fatalf("cross-user project leak: %s", list.Body.String())
	}
	path := fmt.Sprintf("/api/projects/%d", project.ID)
	call(t, h, "GET", path, "", "", cookie, 200)
	call(t, h, "GET", path, "", "", otherCookie, 404)
	call(t, h, "GET", "/api/projects/nope", "", "", cookie, 400)
	call(t, h, "PATCH", path, body, two.CSRF, otherCookie, 404)
	updated := strings.Replace(body, "Planned", "Completed", 1)
	var saved store.Project
	_ = json.Unmarshal(call(t, h, "PATCH", path, updated, one.CSRF, cookie, 200).Body.Bytes(), &saved)
	if saved.ID != project.ID || saved.Status != "Completed" || !saved.CreatedAt.Equal(project.CreatedAt) || saved.UpdatedAt.Before(project.UpdatedAt) {
		t.Fatalf("updated project not returned: %+v", saved)
	}
	call(t, h, "PATCH", "/api/projects/999999", body, one.CSRF, cookie, 404)
	call(t, h, "PATCH", "/api/projects/nope", body, one.CSRF, cookie, 400)
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
	if _, err := db.ExecContext(ctx, "UPDATE sessions SET last_seen_at=now()-interval '31 minutes' WHERE user_id=?", two.User.ID); err != nil {
		t.Fatal(err)
	}
	call(t, h, "GET", "/api/auth/me", "", "", otherCookie, 401)
	_, expiryCookie := register("expiry@example.com")
	if _, err := db.ExecContext(ctx, "UPDATE sessions SET expires_at=now()-interval '1 second' WHERE token_hash=?", auth.Digest(expiryCookie.Value)); err != nil {
		t.Fatal(err)
	}
	call(t, h, "GET", "/api/auth/me", "", "", expiryCookie, 401)
	capped, firstCookie := register("cap@example.com")
	for i := 0; i < 5; i++ {
		call(t, h, "POST", "/api/auth/login", `{"email":"cap@example.com","password":"correct horse battery staple"}`, "", nil, 200)
	}
	call(t, h, "GET", "/api/auth/me", "", "", firstCookie, 401)
	var sessions int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM sessions WHERE user_id=?", capped.User.ID).Scan(&sessions); err != nil || sessions != 5 {
		t.Fatalf("session cap: want 5 sessions, got %d (%v)", sessions, err)
	}
	for i := 0; i < 10; i++ {
		call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 401)
	}
	call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 429)
}
