package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/mail"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

// verificationServer is integrationServer with EMAIL_VERIFICATION on, and
// EMAIL_VERIFICATION_EXISTING=required when required. Its listeners run
// only when a test delivers an event (deliver), into an inbox it can read.
func verificationServer(t *testing.T, required bool) (*Server, *bun.DB, *inbox) {
	t.Helper()
	s, db := integrationServer(t)
	s.Config.EmailVerification = config.EmailVerification{Verify: true, RequireExisting: required}
	s.Config.MailDriver = config.MailSMTP
	box := &inbox{}
	box.mailer = must(mail.New(box, "lewcontext"))(t)
	return s, db, box
}

// inbox is a mail driver that keeps what it's given.
type inbox struct {
	mailer *mail.Mailer
	sent   []mail.Message
}

func (b *inbox) Deliver(_ context.Context, m mail.Message) error {
	b.sent = append(b.sent, m)
	return nil
}

// last is the newest message, failing the test without one.
func (b *inbox) last(t *testing.T) mail.Message {
	t.Helper()
	if len(b.sent) == 0 {
		t.Fatal("no mail sent")
	}
	return b.sent[len(b.sent)-1]
}

var emailedCode = regexp.MustCompile(`\b(\d{3}) (\d{3})\b`)

// code is the code in the newest message.
func (b *inbox) code(t *testing.T) string {
	t.Helper()
	match := emailedCode.FindStringSubmatch(b.last(t).Text)
	if match == nil {
		t.Fatalf("no code in:\n%s", b.last(t).Text)
	}
	return match[1] + match[2]
}

// deliverSignUp runs the mail.registration listener on the newest
// auth.registration.requested, as the worker would.
func deliverSignUp(t *testing.T, s *Server, box *inbox) {
	t.Helper()
	id, occurred := newestEvent(t, s.Store.DB, "auth.registration.requested")
	listen := mail.Registration(s.Store, box.mailer, s.keys, s.Config.Origin, true)
	if err := listen(context.Background(), events.RegistrationRequested{}, events.Delivered{EventID: id, OccurredAt: occurred, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
}

// deliverVerification runs the mail.email_verification listener on the
// newest auth.email_verification.requested.
func deliverVerification(t *testing.T, s *Server, box *inbox, user string) {
	t.Helper()
	id, occurred := newestEvent(t, s.Store.DB, "auth.email_verification.requested")
	listen := mail.EmailVerification(s.Store, box.mailer, s.keys)
	if err := listen(context.Background(), events.EmailVerificationRequested{User: user}, events.Delivered{EventID: id, OccurredAt: occurred, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
}

func newestEvent(t *testing.T, db bun.IDB, name string) (string, time.Time) {
	t.Helper()
	var id string
	var occurred time.Time
	err := db.NewSelect().TableExpr("events").Column("public_id", "created_at").
		Where("name = ?", name).OrderExpr("id DESC").Limit(1).
		Scan(context.Background(), &id, &occurred)
	if err != nil {
		t.Fatalf("no %s: %v", name, err)
	}
	return id, occurred
}

// mustCookie is the response's cookie called name, failing the test
// without it.
func mustCookie(t *testing.T, w *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	c := cookieNamed(w, name)
	if c == nil {
		t.Fatalf("no %s cookie in %v", name, w.Result().Cookies())
	}
	return c
}

func signUpBody(email string) string {
	return fmt.Sprintf(`{"name":"Ada Lovelace","email":%q,"password":"correct horse battery staple"}`, email)
}

// signUp starts a sign-up and returns its registration cookie.
func signUp(t *testing.T, h http.Handler, email string) *http.Cookie {
	t.Helper()
	w := call(t, h, "POST", "/api/auth/register", signUpBody(email), "", nil, 202)
	return mustCookie(t, w, auth.RegistrationCookieName(false))
}

func countUsers(t *testing.T, db bun.IDB, email string) int {
	t.Helper()
	n, err := db.NewSelect().TableExpr("users").Where("email = ?", email).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestSignUpWithAnEmailedCode(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h := s.Routes()
	w := call(t, h, "POST", "/api/auth/register", signUpBody("ada@example.com"), "", nil, 202)
	if body := strings.TrimSpace(w.Body.String()); body != `{"email":"ada@example.com","verificationRequired":true}` {
		t.Fatalf("body %s", body)
	}
	cookie := mustCookie(t, w, auth.RegistrationCookieName(false))
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != int(auth.EmailCodeLifetime.Seconds()) {
		t.Fatalf("cookie %+v", cookie)
	}
	// No account, and no session, until the code is right.
	if countUsers(t, db, "ada@example.com") != 0 || len(w.Result().Cookies()) != 1 {
		t.Fatal("register made an account or a session")
	}
	requested := onlyEvent(t, db, "auth.registration.requested")
	if requested.Payload["email"] != "ada@example.com" || requested.IP != "127.0.0.1" || requested.UserID.Valid {
		t.Fatalf("requested %+v", requested)
	}
	deliverSignUp(t, s, box)
	if box.last(t).Template != mail.VerifyEmail || box.last(t).To != "ada@example.com" {
		t.Fatalf("sent %+v", box.last(t))
	}
	code := box.code(t)

	// The code page resumes from the cookie.
	checked := call(t, h, "POST", "/api/auth/register/check", "{}", "", cookie, 200)
	if !strings.Contains(checked.Body.String(), `"email":"ada@example.com"`) {
		t.Fatalf("check %s", checked.Body.String())
	}
	wrong := call(t, h, "POST", "/api/auth/register/verify", codeBody(wrongCode(code)), "", cookie, 422)
	if !strings.Contains(wrong.Body.String(), `"field":"code"`) {
		t.Fatalf("wrong %s", wrong.Body.String())
	}
	// Spaces and a hyphen are fine.
	done := call(t, h, "POST", "/api/auth/register/verify", codeBody(code[:3]+" "+code[3:]), "", cookie, 201)
	var signed response
	_ = json.Unmarshal(done.Body.Bytes(), &signed)
	if signed.User.Email != "ada@example.com" || signed.User.EmailVerifiedAt == nil || signed.CSRF == "" {
		t.Fatalf("signed in %s", done.Body.String())
	}
	mustCookie(t, done, auth.CookieName(false))
	if c := mustCookie(t, done, auth.RegistrationCookieName(false)); c.MaxAge >= 0 {
		t.Fatal("the registration cookie wasn't cleared")
	}
	registered := onlyEvent(t, db, "auth.registered")
	if registered.Payload["verifiedBy"] != "code" || registered.Payload["name"] != "Ada Lovelace" {
		t.Fatalf("registered %+v", registered)
	}
	checks := eventsNamed(t, db, "auth.registration.checked")
	if len(checks) != 2 || checks[0].Payload["ok"] != false || checks[1].Payload["ok"] != true {
		t.Fatalf("checks %+v", checks)
	}
	// The password typed at sign-up signs in.
	call(t, h, "POST", "/api/auth/login", `{"email":"ada@example.com","password":"correct horse battery staple"}`, "", nil, 200)
	// The sign-up is used up.
	call(t, h, "POST", "/api/auth/register/verify", codeBody(code), "", cookie, 401)
	if n := must(db.NewSelect().TableExpr("pending_registrations").Count(context.Background()))(t); n != 0 {
		t.Fatalf("%d sign-ups left", n)
	}
}

// wrongCode is a code that isn't code.
func wrongCode(code string) string {
	if code == "000000" {
		return "000001"
	}
	return "000000"
}

// queryLog records the tables each query names, for comparing two requests.
type queryLog struct {
	mu     sync.Mutex
	tables []string
}

var queryTable = regexp.MustCompile(`(?:FROM|INTO|UPDATE|JOIN) "([a-z_]+)"`)

func (q *queryLog) BeforeQuery(ctx context.Context, _ *bun.QueryEvent) context.Context { return ctx }
func (q *queryLog) AfterQuery(_ context.Context, e *bun.QueryEvent) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, m := range queryTable.FindAllStringSubmatch(e.Query, -1) {
		q.tables = append(q.tables, m[1])
	}
}

// during returns the tables run touches, in order.
func (q *queryLog) during(run func()) []string {
	q.mu.Lock()
	q.tables = nil
	q.mu.Unlock()
	run()
	q.mu.Lock()
	defer q.mu.Unlock()
	return slices.Clone(q.tables)
}

// A sign-up for an email with an account is indistinguishable from one for a
// new email to anyone but the inbox: the same answers, the same cookie, the
// same queries (never users), and the same failures for every code tried.
func TestSignUpLooksTheSameForATakenEmail(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h := s.Routes()
	f := factory.TestFaker(t)
	taken := must(factory.Users.CreateOne(context.Background(), db, f))(t)
	log := &queryLog{}
	db.AddQueryHook(log)

	type run struct {
		register, check, resend, verify *httptest.ResponseRecorder
		tables                          map[string][]string
	}
	try := func(email string) run {
		var r run
		r.tables = map[string][]string{}
		r.tables["register"] = log.during(func() { r.register = send(h, "POST", "/api/auth/register", signUpBody(email), "", nil) })
		cookie := mustCookie(t, r.register, auth.RegistrationCookieName(false))
		deliverSignUp(t, s, box)
		r.tables["check"] = log.during(func() { r.check = send(h, "POST", "/api/auth/register/check", "{}", "", cookie) })
		r.tables["resend"] = log.during(func() { r.resend = send(h, "POST", "/api/auth/register/resend", "{}", "", cookie) })
		deliverSignUp(t, s, box)
		r.tables["verify"] = log.during(func() { r.verify = send(h, "POST", "/api/auth/register/verify", codeBody("000000"), "", cookie) })
		return r
	}
	known, unknown := try(taken.Email), try("nobody@example.com")
	for name, pair := range map[string][2]*httptest.ResponseRecorder{
		"register": {known.register, unknown.register}, "check": {known.check, unknown.check},
		"resend": {known.resend, unknown.resend}, "verify": {known.verify, unknown.verify},
	} {
		a, b := pair[0], pair[1]
		same := strings.ReplaceAll(a.Body.String(), taken.Email, "nobody@example.com")
		if pair[0] == known.check {
			// expiresAt differs by the time between the two runs.
			same, b.Body = "", httptest.NewRecorder().Body
		}
		if a.Code != b.Code || same != b.Body.String() || !sameHeaders(a.Header(), b.Header()) {
			t.Errorf("%s: %d %s %v vs %d %s %v", name, a.Code, a.Body, a.Header(), b.Code, b.Body, b.Header())
		}
	}
	for step, tables := range known.tables {
		if !slices.Equal(tables, unknown.tables[step]) || slices.Contains(tables, "users") {
			t.Errorf("%s touched %v for a taken email, %v for a new one", step, tables, unknown.tables[step])
		}
	}
	// Only the inbox can tell: "you already have an account", no code.
	var notices int
	for _, m := range box.sent {
		if m.To == taken.Email {
			if m.Template != mail.AccountExists || emailedCode.MatchString(m.Text) {
				t.Fatalf("a taken email got %s:\n%s", m.Template, m.Text)
			}
			notices++
		}
	}
	if notices != 2 {
		t.Fatalf("%d notices", notices)
	}
	// Every code fails for the taken email exactly as a wrong one does for a
	// new email: four more 422s, then the fifth ends it.
	for _, email := range []string{taken.Email, "nobody@example.com"} {
		cookie := signUp(t, h, email)
		deliverSignUp(t, s, box)
		for range auth.EmailCodeMaxAttempts - 1 {
			call(t, h, "POST", "/api/auth/register/verify", codeBody("000000"), "", cookie, 422)
		}
		call(t, h, "POST", "/api/auth/register/verify", codeBody("000000"), "", cookie, 401)
	}
	if countUsers(t, db, "nobody@example.com") != 0 {
		t.Fatal("an account was made")
	}
}

// sameHeaders compares two responses' headers, apart from the cookie's
// value and expiry, which are random and timed.
func sameHeaders(a, b http.Header) bool {
	strip := func(h http.Header) http.Header {
		h = h.Clone()
		for i, c := range h.Values("Set-Cookie") {
			parsed, err := http.ParseSetCookie(c)
			if err == nil {
				parsed.Value, parsed.Expires, parsed.RawExpires, parsed.Raw = "", time.Time{}, "", ""
				h["Set-Cookie"][i] = parsed.String()
			}
		}
		return h
	}
	x, y := strip(a), strip(b)
	if len(x) != len(y) {
		return false
	}
	for k := range x {
		if !slices.Equal(x[k], y[k]) {
			return false
		}
	}
	return true
}

// Two sign-ups for one email (the owner's and a stranger's, say) each have
// their own code, which only completes the sign-up in the browser that
// started it: the owner can never create the account with the stranger's
// password.
func TestSignUpCodeBelongsToItsBrowser(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h := s.Routes()
	owner := signUp(t, h, "ada@example.com")
	deliverSignUp(t, s, box)
	ownerCode := box.code(t)
	stranger := call(t, h, "POST", "/api/auth/register", `{"name":"Mallory","email":"ada@example.com","password":"the stranger's password"}`, "", nil, 202)
	strangerCookie := mustCookie(t, stranger, auth.RegistrationCookieName(false))
	deliverSignUp(t, s, box)
	strangerCode := box.code(t)
	if ownerCode == strangerCode {
		t.Skip("the two codes matched by chance")
	}
	// The newest email's code, typed in the owner's browser, fails.
	call(t, h, "POST", "/api/auth/register/verify", codeBody(strangerCode), "", owner, 422)
	call(t, h, "POST", "/api/auth/register/verify", codeBody(ownerCode), "", owner, 201)
	// The account has the owner's password, and every other sign-up for the
	// email is gone.
	call(t, h, "POST", "/api/auth/login", `{"email":"ada@example.com","password":"correct horse battery staple"}`, "", nil, 200)
	call(t, h, "POST", "/api/auth/register/verify", codeBody(strangerCode), "", strangerCookie, 401)
	if countUsers(t, db, "ada@example.com") != 1 {
		t.Fatal("two accounts")
	}
}

func TestSignUpCodeEnds(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	t.Run("without a cookie, or a live sign-up", func(t *testing.T) {
		for _, path := range []string{"check", "resend", "verify"} {
			call(t, h, "POST", "/api/auth/register/"+path, codeBody("123456"), "", nil, 401)
		}
		expired := must(factory.PendingRegistrations.State(factory.ExpiredRegistration).CreateOne(ctx, db, f))(t)
		call(t, h, "POST", "/api/auth/register/verify", codeBody(expired.Code), "", expired.Cookie(), 401)
	})
	t.Run("a new code replaces the old", func(t *testing.T) {
		cookie := signUp(t, h, "resend@example.com")
		deliverSignUp(t, s, box)
		first := box.code(t)
		call(t, h, "POST", "/api/auth/register/verify", codeBody(wrongCode(first)), "", cookie, 422)
		resent := call(t, h, "POST", "/api/auth/register/resend", "{}", "", cookie, 202)
		mustCookie(t, resent, auth.RegistrationCookieName(false))
		// Until the listener runs there's no code, and the old one is gone.
		call(t, h, "POST", "/api/auth/register/verify", codeBody(first), "", cookie, 422)
		deliverSignUp(t, s, box)
		second := box.code(t)
		if first != second {
			call(t, h, "POST", "/api/auth/register/verify", codeBody(first), "", cookie, 422)
		}
		call(t, h, "POST", "/api/auth/register/verify", codeBody(second), "", cookie, 201)
	})
	t.Run("an account made meanwhile", func(t *testing.T) {
		pending := must(factory.PendingRegistrations.CreateOne(ctx, db, f))(t)
		must(factory.Users.State(func(u *factory.User) { u.Email = pending.Email }).CreateOne(ctx, db, f))(t)
		w := call(t, h, "POST", "/api/auth/register/verify", codeBody(pending.Code), "", pending.Cookie(), 409)
		if !strings.Contains(w.Body.String(), "You already have an account") || countUsers(t, db, pending.Email) != 1 {
			t.Fatalf("409 %s", w.Body.String())
		}
	})
}

func TestSignUpLimits(t *testing.T) {
	ctx := context.Background()
	t.Run("register:per-email, alike for every address", func(t *testing.T) {
		for _, taken := range []bool{true, false} {
			s, db, _ := verificationServer(t, false)
			f := factory.TestFaker(t)
			if taken {
				must(factory.Users.State(func(u *factory.User) { u.Email = "ada@example.com" }).CreateOne(ctx, db, f))(t)
			}
			must(factory.RateLimits.State(factory.ForLimit("register:per-email", "ada@example.com")).State(factory.Exhausted(3)).CreateOne(ctx, db, f))(t)
			refused := call(t, s.Routes(), "POST", "/api/auth/register", signUpBody("ada@example.com"), "", nil, 429)
			retryWithin(t, refused, time.Hour)
			if !strings.Contains(refused.Body.String(), "This email address has had too many sign-up attempts recently.") {
				t.Fatalf("refused %s", refused.Body.String())
			}
			reached(t, s, "register:per-email:block")
			if n := countRateLimits(t, s, "register:global"); n != 0 {
				t.Fatal("a request refused per email spent the global limit")
			}
		}
	})
	t.Run("email-code:per-email", func(t *testing.T) {
		s, db, _ := verificationServer(t, false)
		f := factory.TestFaker(t)
		pending := must(factory.PendingRegistrations.CreateOne(ctx, db, f))(t)
		must(factory.RateLimits.State(factory.ForLimit("email-code:per-email", pending.Email)).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
		refused := call(t, s.Routes(), "POST", "/api/auth/register/verify", codeBody(pending.Code), "", pending.Cookie(), 429)
		retryWithin(t, refused, 24*time.Hour)
		reached(t, s, "email-code:per-email:block")
		if countUsers(t, db, pending.Email) != 0 {
			t.Fatal("a refused code made the account")
		}
	})
}

// With verification off, sign-up is as before and the code routes don't
// exist.
func TestSignUpWithoutVerification(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	w := call(t, h, "POST", "/api/auth/register", signUpBody("ada@example.com"), "", nil, 201)
	if !strings.Contains(w.Body.String(), `"emailVerifiedAt":null`) || !strings.Contains(w.Body.String(), `"emailVerification":"off"`) {
		t.Fatalf("register %s", w.Body.String())
	}
	if onlyEvent(t, db, "auth.registered").Payload["verifiedBy"] != "none" {
		t.Fatal("verifiedBy")
	}
	call(t, h, "POST", "/api/auth/register", signUpBody("ada@example.com"), "", nil, 409)
	for _, route := range []string{"POST /api/auth/register/check", "POST /api/auth/register/verify", "POST /api/auth/email-verification"} {
		method, path, _ := strings.Cut(route, " ")
		call(t, h, method, path, "{}", "", nil, 404)
	}
}

func TestVerifyingAnExistingAccount(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := must(factory.Users.State(factory.Unverified).CreateOne(ctx, db, f))(t)
	session := must(factory.SessionFor(ctx, db, f, user.ID))(t)
	me := call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 200)
	if !strings.Contains(me.Body.String(), `"emailVerifiedAt":null`) || !strings.Contains(me.Body.String(), `"emailVerification":"optional"`) {
		t.Fatalf("me %s", me.Body.String())
	}
	call(t, h, "PATCH", "/api/auth/email-verification", codeBody("123456"), session.CSRFToken, session.Cookie(), 410)
	call(t, h, "POST", "/api/auth/email-verification", "{}", session.CSRFToken, session.Cookie(), 202)
	deliverVerification(t, s, box, user.PublicID)
	if box.last(t).To != user.Email || box.last(t).Template != mail.VerifyEmail || strings.Contains(box.last(t).HTML, user.Name) {
		t.Fatalf("sent %+v", box.last(t))
	}
	code := box.code(t)
	call(t, h, "PATCH", "/api/auth/email-verification", codeBody(wrongCode(code)), session.CSRFToken, session.Cookie(), 422)
	done := call(t, h, "PATCH", "/api/auth/email-verification", codeBody(code), session.CSRFToken, session.Cookie(), 200)
	if strings.Contains(done.Body.String(), `"emailVerifiedAt":null`) {
		t.Fatalf("verify %s", done.Body.String())
	}
	if e := onlyEvent(t, db, "auth.email_verified"); e.Payload["via"] != "code" || !is(e.UserID, user.ID) {
		t.Fatalf("verified %+v", e)
	}
	call(t, h, "POST", "/api/auth/email-verification", "{}", session.CSRFToken, session.Cookie(), 409)
	call(t, h, "PATCH", "/api/auth/email-verification", codeBody(code), session.CSRFToken, session.Cookie(), 409)

	t.Run("five wrong codes end it", func(t *testing.T) {
		other := must(factory.Users.State(factory.Unverified).CreateOne(ctx, db, f))(t)
		session := must(factory.SessionFor(ctx, db, f, other.ID))(t)
		code := must(factory.EmailVerifications.State(factory.VerificationFor(other.ID)).CreateOne(ctx, db, f))(t)
		for range auth.EmailCodeMaxAttempts - 1 {
			call(t, h, "PATCH", "/api/auth/email-verification", codeBody(wrongCode(code.Code)), session.CSRFToken, session.Cookie(), 422)
		}
		call(t, h, "PATCH", "/api/auth/email-verification", codeBody(wrongCode(code.Code)), session.CSRFToken, session.Cookie(), 410)
		call(t, h, "PATCH", "/api/auth/email-verification", codeBody(code.Code), session.CSRFToken, session.Cookie(), 410)
	})
	t.Run("a password reset verifies the email", func(t *testing.T) {
		other := must(factory.Users.State(factory.Unverified).CreateOne(ctx, db, f))(t)
		reset := must(factory.PasswordResets.State(factory.ResetFor(other.ID)).CreateOne(ctx, db, f))(t)
		call(t, h, "PATCH", "/api/auth/password-reset", resetBody(reset.Token, "a brand new passphrase"), "", nil, 200)
		var verified *time.Time
		if err := db.NewSelect().TableExpr("users").Column("email_verified_at").Where("id = ?", other.ID).Scan(ctx, &verified); err != nil || verified == nil {
			t.Fatalf("not verified: %v", err)
		}
	})
	t.Run("asking is limited", func(t *testing.T) {
		other := must(factory.Users.State(factory.Unverified).CreateOne(ctx, db, f))(t)
		session := must(factory.SessionFor(ctx, db, f, other.ID))(t)
		must(factory.RateLimits.State(factory.ForLimit("email-verification:per-email", other.Email)).State(factory.Exhausted(3)).CreateOne(ctx, db, f))(t)
		retryWithin(t, call(t, h, "POST", "/api/auth/email-verification", "{}", session.CSRFToken, session.Cookie(), 429), time.Hour)
	})
}

// With EMAIL_VERIFICATION_EXISTING=required an unverified account's session
// works only on the routes allowUnverified marks; a verified one isn't
// affected, nor is anyone with it optional.
func TestUnverifiedAccountsMustVerify(t *testing.T) {
	ctx := context.Background()
	s, db, _ := verificationServer(t, true)
	h := s.Routes()
	f := factory.TestFaker(t)
	unverified := must(factory.Users.State(factory.Unverified).CreateOne(ctx, db, f))(t)
	session := must(factory.SessionFor(ctx, db, f, unverified.ID))(t)
	workspace := ownedWorkspace(t, db, f, unverified.ID)
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/workspaces", ""},
		{"GET", workspacePath(workspace.PublicID, "/projects"), ""},
		{"POST", "/api/workspaces", `{"name":"Acme"}`},
		{"PATCH", "/api/auth/me", `{"name":"New name"}`},
		{"POST", "/api/auth/password/verify", `{"password":"x"}`},
		{"POST", "/api/invitations/accept", `{"token":"x"}`},
	} {
		w := call(t, h, route.method, route.path, route.body, session.CSRFToken, session.Cookie(), 403)
		if !strings.Contains(w.Body.String(), `"verificationRequired":true`) {
			t.Errorf("%s %s: %s", route.method, route.path, w.Body.String())
		}
	}
	me := call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 200)
	if !strings.Contains(me.Body.String(), `"emailVerification":"required"`) {
		t.Fatalf("me %s", me.Body.String())
	}
	call(t, h, "POST", "/api/auth/email-verification", "{}", session.CSRFToken, session.Cookie(), 202)
	call(t, h, "PATCH", "/api/auth/email-verification", codeBody("000000"), session.CSRFToken, session.Cookie(), 410)
	call(t, h, "DELETE", "/api/auth/sessions", "", session.CSRFToken, session.Cookie(), 204)
	call(t, h, "POST", "/api/auth/logout", "", session.CSRFToken, session.Cookie(), 204)

	verified := signedIn(t, db, f)
	call(t, h, "GET", "/api/workspaces", "", "", verified.Cookie(), 200)
	optional, db2, _ := verificationServer(t, false)
	other := must(factory.Users.State(factory.Unverified).CreateOne(ctx, db2, f))(t)
	otherSession := must(factory.SessionFor(ctx, db2, f, other.ID))(t)
	call(t, optional.Routes(), "GET", "/api/workspaces", "", "", otherSession.Cookie(), 200)
}

// Only the five routes an unverified account needs skip the check: reading
// the session, signing out, signing out other sessions, asking for a code
// and entering it. A new route is covered unless someone adds it here.
func TestOnlyFiveRoutesAllowUnverified(t *testing.T) {
	source, err := os.ReadFile("routes.go")
	if err != nil {
		t.Fatal(err)
	}
	var allowed []string
	for _, m := range regexp.MustCompile(`mux\.Handle\("([^"]+)", allowUnverified\(`).FindAllStringSubmatch(string(source), -1) {
		allowed = append(allowed, m[1])
	}
	slices.Sort(allowed)
	want := []string{"DELETE /api/auth/sessions", "GET /api/auth/me", "PATCH /api/auth/email-verification", "POST /api/auth/email-verification", "POST /api/auth/logout"}
	if !slices.Equal(allowed, want) || strings.Count(string(source), "allowUnverified(") != len(want) {
		t.Fatalf("allowUnverified on %v", allowed)
	}
}
