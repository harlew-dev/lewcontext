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
	"lewcontext/api/internal/httpapi/router"
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
	if err := db.NewSelect().TableExpr("users").Column("password_hash").Where("id = ?", userID).Scan(context.Background(), &hash); err != nil {
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
	s, db := server(t)
	h := router.New(s)
	session := apitest.SignedIn(t, db, factory.TestFaker(t))
	cookie := session.Cookie()
	body := fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword)
	apitest.Call(t, h, "POST", "/api/auth/password/verify", body, "", nil, 401)
	apitest.Call(t, h, "POST", "/api/auth/password/verify", body, "", cookie, 403)
	apitest.Call(t, h, "POST", "/api/auth/password/verify", body, session.CSRFToken, cookie, 204)
	wrong := apitest.Call(t, h, "POST", "/api/auth/password/verify", `{"password":"not my password"}`, session.CSRFToken, cookie, 422)
	if errorField(t, wrong) != "password" || !strings.Contains(wrong.Body.String(), httpapi.WrongCurrentPassword) {
		t.Fatalf("wrong password error: %s", wrong.Body.String())
	}
	apitest.Call(t, h, "POST", "/api/auth/password/verify", `{"password":""}`, session.CSRFToken, cookie, 422)
	// Checking the password never signs anyone out.
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)
}

func TestChangePassword(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	cookie := session.Cookie()
	other := apitest.Must(factory.SessionFor(ctx, db, f, session.UserID))(t)
	stranger := apitest.SignedIn(t, db, f)
	const path = "/api/auth/password"

	// Both password routes are updates, so PATCH; PUT isn't routed.
	apitest.Call(t, h, "PUT", path, changeBody(factory.DefaultPassword, newPassword), session.CSRFToken, cookie, 405)
	apitest.Call(t, h, "PUT", "/api/auth/password-reset", `{"token":"x","newPassword":"y"}`, "", nil, 405)
	apitest.Call(t, h, "PATCH", path, changeBody(factory.DefaultPassword, newPassword), "", nil, 401)
	apitest.Call(t, h, "PATCH", path, changeBody(factory.DefaultPassword, newPassword), "", cookie, 403)
	for _, tc := range []struct{ body, field string }{
		{changeBody("not my password", newPassword), "currentPassword"},
		{changeBody("", newPassword), "currentPassword"},
		{changeBody(factory.DefaultPassword, "too short"), "newPassword"},
		{changeBody(factory.DefaultPassword, strings.Repeat("x", 129)), "newPassword"},
		{changeBody(factory.DefaultPassword, factory.DefaultPassword), "newPassword"},
	} {
		refused := apitest.Call(t, h, "PATCH", path, tc.body, session.CSRFToken, cookie, 422)
		if errorField(t, refused) != tc.field {
			t.Fatalf("%s: want field %s: %s", tc.body, tc.field, refused.Body.String())
		}
	}
	// Refused changes leave the password and every session alone.
	if !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatal("refused change altered the password")
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 200)

	changed := apitest.Call(t, h, "PATCH", path, changeBody(factory.DefaultPassword, newPassword), session.CSRFToken, cookie, 200)
	var signed apitest.SessionResponse
	if err := json.Unmarshal(changed.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	rotated := changed.Result().Cookies()[0]
	if rotated.Value == cookie.Value || signed.CSRF == "" || signed.CSRF == session.CSRFToken || signed.User.PublicID == "" || apitest.NumericID.MatchString(changed.Body.String()) {
		t.Fatalf("session not rotated: %s", changed.Body.String())
	}
	if !storedPasswordIs(t, db, session.UserID, newPassword) {
		t.Fatal("new password not saved")
	}
	// Every earlier session is gone; the new one works with its CSRF token.
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 401)
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 401)
	apitest.Call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, newPassword), signed.CSRF, rotated, 204)
	// Another account is untouched.
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", stranger.Cookie(), 200)

	email := apitest.EmailOf(t, db, session.UserID)
	apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, factory.DefaultPassword), "", nil, 401)
	apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, newPassword), "", nil, 200)
}

// Password checks share the account's sign-in limit.
func TestPasswordRateLimit(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	email := apitest.EmailOf(t, db, session.UserID)
	apitest.Must(factory.RateLimits.State(factory.ForAccount(email)).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	locked := apitest.Call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword), session.CSRFToken, session.Cookie(), 429)
	if locked.Header().Get("Retry-After") != "900" {
		t.Fatal("rate-limited check without Retry-After")
	}
	apitest.Call(t, h, "PATCH", "/api/auth/password", changeBody(factory.DefaultPassword, newPassword), session.CSRFToken, session.Cookie(), 429)
	if !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatal("rate-limited change altered the password")
	}
}

// A request from another site is refused before the password is looked at.
func TestPasswordChangeNeedsOrigin(t *testing.T) {
	s, db := server(t)
	session := apitest.SignedIn(t, db, factory.TestFaker(t))
	r := httptest.NewRequest("PATCH", "/api/auth/password", strings.NewReader(changeBody(factory.DefaultPassword, newPassword)))
	r.Header.Set("Origin", "https://evil.test")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.AddCookie(session.Cookie())
	w := httptest.NewRecorder()
	router.New(s).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatalf("cross-origin change: %d", w.Code)
	}
}
