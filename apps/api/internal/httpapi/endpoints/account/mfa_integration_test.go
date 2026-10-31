package account_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// mfaEnabledAt reads the session's user's mfaEnabledAt from /api/auth/me.
func mfaEnabledAt(t *testing.T, h http.Handler, cookie *http.Cookie) *time.Time {
	t.Helper()
	var body struct {
		User struct {
			MFAEnabledAt *time.Time `json:"mfaEnabledAt"`
		} `json:"user"`
	}
	w := apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)
	if !strings.Contains(w.Body.String(), `"mfaEnabledAt":`) {
		t.Fatalf("no mfaEnabledAt: %s", w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.User.MFAEnabledAt
}

func TestMFASetup(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	cookie, csrf := session.Cookie(), session.CSRFToken
	other := apitest.Must(factory.SessionFor(ctx, db, f, session.UserID))(t)
	password := fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword)

	apitest.Call(t, h, "POST", "/api/auth/mfa/setup", password, "", nil, 401)
	apitest.Call(t, h, "POST", "/api/auth/mfa/setup", password, "", cookie, 403)
	wrong := apitest.Call(t, h, "POST", "/api/auth/mfa/setup", `{"password":"not my password"}`, csrf, cookie, 422)
	if apitest.ErrorField(t, wrong) != "password" {
		t.Fatalf("wrong password: %s", wrong.Body.String())
	}
	apitest.Call(t, h, "POST", "/api/auth/mfa", apitest.CodeBody("123456"), csrf, cookie, 409) // nothing pending

	setup := func() []byte {
		var body struct{ Secret, URI string }
		w := apitest.Call(t, h, "POST", "/api/auth/mfa/setup", password, csrf, cookie, 200)
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(body.Secret)
		if err != nil || len(secret) != auth.TOTPSecretSize || !strings.HasPrefix(body.URI, "otpauth://totp/") || !strings.Contains(body.URI, "secret="+body.Secret) {
			t.Fatalf("setup answer: %+v (%v)", body, err)
		}
		return secret
	}
	first := setup()
	second := setup() // starting again replaces the pending secret
	var stored []byte
	apitest.Must(0, db.NewSelect().TableExpr("mfa_totp").Column("secret").Where("user_id = ?", session.UserID).Scan(ctx, &stored))(t)
	if bytes.Contains(stored, second) || bytes.Equal(first, second) {
		t.Fatal("the secret is stored in plain, or setup didn't replace it")
	}
	now := auth.TOTPStep(time.Now())

	refused := apitest.Call(t, h, "POST", "/api/auth/mfa", apitest.CodeBody(auth.TOTPCode(first, now)), csrf, cookie, 422)
	if apitest.ErrorField(t, refused) != "code" {
		t.Fatalf("the replaced secret's code: %s", refused.Body.String())
	}
	if mfaEnabledAt(t, h, cookie) != nil {
		t.Fatal("a wrong code turned MFA on")
	}
	w := apitest.Call(t, h, "POST", "/api/auth/mfa", apitest.CodeBody(auth.TOTPCode(second, now)), csrf, cookie, 201)
	var enabled struct{ RecoveryCodes []string }
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &enabled))(t)
	if len(enabled.RecoveryCodes) != auth.RecoveryCodeCount {
		t.Fatalf("recovery codes: %v", enabled.RecoveryCodes)
	}
	if mfaEnabledAt(t, h, cookie) == nil {
		t.Fatal("MFA isn't on after the right code")
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 401) // other sessions are signed out
	var digests []string
	apitest.Must(0, db.NewSelect().TableExpr("mfa_recovery_codes").Column("code_hash").Where("user_id = ?", session.UserID).Scan(ctx, &digests))(t)
	for _, code := range enabled.RecoveryCodes {
		normal, _ := auth.NormalizeRecoveryCode(code)
		if slices.Contains(digests, code) || slices.Contains(digests, normal) || !slices.Contains(digests, factory.Keys.RecoveryCodeDigest(normal)) {
			t.Fatalf("recovery code %s isn't stored as its keyed digest", code)
		}
	}

	apitest.Call(t, h, "POST", "/api/auth/mfa/setup", password, csrf, cookie, 409)
	apitest.Call(t, h, "POST", "/api/auth/mfa", apitest.CodeBody(auth.TOTPCode(second, now+1)), csrf, cookie, 409)

	e := apitest.OnlyEvent(t, db, "auth.mfa.enabled")
	if e.UserID.Int64 != session.UserID || e.Payload["otherSessionsRevoked"] != float64(1) {
		t.Fatalf("auth.mfa.enabled: %+v", e)
	}
	checks := apitest.EventsNamed(t, db, "auth.mfa.checked")
	if len(checks) != 2 || checks[0].Payload["ok"] != false || checks[1].Payload["ok"] != true || checks[1].Payload["step"] != "enable" || checks[1].Payload["method"] != "totp" {
		t.Fatalf("auth.mfa.checked: %+v", checks)
	}
	if steps := apitest.EventsNamed(t, db, "auth.password_checked"); len(steps) != 3 || steps[0].Payload["step"] != "mfa_setup" {
		t.Fatalf("auth.password_checked: %+v", steps)
	}
}

func TestMFASetupExpires(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	pending := apitest.Must(factory.MFATOTPs.State(factory.PendingTOTP).State(factory.TOTPFor(session.UserID)).State(func(t *factory.MFATOTP) {
		t.CreatedAt = time.Now().Add(-16 * time.Minute)
	}).CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "POST", "/api/auth/mfa", apitest.CodeBody(pending.Code()), session.CSRFToken, session.Cookie(), 409)
	apitest.Must(0, s.Repo.Cleanup(ctx))(t)
	if apitest.CountRows(t, db, "mfa_totp", session.UserID) != 0 {
		t.Fatal("cleanup kept an abandoned setup")
	}
}

func TestMFALogin(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	mfaCookie := auth.MFACookieName(false)

	t.Run("a code starts the session", func(t *testing.T) {
		a := apitest.WithMFA(t, db, f)
		challenge := apitest.StartMFALogin(t, h, a.User.Email)
		apitest.Call(t, h, "GET", "/api/auth/me", "", "", challenge, 401)
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", nil, 401)
		wrong := apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody("000000"), "", challenge, 422)
		if apitest.ErrorField(t, wrong) != "code" {
			t.Fatalf("wrong code: %s", wrong.Body.String())
		}
		w := apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)[:3]+" "+a.CodeAt(0)[3:]), "", challenge, 200)
		session := apitest.CookieNamed(w, auth.CookieName(false))
		if session == nil || apitest.CookieNamed(w, mfaCookie).MaxAge >= 0 {
			t.Fatalf("cookies after the code: %v", w.Result().Cookies())
		}
		if mfaEnabledAt(t, h, session) == nil {
			t.Fatal("the session's user has MFA off")
		}
		// The challenge is used up, and so is the code.
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", challenge, 401)
		again := apitest.StartMFALogin(t, h, a.User.Email)
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", again, 422)

		attempt := apitest.EventsNamed(t, db, "auth.login_attempted")
		if attempt[0].Payload["result"] != "mfa_required" || attempt[0].Payload["user"] != a.User.PublicID {
			t.Fatalf("auth.login_attempted: %+v", attempt[0])
		}
		if logins := apitest.EventsNamed(t, db, "auth.logged_in"); len(logins) != 1 {
			t.Fatalf("auth.logged_in: %+v", logins)
		}
	})

	t.Run("a recovery code works once", func(t *testing.T) {
		a := apitest.WithMFA(t, db, f)
		code := strings.ToUpper(a.Codes[0].Code)
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(code), "", apitest.StartMFALogin(t, h, a.User.Email), 200)
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(code), "", apitest.StartMFALogin(t, h, a.User.Email), 422)
		var used []apitest.Event
		for _, e := range apitest.EventsNamed(t, db, "auth.mfa.checked") {
			if e.Payload["user"] == a.User.PublicID {
				used = append(used, e)
			}
		}
		if len(used) != 2 || used[0].Payload["method"] != "recovery_code" || used[0].Payload["recoveryCodesLeft"] != float64(1) || used[1].Payload["ok"] != false {
			t.Fatalf("auth.mfa.checked: %+v", used)
		}
	})

	t.Run("the fifth wrong code ends the challenge", func(t *testing.T) {
		a := apitest.WithMFA(t, db, f)
		challenge := apitest.StartMFALogin(t, h, a.User.Email)
		for range store.MaxChallengeAttempts - 1 {
			apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody("not a code"), "", challenge, 422)
		}
		w := apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody("000000"), "", challenge, 401)
		if !strings.Contains(w.Body.String(), httpapi.MFATooManyCodes) || apitest.CookieNamed(w, mfaCookie).MaxAge >= 0 {
			t.Fatalf("fifth wrong code: %s %v", w.Body.String(), w.Result().Cookies())
		}
		if apitest.CountRows(t, db, "mfa_challenges", a.User.ID) != 0 {
			t.Fatal("the challenge outlived its tries")
		}
	})

	t.Run("expired, another flow's, or MFA turned off since", func(t *testing.T) {
		ctx := context.Background()
		a := apitest.WithMFA(t, db, f)
		expired := apitest.Must(factory.MFAChallenges.State(factory.ChallengeFor(a.User.ID)).State(factory.ExpiredChallenge).CreateOne(ctx, db, f))(t)
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", expired.Cookie(), 401)
		admin := apitest.Must(factory.MFAChallenges.State(factory.ChallengeFor(a.User.ID)).State(func(c *factory.MFAChallenge) { c.Purpose = "admin" }).CreateOne(ctx, db, f))(t)
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", admin.Cookie(), 401)
		challenge := apitest.StartMFALogin(t, h, a.User.Email)
		apitest.Must(db.NewDelete().TableExpr("mfa_totp").Where("user_id = ?", a.User.ID).Exec(ctx))(t)
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", challenge, 401)
		malformed := &http.Cookie{Name: mfaCookie, Value: "short"}
		apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", malformed, 401)
	})

	t.Run("MFA off signs in with the password, as before", func(t *testing.T) {
		user := apitest.Must(factory.Users.CreateOne(context.Background(), db, f))(t)
		w := apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(user.Email), "", nil, 200)
		if apitest.CookieNamed(w, auth.CookieName(false)) == nil || strings.Contains(w.Body.String(), "mfaRequired") {
			t.Fatalf("login without MFA: %s", w.Body.String())
		}
	})

	t.Run("a password reset leaves MFA on", func(t *testing.T) {
		ctx := context.Background()
		a := apitest.WithMFA(t, db, f)
		reset := apitest.Must(factory.PasswordResets.State(factory.ResetFor(a.User.ID)).CreateOne(ctx, db, f))(t)
		apitest.Call(t, h, "PATCH", "/api/auth/password-reset", fmt.Sprintf(`{"token":%q,"newPassword":"a brand new passphrase"}`, reset.Token), "", nil, 200)
		w := apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"a brand new passphrase"}`, a.User.Email), "", nil, 200)
		if !strings.Contains(w.Body.String(), "mfaRequired") {
			t.Fatalf("after a reset: %s", w.Body.String())
		}
	})
}

func TestMFARateLimit(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	challenge := apitest.StartMFALogin(t, h, a.User.Email)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("mfa:per-account", a.User.PublicID)).State(factory.Exhausted(5)).CreateOne(ctx, db, f))(t)
	w := apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", challenge, 429)
	apitest.RetryWithin(t, w, 15*time.Minute)
	if !strings.Contains(w.Body.String(), "Too many attempts") {
		t.Fatalf("refusal: %s", w.Body.String())
	}
	apitest.Reached(t, s.Repo.DB, "mfa:per-account:block")
	if apitest.CountRows(t, db, "mfa_challenges", a.User.ID) != 1 {
		t.Fatal("a refused request spent the challenge")
	}
}

func TestDisableMFA(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	apitest.Must(factory.MFAChallenges.State(factory.ChallengeFor(a.User.ID)).CreateOne(ctx, db, f))(t)
	session := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	cookie, csrf := session.Cookie(), session.CSRFToken
	body := func(password, code string) string {
		return fmt.Sprintf(`{"password":%q,"code":%q}`, password, code)
	}

	apitest.Call(t, h, "DELETE", "/api/auth/mfa", body(factory.DefaultPassword, a.CodeAt(0)), "", cookie, 403)
	if w := apitest.Call(t, h, "DELETE", "/api/auth/mfa", body("not my password", a.CodeAt(0)), csrf, cookie, 422); apitest.ErrorField(t, w) != "password" {
		t.Fatalf("wrong password: %s", w.Body.String())
	}
	if w := apitest.Call(t, h, "DELETE", "/api/auth/mfa", body(factory.DefaultPassword, "000000"), csrf, cookie, 422); apitest.ErrorField(t, w) != "code" {
		t.Fatalf("wrong code: %s", w.Body.String())
	}
	if mfaEnabledAt(t, h, cookie) == nil {
		t.Fatal("refused requests turned MFA off")
	}
	apitest.Call(t, h, "DELETE", "/api/auth/mfa", body(factory.DefaultPassword, a.Codes[0].Code), csrf, cookie, 204)
	if mfaEnabledAt(t, h, cookie) != nil {
		t.Fatal("MFA still on")
	}
	for _, table := range []string{"mfa_totp", "mfa_recovery_codes", "mfa_challenges"} {
		if n := apitest.CountRows(t, db, table, a.User.ID); n != 0 {
			t.Fatalf("%s kept %d rows", table, n)
		}
	}
	if e := apitest.OnlyEvent(t, db, "auth.mfa.disabled"); e.Payload["reason"] != "user" || e.UserID.Int64 != a.User.ID {
		t.Fatalf("auth.mfa.disabled: %+v", e)
	}
	apitest.Call(t, h, "DELETE", "/api/auth/mfa", body(factory.DefaultPassword, a.CodeAt(0)), csrf, cookie, 409)
	apitest.Call(t, h, "POST", "/api/auth/mfa/recovery-codes", body(factory.DefaultPassword, a.CodeAt(0)), csrf, cookie, 409)
	w := apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(a.User.Email), "", nil, 200)
	if apitest.CookieNamed(w, auth.CookieName(false)) == nil {
		t.Fatal("signing in still asks for a code")
	}
}

func TestNewRecoveryCodes(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	session := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	body := fmt.Sprintf(`{"password":%q,"code":%q}`, factory.DefaultPassword, a.CodeAt(0))

	w := apitest.Call(t, h, "POST", "/api/auth/mfa/recovery-codes", body, session.CSRFToken, session.Cookie(), 201)
	var renewed struct{ RecoveryCodes []string }
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &renewed))(t)
	if len(renewed.RecoveryCodes) != auth.RecoveryCodeCount || apitest.CountRows(t, db, "mfa_recovery_codes", a.User.ID) != auth.RecoveryCodeCount {
		t.Fatalf("renewed: %v", renewed.RecoveryCodes)
	}
	if e := apitest.OnlyEvent(t, db, "auth.recovery_codes.created"); e.Payload["count"] != float64(10) {
		t.Fatalf("auth.recovery_codes.created: %+v", e)
	}
	// The old codes are gone; a new one signs in.
	apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.Codes[1].Code), "", apitest.StartMFALogin(t, h, a.User.Email), 422)
	apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(renewed.RecoveryCodes[0]), "", apitest.StartMFALogin(t, h, a.User.Email), 200)
}

func TestResetMFA(t *testing.T) {
	s, db := server(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)

	user := apitest.Must(s.Repo.ResetMFA(ctx, a.User.Email))(t)
	if user.PublicID != a.User.PublicID {
		t.Fatalf("reset %+v", user)
	}
	for _, table := range []string{"mfa_totp", "mfa_recovery_codes", "sessions"} {
		if n := apitest.CountRows(t, db, table, a.User.ID); n != 0 {
			t.Fatalf("%s kept %d rows", table, n)
		}
	}
	e := apitest.OnlyEvent(t, db, "auth.mfa.disabled")
	if e.Payload["reason"] != "reset" || e.ActorID.Valid {
		t.Fatalf("auth.mfa.disabled: %+v", e)
	}
	if _, err := s.Repo.ResetMFA(ctx, a.User.Email); !errors.Is(err, store.ErrMFANotEnabled) {
		t.Fatalf("a second reset: %v", err)
	}
	if _, err := s.Repo.ResetMFA(ctx, "nobody@example.test"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("an unknown email: %v", err)
	}
}

// Deleting the account deletes its MFA rows with it.
func TestDeletingAnAccountDeletesItsMFA(t *testing.T) {
	_, db := server(t)
	ctx := context.Background()
	a := apitest.WithMFA(t, db, factory.TestFaker(t))
	apitest.Must(factory.MFAChallenges.State(factory.ChallengeFor(a.User.ID)).CreateOne(ctx, db, factory.TestFaker(t)))(t)
	apitest.Must(db.NewDelete().TableExpr("users").Where("id = ?", a.User.ID).Exec(ctx))(t)
	for _, table := range []string{"mfa_totp", "mfa_recovery_codes", "mfa_challenges"} {
		if n := apitest.CountRows(t, db, table, a.User.ID); n != 0 {
			t.Fatalf("%s kept %d rows", table, n)
		}
	}
}
