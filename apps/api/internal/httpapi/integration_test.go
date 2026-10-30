package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"forma/api/internal/auth"
	"forma/api/internal/config"
	"forma/api/internal/database"
	"forma/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// Each run owns a unique schema; existing application data is never reset.
func integrationServer(t *testing.T) (*Server, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("forma_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); _, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close() })
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, db); err != nil {
		t.Fatal("migration is not idempotent", err)
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
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie flags missing")
	}
	var stored string
	if err := db.QueryRow(ctx, "SELECT token_hash FROM sessions WHERE user_id=$1", one.User.ID).Scan(&stored); err != nil {
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
	list := call(t, h, "GET", "/api/projects", "", "", otherCookie, 200)
	if strings.TrimSpace(list.Body.String()) != "[]" {
		t.Fatal("cross-user project leak")
	}
	path := fmt.Sprintf("/api/projects/%d", project.ID)
	call(t, h, "PATCH", path, body, two.CSRF, otherCookie, 404)
	updated := strings.Replace(body, "Planned", "Completed", 1)
	call(t, h, "PATCH", path, updated, one.CSRF, cookie, 200)
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
	if _, err := db.Exec(ctx, "UPDATE sessions SET last_seen_at=now()-interval '31 minutes' WHERE user_id=$1", two.User.ID); err != nil {
		t.Fatal(err)
	}
	call(t, h, "GET", "/api/auth/me", "", "", otherCookie, 401)
	_, expiryCookie := register("expiry@example.com")
	if _, err := db.Exec(ctx, "UPDATE sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1", auth.Digest(expiryCookie.Value)); err != nil {
		t.Fatal(err)
	}
	call(t, h, "GET", "/api/auth/me", "", "", expiryCookie, 401)
	for i := 0; i < 10; i++ {
		call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 401)
	}
	call(t, h, "POST", "/api/auth/login", `{"email":"missing@example.com","password":"correct horse battery staple"}`, "", nil, 429)
}
