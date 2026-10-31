package httpapi_test

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// adminSend makes a request as the admin pages would: X-Admin-Key, and any
// cookies (the admin session, a challenge, an app session).
func adminSend(h http.Handler, method, path, body, key, csrf string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "http://localhost:3000")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", apitest.UserAgent)
	if key != "" {
		r.Header.Set("X-Admin-Key", key)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	r.RemoteAddr = "127.0.0.1:1234"
	for _, c := range cookies {
		if c != nil {
			r.AddCookie(c)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// adminCall is adminSend with the test servers' key, failing unless the
// status is want.
func adminCall(t *testing.T, h http.Handler, method, path, body, csrf string, want int, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	w := adminSend(h, method, path, body, factory.AdminRouteKey, csrf, cookies...)
	if w.Code != want {
		t.Fatalf("%s %s: want %d got %d: %s", method, path, want, w.Code, w.Body.String())
	}
	return w
}

// withAdmin is a user with MFA on who is an admin.
func withAdmin(t *testing.T, db bun.IDB, f *gofakeit.Faker) apitest.MFAAccount {
	t.Helper()
	a := apitest.WithMFA(t, db, f)
	apitest.Must(factory.Admins.State(factory.AdminFor(a.User.ID)).CreateOne(context.Background(), db, f))(t)
	return a
}

// adminSignedIn is a live admin session, without the sign-in endpoints.
func adminSignedIn(t *testing.T, db bun.IDB, f *gofakeit.Faker) (apitest.MFAAccount, factory.AdminSession) {
	t.Helper()
	a := withAdmin(t, db, f)
	session := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).CreateOne(context.Background(), db, f))(t)
	return a, session
}

// adminLogin runs the door's password step and returns its answer.
func adminLogin(t *testing.T, h http.Handler, email string, want int) *httptest.ResponseRecorder {
	t.Helper()
	return adminCall(t, h, "POST", "/api/admin/login", apitest.LoginBody(email), "", want)
}

// adminRoutesList is every admin API route, for the key and session checks.
var adminRoutesList = []struct{ method, path string }{
	{"GET", "/api/admin/me"},
	{"POST", "/api/admin/login"},
	{"POST", "/api/admin/login/mfa/setup"},
	{"POST", "/api/admin/login/mfa"},
	{"POST", "/api/admin/logout"},
	{"GET", "/api/admin/users"},
	{"GET", "/api/admin/users/usr_2yEHYLwiwPVhLW"},
	{"GET", "/api/admin/workspaces"},
	{"GET", "/api/admin/workspaces/wsp_2yEHYLwiwPVhLW"},
	{"GET", "/api/admin/workspaces/wsp_2yEHYLwiwPVhLW/projects"},
	{"GET", "/api/admin/events"},
	{"GET", "/api/admin/events/evt_2yEHYLwiwPVhLW"},
	{"GET", "/api/admin/catalogue"},
	{"GET", "/api/admin/mail"},
	{"GET", "/api/admin/deliveries"},
	{"PUT", "/api/admin/login"},
	{"GET", "/api/admin/nothing"},
}

// sameAnswer fails unless two responses have the same status, headers and
// body.
func sameAnswer(t *testing.T, name string, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code || got.Body.String() != want.Body.String() {
		t.Fatalf("%s: %d %q, want %d %q", name, got.Code, got.Body.String(), want.Code, want.Body.String())
	}
	if fmt.Sprint(got.Header()) != fmt.Sprint(want.Header()) {
		t.Fatalf("%s headers: %v, want %v", name, got.Header(), want.Header())
	}
}

// Without the right X-Admin-Key, every admin route answers exactly as an
// unknown API route, even with a live admin session; admin off too.
func TestAdminKey(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)

	unknown := adminSend(h, "GET", "/api/nowhere", "", "", "")
	if unknown.Code != 404 || strings.TrimSpace(unknown.Body.String()) != `{"error":"Not found."}` || unknown.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("an unknown route: %d %q %v", unknown.Code, unknown.Body.String(), unknown.Header())
	}
	off := router.New(apitest.Must(httpapi.New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000", AppKey: factory.AppKey}))(t))
	for _, route := range adminRoutesList {
		want := adminSend(h, route.method, "/api/nowhere", "{}", "", "")
		for name, key := range map[string]string{"no key": "", "wrong key": factory.AdminRouteKey + "x", "a near miss": strings.ToUpper(factory.AdminRouteKey)} {
			got := adminSend(h, route.method, route.path, "{}", key, session.CSRFToken, session.Cookie())
			sameAnswer(t, route.method+" "+route.path+" with "+name, got, want)
		}
		got := adminSend(off, route.method, route.path, "{}", factory.AdminRouteKey, session.CSRFToken, session.Cookie())
		sameAnswer(t, route.method+" "+route.path+" with admin off", got, want)
	}
	// A bad Origin is refused the same way, with or without the key.
	for _, path := range []string{"/api/nowhere", "/api/admin/login"} {
		r := httptest.NewRequest("POST", path, strings.NewReader("{}"))
		r.Header.Set("Origin", "https://evil.example")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("%s with a bad Origin: %d", path, w.Code)
		}
	}
	// A known path with the wrong method keeps its 405.
	apitest.Call(t, h, "PUT", "/api/auth/login", "{}", "", nil, 405)

	// The right key and the session: in.
	adminCall(t, h, "GET", "/api/admin/me", "", "", 200, session.Cookie())

	// Rotating the key (another server, same database) ends the session.
	rotated := router.New(apitest.Must(httpapi.New(&store.Store{DB: db}, config.Config{Origin: "http://localhost:3000", AppKey: factory.AppKey, AdminRouteKey: factory.AdminRouteKey + "-rotated"}))(t))
	w := adminSend(rotated, "GET", "/api/admin/me", "", factory.AdminRouteKey+"-rotated", "", session.Cookie())
	if w.Code != 401 {
		t.Fatalf("the old key's session after rotation: %d %s", w.Code, w.Body.String())
	}
	if w := adminSend(rotated, "GET", "/api/admin/me", "", factory.AdminRouteKey, "", session.Cookie()); w.Code != 404 {
		t.Fatalf("the old key after rotation: %d", w.Code)
	}
}

// Every refusal at the door's password step is the same 401, with its
// reason only in the event.
func TestAdminLoginRefusals(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	notAdmin := apitest.WithMFA(t, db, f)
	noMFA := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Admins.State(factory.AdminFor(noMFA.ID)).State(factory.ClosedSetupWindow).CreateOne(ctx, db, f))(t)
	admin := withAdmin(t, db, f)

	cases := map[string]struct{ body, result string }{
		"not an admin":         {apitest.LoginBody(notAdmin.User.Email), "not_admin"},
		"an admin without MFA": {apitest.LoginBody(noMFA.Email), "mfa_off"},
		"a wrong password":     {fmt.Sprintf(`{"email":%q,"password":"not the password"}`, admin.User.Email), "wrong_password"},
		"an unknown email":     {apitest.LoginBody("nobody@example.test"), "unknown_account"},
		"a malformed email":    {`{"email":"nope","password":"x"}`, ""},
	}
	var first string
	for name, c := range cases {
		w := adminCall(t, h, "POST", "/api/admin/login", c.body, "", 401)
		if first == "" {
			first = w.Body.String()
		}
		if w.Body.String() != first || apitest.CookieNamed(w, auth.AdminMFACookieName(false)) != nil || apitest.CookieNamed(w, auth.AdminCookieName(false)) != nil {
			t.Fatalf("%s: %s %v", name, w.Body.String(), w.Result().Cookies())
		}
	}
	results := map[string]int{}
	for _, e := range apitest.EventsNamed(t, db, "admin.login_attempted") {
		results[e.Payload["result"].(string)]++
		if e.ActorID.Valid || e.UserID.Valid || e.IP != "127.0.0.1" {
			t.Fatalf("scope or ip: %+v", e)
		}
	}
	if fmt.Sprint(results) != "map[mfa_off:1 not_admin:1 unknown_account:1 wrong_password:1]" {
		t.Fatalf("results: %v", results)
	}
	if len(apitest.EventsNamed(t, db, "auth.login_attempted")) != 0 {
		t.Fatal("the door recorded app login attempts")
	}
}

// An admin with MFA on signs in with the password, then a code, and gets an
// admin session but no app session.
func TestAdminLogin(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a := withAdmin(t, db, f)

	w := adminLogin(t, h, a.User.Email, 200)
	if strings.TrimSpace(w.Body.String()) != `{"mfaRequired":true}` || apitest.CookieNamed(w, auth.AdminCookieName(false)) != nil {
		t.Fatalf("the password step: %s", w.Body.String())
	}
	challenge := apitest.CookieNamed(w, auth.AdminMFACookieName(false))
	if challenge == nil || !challenge.HttpOnly || challenge.SameSite != http.SameSiteStrictMode || challenge.MaxAge != 300 {
		t.Fatalf("challenge cookie: %+v", challenge)
	}
	if e := apitest.OnlyEvent(t, db, "admin.login_attempted"); e.Payload["result"] != "mfa_required" || e.Payload["user"] != a.User.PublicID {
		t.Fatalf("attempt: %+v", e)
	}
	// The app's login can't use the door's challenge, even in its cookie.
	appCookie := &http.Cookie{Name: auth.MFACookieName(false), Value: challenge.Value}
	apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", appCookie, 401)
	// The setup step refuses a code challenge, and ends it.
	adminCall(t, h, "POST", "/api/admin/login/mfa/setup", "{}", "", 401, challenge)
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", 401, challenge)

	challenge = apitest.CookieNamed(adminLogin(t, h, a.User.Email, 200), auth.AdminMFACookieName(false))
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody("000000"), "", 422, challenge)
	w = adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", 200, challenge)
	cookie := apitest.CookieNamed(w, auth.AdminCookieName(false))
	if cookie == nil || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || apitest.CookieNamed(w, auth.CookieName(false)) != nil {
		t.Fatalf("admin cookie: %+v, all %v", cookie, w.Result().Cookies())
	}
	var res apitest.SessionResponse
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &res))(t)
	me := adminCall(t, h, "GET", "/api/admin/me", "", "", 200, cookie)
	if res.User.PublicID != a.User.PublicID || !strings.Contains(me.Body.String(), res.CSRF) {
		t.Fatalf("session: %s / %s", w.Body.String(), me.Body.String())
	}
	if e := apitest.OnlyEvent(t, db, "admin.logged_in"); !apitest.Is(e.ActorID, a.User.ID) || e.UserID.Valid || e.Payload["user"] != a.User.PublicID {
		t.Fatalf("admin.logged_in: %+v", e)
	}
	steps := []string{}
	for _, e := range apitest.EventsNamed(t, db, "auth.mfa.checked") {
		steps = append(steps, fmt.Sprintf("%v %v", e.Payload["step"], e.Payload["ok"]))
	}
	if strings.Join(steps, ",") != "admin_login false,admin_login true" {
		t.Fatalf("mfa checks: %v", steps)
	}
	if len(apitest.EventsNamed(t, db, "auth.logged_in")) != 0 || apitest.CountRows(t, db, "sessions", a.User.ID) != 0 {
		t.Fatal("the admin door started an app session")
	}
	// The challenge worked once.
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(a.CodeAt(1)), "", 401, challenge)

	// An app login's challenge can't finish an admin sign-in.
	login := apitest.Must(factory.MFAChallenges.State(factory.ChallengeFor(a.User.ID)).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(a.CodeAt(1)), "", 401, login.AdminCookie())

	// Someone who stops being an admin between the two steps is refused.
	challenge = apitest.CookieNamed(adminLogin(t, h, a.User.Email, 200), auth.AdminMFACookieName(false))
	must2(s.Repo.RevokeAdmin(ctx, a.User.Email))(t)
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(a.CodeAt(1)), "", 401, challenge)
}

// An admin without MFA sets it up at their first admin sign-in, only inside
// the setup window; it's then on for the whole account.
func TestAdminFirstSignInSetsUpMFA(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	granted := apitest.Must(s.Repo.GrantAdmin(ctx, user.Email))(t)
	if granted.MFASetupUntil == nil || time.Until(*granted.MFASetupUntil) < 23*time.Hour {
		t.Fatalf("grant opened no window: %+v", granted)
	}
	if e := apitest.OnlyEvent(t, db, "admin.granted"); e.ActorID.Valid || !apitest.Is(e.UserID, user.ID) || e.Payload["mfaSetupUntil"] == nil {
		t.Fatalf("admin.granted: %+v", e)
	}

	w := adminLogin(t, h, user.Email, 200)
	if strings.TrimSpace(w.Body.String()) != `{"mfaSetupRequired":true}` {
		t.Fatalf("the password step: %s", w.Body.String())
	}
	challenge := apitest.CookieNamed(w, auth.AdminMFACookieName(false))
	if challenge.MaxAge != 900 {
		t.Fatalf("setup challenge: %+v", challenge)
	}
	// The setup challenge can't be answered by the app's login.
	apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody("123456"), "", &http.Cookie{Name: auth.MFACookieName(false), Value: challenge.Value}, 401)

	w = adminCall(t, h, "POST", "/api/admin/login/mfa/setup", "{}", "", 200, challenge)
	var setup struct{ Secret, URI string }
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &setup))(t)
	secret := apitest.Must(base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(setup.Secret))(t)
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody("000000"), "", 422, challenge)
	code := auth.TOTPCode(secret, auth.TOTPStep(time.Now()))
	w = adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(code), "", 201, challenge)
	var done struct {
		RecoveryCodes []string   `json:"recoveryCodes"`
		User          store.User `json:"user"`
	}
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &done))(t)
	if len(done.RecoveryCodes) != 10 || done.User.MFAEnabledAt == nil || apitest.CookieNamed(w, auth.AdminCookieName(false)) == nil {
		t.Fatalf("setup done: %s", w.Body.String())
	}
	admin := apitest.Must(s.Repo.FindAdmin(ctx, user.ID))(t)
	if admin.MFASetupUntil != nil || admin.User.MFAEnabledAt == nil {
		t.Fatalf("after setup: %+v", admin)
	}
	apitest.OnlyEvent(t, db, "auth.mfa.enabled")
	apitest.OnlyEvent(t, db, "admin.logged_in")
	// The app's sign-in now asks for a code too.
	apitest.StartMFALogin(t, h, user.Email)
	// And the next admin sign-in asks for a code, not a setup.
	if w := adminLogin(t, h, user.Email, 200); strings.TrimSpace(w.Body.String()) != `{"mfaRequired":true}` {
		t.Fatalf("second admin sign-in: %s", w.Body.String())
	}

	// A reset reopens the window, and the setup is offered again.
	apitest.Must(s.Repo.ResetMFA(ctx, user.Email))(t)
	if admin := apitest.Must(s.Repo.FindAdmin(ctx, user.ID))(t); !admin.SetupOpen() {
		t.Fatalf("reset didn't reopen the window: %+v", admin)
	}
	challenge = apitest.CookieNamed(adminLogin(t, h, user.Email, 200), auth.AdminMFACookieName(false))
	adminCall(t, h, "POST", "/api/admin/login/mfa/setup", "{}", "", 200, challenge)

	// The window closing mid-setup refuses both steps.
	apitest.Must(db.NewUpdate().TableExpr("admins").Set("mfa_setup_until = now() - interval '1 second'").Where("user_id = ?", user.ID).Exec(ctx))(t)
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody("123456"), "", 401, challenge)
	adminLogin(t, h, user.Email, 401)
	if e := apitest.EventsNamed(t, db, "admin.login_attempted"); e[len(e)-1].Payload["result"] != "mfa_off" {
		t.Fatalf("outside the window: %+v", e[len(e)-1])
	}
	// Granting again opens a new one.
	apitest.Must(s.Repo.GrantAdmin(ctx, user.Email))(t)
	if w := adminLogin(t, h, user.Email, 200); !strings.Contains(w.Body.String(), "mfaSetupRequired") {
		t.Fatalf("after a new grant: %s", w.Body.String())
	}
}

// Admin sessions are their own: an app cookie isn't one, they end idle, at
// their absolute expiry, and with everything that ends admin.
func TestAdminSessions(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a, session := adminSignedIn(t, db, f)

	// An app session's token in the admin cookie, and the other way round.
	app := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, auth.AdminCookie(app.Token, false))
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, app.Cookie())
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", auth.Cookie(session.Token, false), 401)
	// A normal app session is refused by the data routes, key or not.
	adminCall(t, h, "GET", "/api/admin/users", "", "", 401, app.Cookie())

	for name, state := range map[string]func(*factory.AdminSession){"idle": factory.AdminIdle, "expired": factory.AdminExpired} {
		stale := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).State(state).CreateOne(ctx, db, f))(t)
		if w := adminSend(h, "GET", "/api/admin/me", "", factory.AdminRouteKey, "", stale.Cookie()); w.Code != 401 {
			t.Fatalf("%s: %d", name, w.Code)
		}
	}
	// A session made under another key is refused.
	other := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).State(factory.UnderKey("another-key")).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, other.Cookie())

	// At most two per admin: a third sign-in drops the oldest.
	apitest.Must(db.NewDelete().TableExpr("admin_sessions").Where("token_hash <> ?", auth.Digest(session.Token)).Exec(ctx))(t)
	second := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(a.User.ID)).CreateOne(ctx, db, f))(t)
	challenge := apitest.CookieNamed(adminLogin(t, h, a.User.Email, 200), auth.AdminMFACookieName(false))
	adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(a.CodeAt(0)), "", 200, challenge)
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/me", "", "", 200, second.Cookie())
	if n := apitest.CountRows(t, db, "admin_sessions", a.User.ID); n != 2 {
		t.Fatalf("%d admin sessions", n)
	}

	// Signing out of the app leaves the admin session, and the other way round.
	apitest.Call(t, h, "POST", "/api/auth/logout", "", app.CSRFToken, app.Cookie(), 204)
	adminCall(t, h, "GET", "/api/admin/me", "", "", 200, second.Cookie())

	// Admins can't turn MFA off.
	app = apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	w := apitest.Call(t, h, "DELETE", "/api/auth/mfa", fmt.Sprintf(`{"password":%q,"code":%q}`, factory.DefaultPassword, a.Codes[0].Code), app.CSRFToken, app.Cookie(), 409)
	if !strings.Contains(w.Body.String(), "Admins must keep two-factor authentication on.") || apitest.CountRows(t, db, "mfa_totp", a.User.ID) != 1 {
		t.Fatalf("turning MFA off as an admin: %s", w.Body.String())
	}
}

// Everything that takes admin away, or changes the password or MFA, ends
// every admin session.
func TestLosingAdminEndsAdminSessions(t *testing.T) {
	s, db := server(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	ends := map[string]func(a apitest.MFAAccount){
		"revoking admin": func(a apitest.MFAAccount) {
			_, revoked := must2(s.Repo.RevokeAdmin(ctx, a.User.Email))(t)
			if revoked != 1 {
				t.Fatalf("revoked %d", revoked)
			}
			if e := apitest.EventsNamed(t, db, "admin.revoked"); e[len(e)-1].Payload["adminSessionsRevoked"] != 1.0 {
				t.Fatalf("admin.revoked: %+v", e)
			}
		},
		"a password change": func(a apitest.MFAAccount) {
			user := store.User{ID: a.User.ID, PublicID: a.User.PublicID}
			apitest.Must(0, s.Repo.ChangePassword(ctx, user, "", func(string) (string, error) { return "new-hash", nil }))(t)
		},
		"a password reset": func(a apitest.MFAAccount) {
			reset := apitest.Must(factory.PasswordResets.State(factory.ResetFor(a.User.ID)).CreateOne(ctx, db, f))(t)
			apitest.Must(s.Repo.ResetPassword(ctx, auth.Digest(reset.Token), "new-hash"))(t)
		},
		"an MFA reset": func(a apitest.MFAAccount) { apitest.Must(s.Repo.ResetMFA(ctx, a.User.Email))(t) },
		"deleting the account": func(a apitest.MFAAccount) {
			apitest.Must(0, s.Repo.DeleteAccount(ctx, store.User{ID: a.User.ID, PublicID: a.User.PublicID}))(t)
		},
	}
	for name, end := range ends {
		a, _ := adminSignedIn(t, db, f)
		end(a)
		if n := apitest.CountRows(t, db, "admin_sessions", a.User.ID); n != 0 {
			t.Fatalf("%s kept %d admin sessions", name, n)
		}
	}
	if _, _, err := s.Repo.RevokeAdmin(ctx, apitest.WithMFA(t, db, f).User.Email); !errors.Is(err, store.ErrNotAdmin) {
		t.Fatalf("revoking a non-admin: %v", err)
	}
}

// must2 is must for calls with two results besides the error.
func must2[A, B any](a A, b B, err error) func(testing.TB) (A, B) {
	return func(t testing.TB) (A, B) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return a, b
	}
}

// Signing out needs the admin CSRF token, ends only the admin session and
// is recorded.
func TestAdminLogout(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	a, session := adminSignedIn(t, db, f)
	app := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)

	adminCall(t, h, "POST", "/api/admin/logout", "", "", 403, session.Cookie())
	adminCall(t, h, "POST", "/api/admin/logout", "", app.CSRFToken, 403, session.Cookie())
	w := adminCall(t, h, "POST", "/api/admin/logout", "", session.CSRFToken, 204, session.Cookie(), app.Cookie())
	if c := apitest.CookieNamed(w, auth.AdminCookieName(false)); c == nil || c.MaxAge >= 0 {
		t.Fatalf("cookie not cleared: %+v", c)
	}
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, session.Cookie())
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", app.Cookie(), 200)
	if e := apitest.OnlyEvent(t, db, "admin.logged_out"); !apitest.Is(e.ActorID, a.User.ID) || e.UserID.Valid {
		t.Fatalf("admin.logged_out: %+v", e)
	}
}

// The door's password step draws on the same per-account allowance as the
// app's login.
func TestAdminLoginRateLimit(t *testing.T) {
	s, db := server(t)
	s.Config.RateLimits = config.RateLimitsOn
	h := router.New(s)
	f := factory.TestFaker(t)
	a := withAdmin(t, db, f)
	apitest.Must(factory.RateLimits.State(factory.ForAccount(a.User.Email)).State(factory.Exhausted(10)).CreateOne(context.Background(), db, f))(t)
	w := adminLogin(t, h, a.User.Email, 429)
	if w.Header().Get("Retry-After") == "" || len(apitest.EventsNamed(t, db, "admin.login_attempted")) != 0 {
		t.Fatalf("429: %v", w.Header())
	}
	apitest.OnlyEvent(t, db, "rate_limit.reached")
}
