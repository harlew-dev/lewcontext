package account_test

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

// storedPasswordIs reports whether the user's stored hash matches password.
func storedPasswordIs(t *testing.T, db bun.IDB, userID int64, password string) bool {
	t.Helper()
	var hash string
	if err := db.NewSelect().TableExpr("users").Column("password_hash").Where("id = ?", userID).Scan(context.Background(), &hash); err != nil {
		t.Fatal(err)
	}
	return auth.CheckPassword(password, hash)
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
	if apitest.ErrorField(t, wrong) != "password" || !strings.Contains(wrong.Body.String(), httpapi.WrongCurrentPassword) {
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
	apitest.Call(t, h, "PUT", path, apitest.ChangeBody(factory.DefaultPassword, apitest.NewPassword), session.CSRFToken, cookie, 405)
	apitest.Call(t, h, "PUT", "/api/auth/password-reset", `{"token":"x","newPassword":"y"}`, "", nil, 405)
	apitest.Call(t, h, "PATCH", path, apitest.ChangeBody(factory.DefaultPassword, apitest.NewPassword), "", nil, 401)
	apitest.Call(t, h, "PATCH", path, apitest.ChangeBody(factory.DefaultPassword, apitest.NewPassword), "", cookie, 403)
	for _, tc := range []struct{ body, field string }{
		{apitest.ChangeBody("not my password", apitest.NewPassword), "currentPassword"},
		{apitest.ChangeBody("", apitest.NewPassword), "currentPassword"},
		{apitest.ChangeBody(factory.DefaultPassword, "too short"), "newPassword"},
		{apitest.ChangeBody(factory.DefaultPassword, strings.Repeat("x", 129)), "newPassword"},
		{apitest.ChangeBody(factory.DefaultPassword, factory.DefaultPassword), "newPassword"},
		// Without MFA a code doesn't stand in for the password.
		{fmt.Sprintf(`{"newPassword":%q,"code":"123456"}`, apitest.NewPassword), "currentPassword"},
	} {
		refused := apitest.Call(t, h, "PATCH", path, tc.body, session.CSRFToken, cookie, 422)
		if apitest.ErrorField(t, refused) != tc.field {
			t.Fatalf("%s: want field %s: %s", tc.body, tc.field, refused.Body.String())
		}
	}
	// Refused changes leave the password and every session alone.
	if !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatal("refused change altered the password")
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 200)

	changed := apitest.Call(t, h, "PATCH", path, apitest.ChangeBody(factory.DefaultPassword, apitest.NewPassword), session.CSRFToken, cookie, 200)
	var signed apitest.SessionResponse
	if err := json.Unmarshal(changed.Body.Bytes(), &signed); err != nil {
		t.Fatal(err)
	}
	rotated := changed.Result().Cookies()[0]
	if rotated.Value == cookie.Value || signed.CSRF == "" || signed.CSRF == session.CSRFToken || signed.User.PublicID == "" || apitest.NumericID.MatchString(changed.Body.String()) {
		t.Fatalf("session not rotated: %s", changed.Body.String())
	}
	if !storedPasswordIs(t, db, session.UserID, apitest.NewPassword) {
		t.Fatal("new password not saved")
	}
	// Every earlier session is gone; the new one works with its CSRF token.
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 401)
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 401)
	apitest.Call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, apitest.NewPassword), signed.CSRF, rotated, 204)
	// Another account is untouched.
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", stranger.Cookie(), 200)

	email := apitest.EmailOf(t, db, session.UserID)
	apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, factory.DefaultPassword), "", nil, 401)
	apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, email, apitest.NewPassword), "", nil, 200)
}

// With MFA on, a code confirms the change instead of the current password.
func TestChangePasswordWithMFA(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	session := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	cookie, csrf := session.Cookie(), session.CSRFToken
	const path = "/api/auth/password"
	body := func(newPassword, code string) string {
		return fmt.Sprintf(`{"newPassword":%q,"code":%q}`, newPassword, code)
	}
	refuse := func(body, field string) {
		t.Helper()
		if w := apitest.Call(t, h, "PATCH", path, body, csrf, cookie, 422); apitest.ErrorField(t, w) != field {
			t.Fatalf("%s: want field %s: %s", body, field, w.Body.String())
		}
	}

	// The right password isn't enough, and isn't counted as a check.
	refuse(apitest.ChangeBody(factory.DefaultPassword, apitest.NewPassword), "code")
	refuse(body(apitest.NewPassword, "  "), "code")
	if n := len(apitest.EventsNamed(t, db, "auth.password_checked")) + len(apitest.EventsNamed(t, db, "auth.mfa.checked")); n != 0 {
		t.Fatalf("a missing code recorded %d checks", n)
	}
	refuse(body(apitest.NewPassword, "000000"), "code")
	if e := apitest.OnlyEvent(t, db, "auth.mfa.checked"); e.Payload["ok"] != false || e.Payload["step"] != "password_update" {
		t.Fatalf("auth.mfa.checked: %+v", e)
	}
	// Refusing the new password rolls back, so the code still works after.
	code := a.CodeAt(0)
	refuse(body(factory.DefaultPassword, code), "newPassword")
	if !storedPasswordIs(t, db, a.User.ID, factory.DefaultPassword) {
		t.Fatal("a refused change altered the password")
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)

	changed := apitest.Call(t, h, "PATCH", path, body(apitest.NewPassword, code), csrf, cookie, 200)
	if !storedPasswordIs(t, db, a.User.ID, apitest.NewPassword) {
		t.Fatal("new password not saved")
	}
	if n := len(apitest.EventsNamed(t, db, "auth.password_checked")); n != 0 {
		t.Fatalf("the password was recorded as checked %d times", n)
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 401)
	var signed apitest.SessionResponse
	apitest.Must(0, json.Unmarshal(changed.Body.Bytes(), &signed))(t)
	cookie, csrf = changed.Result().Cookies()[0], signed.CSRF

	// Each code works once; a recovery code works too, and is used up.
	refuse(body("another new password", code), "code")
	apitest.Call(t, h, "PATCH", path, body("another new password", a.Codes[0].Code), csrf, cookie, 200)
	checks := apitest.EventsNamed(t, db, "auth.mfa.checked")
	if last := checks[len(checks)-1]; last.Payload["ok"] != true || last.Payload["method"] != "recovery_code" || last.Payload["recoveryCodesLeft"] != 1.0 {
		t.Fatalf("auth.mfa.checked: %+v", last)
	}

	// Signing in with the new password still asks for a code.
	w := apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"another new password"}`, a.User.Email), "", nil, 200)
	if !strings.Contains(w.Body.String(), `"mfaRequired":true`) || apitest.CookieNamed(w, auth.CookieName(false)) != nil {
		t.Fatalf("signing in after the change: %s", w.Body.String())
	}
}

// Codes for a password change share the account's code limit.
func TestChangePasswordWithMFARateLimit(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	session := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("mfa:per-account", a.User.PublicID)).State(factory.Exhausted(5)).CreateOne(ctx, db, f))(t)
	body := fmt.Sprintf(`{"newPassword":%q,"code":%q}`, apitest.NewPassword, a.CodeAt(0))
	apitest.Call(t, h, "PATCH", "/api/auth/password", body, session.CSRFToken, session.Cookie(), 429)
	if !storedPasswordIs(t, db, a.User.ID, factory.DefaultPassword) {
		t.Fatal("rate-limited change altered the password")
	}
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
	apitest.Call(t, h, "PATCH", "/api/auth/password", apitest.ChangeBody(factory.DefaultPassword, apitest.NewPassword), session.CSRFToken, session.Cookie(), 429)
	if !storedPasswordIs(t, db, session.UserID, factory.DefaultPassword) {
		t.Fatal("rate-limited change altered the password")
	}
}

// A request from another site is refused before the password is looked at.
func TestPasswordChangeNeedsOrigin(t *testing.T) {
	s, db := server(t)
	session := apitest.SignedIn(t, db, factory.TestFaker(t))
	r := httptest.NewRequest("PATCH", "/api/auth/password", strings.NewReader(apitest.ChangeBody(factory.DefaultPassword, apitest.NewPassword)))
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
