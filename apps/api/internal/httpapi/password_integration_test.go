package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"forma/api/internal/auth"
	"forma/api/internal/factory"
	"github.com/uptrace/bun"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const newPassword = "a brand new passphrase"

// storedPasswordIs reports whether the user's stored hash matches password.
func storedPasswordIs(t *testing.T, db bun.IDB, userID int64, password string) bool {
	t.Helper()
	var hash string
	if err := db.QueryRowContext(context.Background(), "SELECT password_hash FROM users WHERE id=?", userID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	return auth.CheckPassword(password, hash)
}

// errorField reads the field an error response is about.
func errorField(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Field string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Field
}

func changeBody(current, next string) string {
	return fmt.Sprintf(`{"currentPassword":%q,"newPassword":%q}`, current, next)
}

func TestVerifyPassword(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	session := signedIn(t, db, factory.TestFaker(t))
	cookie := session.Cookie()
	body := fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword)
	call(t, h, "POST", "/api/auth/password/verify", body, "", nil, 401)
	call(t, h, "POST", "/api/auth/password/verify", body, "", cookie, 403)
	call(t, h, "POST", "/api/auth/password/verify", body, session.CSRFToken, cookie, 204)
	wrong := call(t, h, "POST", "/api/auth/password/verify", `{"password":"not my password"}`, session.CSRFToken, cookie, 422)
	if errorField(t, wrong) != "password" || !strings.Contains(wrong.Body.String(), wrongCurrentPassword) {
		t.Fatalf("wrong password error: %s", wrong.Body.String())
	}
	call(t, h, "POST", "/api/auth/password/verify", `{"password":""}`, session.CSRFToken, cookie, 422)
	// Checking the password never signs anyone out.
	call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)
}

func TestChangePassword(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	cookie := session.Cookie()
	other, err := factory.SessionFor(ctx, db, f, session.UserID)
	if err != nil {
		t.Fatal(err)
	}
	stranger := signedIn(t, db, f)
	const path = "/api/auth/password"

	call(t, h, "PUT", path, changeBody(factory.DefaultPassword, newPassword), "", nil, 401)
	call(t, h, "PUT", path, changeBody(factory.DefaultPassword, newPassword), "", cookie, 403)
	for _, tc := range []struct{ body, field string }{
		{changeBody("not my password", newPassword), "currentPassword"},
		{changeBody("", newPassword), "currentPassword"},
		{changeBody(factory.DefaultPassword, "too short"), "newPassword"},
		{changeBody(factory.DefaultPassword, strings.Repeat("x", 129)), "newPassword"},
		{changeBody(factory.DefaultPassword, factory.DefaultPassword), "newPassword"},
	} {
		refused := call(t, h, "PUT", path, tc.body, session.CSRFToken, cookie, 422)
		if errorField(t, refused) != tc.field {
			t.Fatalf("%s: want field %s: %s", tc.body, tc.field, refused.Body.String())
		}
	}
	// Refused changes leave the password and every session alone.
	if !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatal("refused change altered the password")
	}
	call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 200)

	changed := call(t, h, "PUT", path, changeBody(factory.DefaultPassword, newPassword), session.CSRFToken, cookie, 200)
	var signed response
	if err := json.Unmarshal(changed.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	rotated := changed.Result().Cookies()[0]
	if rotated.Value == cookie.Value || signed.CSRF == "" || signed.CSRF == session.CSRFToken || signed.User.PublicID == "" || numericID.MatchString(changed.Body.String()) {
		t.Fatalf("session not rotated: %s", changed.Body.String())
	}
	if !storedPasswordIs(t, db, session.UserID, newPassword) {
		t.Fatal("new password not saved")
	}
	// Every earlier session is gone; the new one works with its CSRF token.
	call(t, h, "GET", "/api/auth/me", "", "", cookie, 401)
	call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 401)
	call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, newPassword), signed.CSRF, rotated, 204)
	// Another account is untouched.
	call(t, h, "GET", "/api/auth/me", "", "", stranger.Cookie(), 200)

	var email string
	if err := db.QueryRowContext(ctx, "SELECT email FROM users WHERE id=?", session.UserID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, factory.DefaultPassword), "", nil, 401)
	call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, newPassword), "", nil, 200)
}

// Password checks share the account's sign-in limit.
func TestPasswordRateLimit(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	var email string
	if err := db.QueryRowContext(ctx, "SELECT email FROM users WHERE id=?", session.UserID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.AuthAttempts.State(factory.ForAccount(email)).State(factory.Exhausted(10)).CreateOne(ctx, db, f); err != nil {
		t.Fatal(err)
	}
	locked := call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword), session.CSRFToken, session.Cookie(), 429)
	if locked.Header().Get("Retry-After") != "900" {
		t.Fatal("rate-limited check without Retry-After")
	}
	call(t, h, "PUT", "/api/auth/password", changeBody(factory.DefaultPassword, newPassword), session.CSRFToken, session.Cookie(), 429)
	if !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatal("rate-limited change altered the password")
	}
}

// A request from another site is refused before the password is looked at.
func TestPasswordChangeNeedsOrigin(t *testing.T) {
	s, db := integrationServer(t)
	session := signedIn(t, db, factory.TestFaker(t))
	r := httptest.NewRequest("PUT", "/api/auth/password", strings.NewReader(changeBody(factory.DefaultPassword, newPassword)))
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.AddCookie(session.Cookie())
	w := httptest.NewRecorder()
	s.Routes().ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatalf("cross-origin change: %d", w.Code)
	}
}
