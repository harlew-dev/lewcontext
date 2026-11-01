package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/captcha"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeCaptcha passes "good" (for any action), fails "down" as unreachable,
// and rejects anything else, recording what it was asked.
type fakeCaptcha struct {
	mu      sync.Mutex
	actions []string
}

func (f *fakeCaptcha) Name() string    { return "fake" }
func (f *fakeCaptcha) SiteKey() string { return "site-key" }
func (f *fakeCaptcha) CSP() captcha.Sources {
	return captcha.Sources{Script: []string{"https://captcha.example"}}
}
func (f *fakeCaptcha) Verify(_ context.Context, token, action, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	switch token {
	case "good":
		return nil
	case "down":
		return captcha.ErrUnavailable
	}
	return captcha.ErrRejected
}
func (f *fakeCaptcha) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.actions)
}

// challengeServer is server with the fake provider.
func challengeServer(t *testing.T) (*httpapi.Server, *fakeCaptcha) {
	t.Helper()
	s, _ := server(t)
	fake := &fakeCaptcha{}
	s.Captcha = fake
	return s, fake
}

// sendFrom is apitest.Send from ip, with a challenge token when given.
func sendFrom(h http.Handler, ip, method, path, body, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", apitest.Origin)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = ip + ":1234"
	if token != "" {
		r.Header.Set(httpapi.ChallengeTokenHeader, token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// spend fills a limit's counter to hits, as that many requests would.
func spend(t *testing.T, s *httpapi.Server, name, subject string, hits int) {
	t.Helper()
	f := factory.TestFaker(t)
	apitest.Must(factory.RateLimits.State(factory.ForLimit(name, subject)).State(factory.Exhausted(hits)).CreateOne(context.Background(), s.Repo.DB, f))(t)
}

// reachedOnce is the one rate_limit.reached, which must be for limit (with
// its level); its ip column is the request's.
func reachedOnce(t *testing.T, s *httpapi.Server, limit string) apitest.Event {
	t.Helper()
	e := apitest.OnlyEvent(t, s.Repo.DB, "rate_limit.reached")
	if e.Payload["limit"] != limit || e.IP == "" || strings.Contains(e.Raw, "good") {
		t.Fatalf("want %s: %+v", limit, e)
	}
	return e
}

// challenged checks a 428 asking for action.
func challenged(t *testing.T, w *httptest.ResponseRecorder, action, message string) {
	t.Helper()
	var body struct {
		Error     string
		Challenge map[string]string
	}
	if w.Code != 428 || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Error != message ||
		body.Challenge["provider"] != "fake" || body.Challenge["siteKey"] != "site-key" || body.Challenge["action"] != action {
		t.Fatalf("want 428 for %s (%q), got %d %s", action, message, w.Code, w.Body.String())
	}
}

const loginUnknown = `{"email":"nobody@example.com","password":"correct horse battery staple"}`

func TestLoginChallenge(t *testing.T) {
	s, fake := challengeServer(t)
	h := router.New(s)
	// Five sign-ins from one address in the window pass without a token.
	for range 5 {
		if w := sendFrom(h, "203.0.113.5", "POST", "/api/auth/login", loginUnknown, ""); w.Code != 401 {
			t.Fatalf("under the challenge level: %d", w.Code)
		}
	}
	// The sixth needs a solved CAPTCHA; a wrong token is refused again.
	challenged(t, sendFrom(h, "203.0.113.5", "POST", "/api/auth/login", loginUnknown, ""), "login", "Complete the check to continue.")
	challenged(t, sendFrom(h, "203.0.113.5", "POST", "/api/auth/login", loginUnknown, "forged"), "login", "The check failed. Try again.")
	if w := sendFrom(h, "203.0.113.5", "POST", "/api/auth/login", loginUnknown, "good"); w.Code != 401 {
		t.Fatalf("a solved check: %d", w.Code)
	}
	if fake.actions[len(fake.actions)-1] != "login" {
		t.Fatalf("verified for %v", fake.actions)
	}
	// Another address is untouched.
	if w := sendFrom(h, "203.0.113.6", "POST", "/api/auth/login", loginUnknown, ""); w.Code != 401 {
		t.Fatalf("another address: %d", w.Code)
	}
	// The challenge level is recorded once, by the request that crossed it.
	reachedOnce(t, s, "auth:per-ip:challenge")
	// Above the block level, even a solved CAPTCHA is refused.
	spend(t, s, "auth:per-ip", "203.0.113.7", 60)
	if w := sendFrom(h, "203.0.113.7", "POST", "/api/auth/login", loginUnknown, "good"); w.Code != 429 {
		t.Fatalf("above the block level: %d", w.Code)
	}
}

// A challenge depends on the address alone: it asks the same, with the
// same answer, whether or not an account uses the email.
func TestChallengeSaysNothingAboutAccounts(t *testing.T) {
	s, _ := challengeServer(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	user := apitest.Must(factory.Users.CreateOne(context.Background(), s.Repo.DB, f))(t)
	known := fmt.Sprintf(`{"email":%q,"password":"wrong password here"}`, user.Email)
	spend(t, s, "auth:per-ip", "203.0.113.20", 5)
	spend(t, s, "auth:per-ip", "203.0.113.21", 5)
	a := sendFrom(h, "203.0.113.20", "POST", "/api/auth/login", known, "")
	b := sendFrom(h, "203.0.113.21", "POST", "/api/auth/login", loginUnknown, "")
	if a.Code != 428 || a.Body.String() != b.Body.String() {
		t.Fatalf("known %d %s, unknown %d %s", a.Code, a.Body, b.Code, b.Body)
	}
	spend(t, s, "auth:per-ip", "203.0.113.22", 5)
	spend(t, s, "auth:per-ip", "203.0.113.23", 5)
	signUp := func(email string) string {
		return fmt.Sprintf(`{"name":"Ada","email":%q,"password":"correct horse battery staple"}`, email)
	}
	a = sendFrom(h, "203.0.113.22", "POST", "/api/auth/register", signUp(user.Email), "")
	b = sendFrom(h, "203.0.113.23", "POST", "/api/auth/register", signUp("new@example.com"), "")
	if a.Code != 428 || a.Body.String() != b.Body.String() {
		t.Fatalf("taken %d %s, new %d %s", a.Code, a.Body, b.Code, b.Body)
	}
}

// A 428 changes nothing, so the request can be sent again; one token
// covers every limit the request is counted against.
func TestRegisterChallenge(t *testing.T) {
	s, fake := challengeServer(t)
	h := router.New(s)
	body := `{"name":"Ada","email":"ada@example.com","password":"correct horse battery staple"}`
	spend(t, s, "auth:per-ip", "203.0.113.30", 5)
	challenged(t, sendFrom(h, "203.0.113.30", "POST", "/api/auth/register", body, ""), "register", "Complete the check to continue.")
	users, _ := s.Repo.DB.NewSelect().TableExpr("users").Count(context.Background())
	registered := len(apitest.EventsNamed(t, s.Repo.DB, "auth.registered"))
	if users != 0 || registered != 0 {
		t.Fatal("a challenged sign-up created something")
	}
	// Past both auth:per-ip's and register:global's challenge levels: one
	// token, checked once.
	spend(t, s, "auth:per-ip", "203.0.113.31", 5)
	spend(t, s, "register:global", "", 200)
	before := fake.calls()
	if w := sendFrom(h, "203.0.113.31", "POST", "/api/auth/register", body, "good"); w.Code != 201 {
		t.Fatalf("a solved sign-up: %d %s", w.Code, w.Body)
	}
	if fake.calls() != before+1 {
		t.Fatalf("the token was verified %d times", fake.calls()-before)
	}
	// Above register:global's block level, counting the requests that got
	// through: 503, whatever the token.
	s2, _ := challengeServer(t)
	spend(t, s2, "register:global", "", 1000)
	spend(t, s2, "register:global:passed", "", 800)
	if w := sendFrom(router.New(s2), "203.0.113.32", "POST", "/api/auth/register", body, "good"); w.Code != 503 || !strings.Contains(w.Body.String(), "Sign-up is busy") {
		t.Fatalf("above the block level: %d %s", w.Code, w.Body)
	}
}

// Asking for a reset link can be challenged; the link's own requests and
// the signed-in password routes never are, however many there were.
func TestPasswordResetChallenge(t *testing.T) {
	s, _ := challengeServer(t)
	h := router.New(s)
	spend(t, s, "auth:per-ip", "203.0.113.40", 5)
	challenged(t, sendFrom(h, "203.0.113.40", "POST", "/api/auth/password-reset", `{"email":"a@example.com"}`, ""), "password_reset", "Complete the check to continue.")
	if n := len(apitest.EventsNamed(t, s.Repo.DB, "auth.password_reset.requested")); n != 0 {
		t.Fatalf("a challenged request was recorded %d times", n)
	}
	token := strings.Repeat("a", 43)
	for _, req := range []struct{ method, path, body string }{
		{"POST", "/api/auth/password-reset/check", apitest.TokenBody(token)},
		{"PATCH", "/api/auth/password-reset", `{"token":"` + token + `","newPassword":"a new long passphrase"}`},
	} {
		if w := sendFrom(h, "203.0.113.40", req.method, req.path, req.body, ""); w.Code == 428 {
			t.Errorf("%s %s was challenged", req.method, req.path)
		}
	}
	// password-reset:global: challenged above 50, refused above 100.
	spend(t, s, "password-reset:global", "", 50)
	challenged(t, sendFrom(h, "203.0.113.41", "POST", "/api/auth/password-reset", `{"email":"b@example.com"}`, ""), "password_reset", "Complete the check to continue.")
	if w := sendFrom(h, "203.0.113.41", "POST", "/api/auth/password-reset", `{"email":"b@example.com"}`, "good"); w.Code != 202 {
		t.Fatalf("a solved reset request: %d %s", w.Code, w.Body)
	}
}

func TestSignedInPasswordRoutesAreNeverChallenged(t *testing.T) {
	s, _ := challengeServer(t)
	h := router.New(s)
	session := apitest.SignedIn(t, s.Repo.DB, factory.TestFaker(t))
	spend(t, s, "auth:per-ip", "127.0.0.1", 10)
	w := apitest.Send(h, "POST", "/api/auth/password/verify", `{"password":"`+factory.DefaultPassword+`"}`, session.CSRFToken, session.Cookie())
	if w.Code == 428 {
		t.Fatal("a signed-in password check was challenged")
	}
}

// Fail closed while the provider can't answer, but only in challenge mode.
func TestChallengeFailsClosed(t *testing.T) {
	s, fake := challengeServer(t)
	h := router.New(s)
	if w := sendFrom(h, "203.0.113.50", "POST", "/api/auth/login", loginUnknown, "down"); w.Code != 401 || fake.calls() != 0 {
		t.Fatalf("below the level the provider was asked: %d, %d calls", w.Code, fake.calls())
	}
	spend(t, s, "auth:per-ip", "203.0.113.51", 5)
	if w := sendFrom(h, "203.0.113.51", "POST", "/api/auth/login", loginUnknown, "down"); w.Code != 503 || !strings.Contains(w.Body.String(), "Verification is unavailable") {
		t.Fatalf("provider down: %d %s", w.Code, w.Body)
	}
}

// CAPTCHA_PROVIDER=off: challenge levels are skipped, but still recorded,
// and block levels apply.
func TestChallengeLevelsWithoutAProvider(t *testing.T) {
	s, _ := server(t)
	h := router.New(s)
	spend(t, s, "auth:per-ip", "203.0.113.60", 5)
	if w := sendFrom(h, "203.0.113.60", "POST", "/api/auth/login", loginUnknown, ""); w.Code != 401 {
		t.Fatalf("off: %d", w.Code)
	}
	reachedOnce(t, s, "auth:per-ip:challenge")
	spend(t, s, "auth:per-ip", "203.0.113.61", 60)
	if w := sendFrom(h, "203.0.113.61", "POST", "/api/auth/login", loginUnknown, ""); w.Code != 429 {
		t.Fatalf("off, above the block level: %d", w.Code)
	}
}

// PER_IP_LIMITS=off: no per-IP level triggers or is recorded; per-account
// and global limits still apply.
func TestPerIPLimitsOff(t *testing.T) {
	s, _ := challengeServer(t)
	s.Config.PerIPLimitsOff = true
	h := router.New(s)
	spend(t, s, "auth:per-ip", "203.0.113.70", 60)
	spend(t, s, "auth:per-account", "nobody@example.com", 9)
	if w := sendFrom(h, "203.0.113.70", "POST", "/api/auth/login", loginUnknown, ""); w.Code != 401 {
		t.Fatalf("a 61st login: %d", w.Code)
	}
	if n := len(apitest.EventsNamed(t, s.Repo.DB, "rate_limit.reached")); n != 0 {
		t.Fatalf("%d events for a never level", n)
	}
	if w := sendFrom(h, "203.0.113.70", "POST", "/api/auth/login", loginUnknown, ""); w.Code != 429 {
		t.Fatalf("auth:per-account's 11th: %d", w.Code)
	}
	spend(t, s, "register:global", "", 200)
	challenged(t, sendFrom(h, "203.0.113.71", "POST", "/api/auth/register", `{"name":"Ada","email":"ada@example.com","password":"correct horse battery staple"}`, ""), "register", "Complete the check to continue.")
}

// Invitations: invitation:global challenges the signed-in inviter.
func TestInvitationChallenge(t *testing.T) {
	s, _ := challengeServer(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, s.Repo.DB, f)
	workspace := apitest.OwnedWorkspace(t, s.Repo.DB, f, session.UserID)
	spend(t, s, "invitation:global", "", 100)
	w := apitest.Send(h, "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), `{"email":"guest@example.com","role":"member"}`, session.CSRFToken, session.Cookie())
	challenged(t, w, "invitation", "Complete the check to continue.")
	if n, _ := s.Repo.DB.NewSelect().TableExpr("workspace_invitations").Count(context.Background()); n != 0 {
		t.Fatal("a challenged invitation was saved")
	}
	e := reachedOnce(t, s, "invitation:global:challenge")
	if e.Payload["workspace"] != workspace.PublicID {
		t.Fatalf("event %+v", e.Payload)
	}
}

// limitEvents counts rate_limit.reached by limit and level.
func limitEvents(t *testing.T, s *httpapi.Server) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, e := range apitest.EventsNamed(t, s.Repo.DB, "rate_limit.reached") {
		counts[e.Payload["limit"].(string)]++
	}
	return counts
}

// Requests nobody solves, from any number of addresses, never push a
// limit shared by everyone to its block level: only the requests that got
// through count there, so a real person with a solved check still gets in.
func TestUnsolvedRequestsNeverBlockASharedLimit(t *testing.T) {
	s, _ := challengeServer(t)
	h := router.New(s)
	// register:global is 10 short of its block level by raw count.
	spend(t, s, "register:global", "", 990)
	for i := range 30 {
		body := fmt.Sprintf(`{"name":"Bot","email":"bot%d@example.com","password":"correct horse battery staple"}`, i)
		challenged(t, sendFrom(h, fmt.Sprintf("198.51.100.%d", i+1), "POST", "/api/auth/register", body, ""), "register", "Complete the check to continue.")
	}
	if w := sendFrom(h, "203.0.113.99", "POST", "/api/auth/register", `{"name":"Ada","email":"ada@example.com","password":"correct horse battery staple"}`, "good"); w.Code != 201 {
		t.Fatalf("a solved sign-up after a flood of unsolved ones: %d %s", w.Code, w.Body)
	}
	// password-reset:global: 101 unsolved requests from 21 addresses, 5 each
	// (under auth:per-ip's challenge level), the probe that blocked everyone.
	codes := map[int]int{}
	for i := range 101 {
		w := sendFrom(h, fmt.Sprintf("198.51.101.%d", i/5+1), "POST", "/api/auth/password-reset", fmt.Sprintf(`{"email":"x%d@example.com"}`, i), "")
		codes[w.Code]++
	}
	if codes[202] != 50 || codes[428] != 51 {
		t.Fatalf("101 unsolved reset requests: %v", codes)
	}
	if w := sendFrom(h, "203.0.113.98", "POST", "/api/auth/password-reset", `{"email":"real@example.com"}`, "good"); w.Code != 202 {
		t.Fatalf("a solved reset request after the flood: %d %s", w.Code, w.Body)
	}
	// The reset challenge level was recorded once, by the request (a 428)
	// that crossed it (register:global's was crossed before the test); no
	// block level was reached.
	if got := limitEvents(t, s); got["password-reset:global:challenge"] != 1 || len(got) != 1 {
		t.Fatalf("events %v", got)
	}
}

// The requests that got through still reach a shared limit's block level,
// once, exactly, under concurrency; past it, an unsolved request is told
// at once instead of being asked for a check it couldn't use.
func TestSolvedRequestsReachASharedLimitsBlock(t *testing.T) {
	s, fake := challengeServer(t)
	h := router.New(s)
	// At the challenge level: 50 more may pass before the block level.
	spend(t, s, "password-reset:global", "", 50)
	var mu sync.Mutex
	codes := map[int]int{}
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token := "good"
			if i%5 == 0 {
				token = "" // 20 never solved
			}
			w := sendFrom(h, fmt.Sprintf("198.51.102.%d", i+1), "POST", "/api/auth/password-reset", fmt.Sprintf(`{"email":"c%d@example.com"}`, i), token)
			mu.Lock()
			codes[w.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()
	// 80 solved: 50 through, 30 refused; the 20 unsolved get 428, or 503
	// once the level is reached.
	if codes[202] != 50 || codes[202]+codes[503]+codes[428] != 100 || codes[503] < 30 {
		t.Fatalf("100 concurrent reset requests: %v", codes)
	}
	if got := limitEvents(t, s); got["password-reset:global:challenge"] != 1 || got["password-reset:global:block"] != 1 {
		t.Fatalf("events %v", got)
	}
	before := fake.calls()
	if w := sendFrom(h, "203.0.113.97", "POST", "/api/auth/password-reset", `{"email":"late@example.com"}`, ""); w.Code != 503 || fake.calls() != before {
		t.Fatalf("an unsolved request past the block level: %d, %d checks", w.Code, fake.calls()-before)
	}
}

// Per-IP limits keep counting unsolved requests: a flood from one address
// still reaches its own block level, and hurts only that address.
func TestUnsolvedRequestsCountPerIP(t *testing.T) {
	s, _ := challengeServer(t)
	h := router.New(s)
	spend(t, s, "auth:per-ip", "203.0.113.80", 58)
	for range 2 {
		challenged(t, sendFrom(h, "203.0.113.80", "POST", "/api/auth/login", loginUnknown, ""), "login", "Complete the check to continue.")
	}
	if w := sendFrom(h, "203.0.113.80", "POST", "/api/auth/login", loginUnknown, ""); w.Code != 429 {
		t.Fatalf("the 61st, unsolved: %d", w.Code)
	}
	var hits int
	if err := s.Repo.DB.NewSelect().TableExpr("rate_limits").Column("hits").Where("key = ?", "auth:per-ip:"+auth.Digest("203.0.113.80")).Scan(context.Background(), &hits); err != nil || hits != 61 {
		t.Fatalf("auth:per-ip counted %d (%v)", hits, err)
	}
}

// resendFrom asks for a sign-up's new code from ip, with its cookie and a
// challenge token when given.
func resendFrom(h http.Handler, ip string, cookie *http.Cookie, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/auth/register/resend", strings.NewReader("{}"))
	r.Header.Set("Origin", apitest.Origin)
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = ip + ":1234"
	r.AddCookie(cookie)
	if token != "" {
		r.Header.Set(httpapi.ChallengeTokenHeader, token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// passedHits is a shared limit's count of requests that got through.
func passedHits(t *testing.T, s *httpapi.Server, name string) int {
	t.Helper()
	var hits int
	if err := s.Repo.DB.NewSelect().TableExpr("rate_limits").Column("hits").Where("key = ?", name+":passed").Scan(context.Background(), &hits); err != nil {
		t.Fatal(err)
	}
	return hits
}

// A new sign-up code counts against register:global, so it's challenged
// like a sign-up: resends nobody solves never count as passed, and can't
// push sign-up to its block level for everyone.
func TestResendIsChallengedLikeASignUp(t *testing.T) {
	s, fake := challengeServer(t)
	s.Config.EmailVerification = config.EmailVerification{Verify: true}
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	spend(t, s, "register:global", "", 300)
	spend(t, s, "register:global:passed", "", 795)
	var cookies []*http.Cookie
	for range 5 {
		cookies = append(cookies, apitest.Must(factory.PendingRegistrations.CreateOne(ctx, s.Repo.DB, f))(t).Cookie())
	}
	for i, cookie := range cookies {
		for range 2 {
			challenged(t, resendFrom(h, fmt.Sprintf("198.51.103.%d", i+1), cookie, ""), "register", "Complete the check to continue.")
		}
	}
	if passedHits(t, s, "register:global") != 795 || fake.calls() != 0 {
		t.Fatalf("unsolved resends counted as passed: %d, %d checks", passedHits(t, s, "register:global"), fake.calls())
	}
	// A solved resend is counted, and a solved sign-up still gets in.
	if w := resendFrom(h, "198.51.103.1", cookies[0], "good"); w.Code != 202 || fake.actions[0] != "register" {
		t.Fatalf("a solved resend: %d %s", w.Code, w.Body)
	}
	if w := sendFrom(h, "203.0.113.50", "POST", "/api/auth/register", `{"name":"Ada","email":"ada@example.com","password":"correct horse battery staple"}`, "good"); w.Code != 202 {
		t.Fatalf("a solved sign-up after the resends: %d %s", w.Code, w.Body)
	}
	if got := passedHits(t, s, "register:global"); got != 797 {
		t.Fatalf("passed %d, want 797", got)
	}
}

// A route that counts a shared limit with a challenge level must be
// Challengeable, or nothing could ask it for a token: it's refused, loudly,
// even with limits off. Per-IP limits may be counted anywhere.
func TestSharedChallengeLimitsNeedAChallengeableRoute(t *testing.T) {
	s, _ := server(t)
	for _, limitsOff := range []bool{false, true} {
		if limitsOff {
			s.Config.RateLimits = config.RateLimitsOff
		}
		for _, l := range httpapi.RateLimits {
			if !l.Shared() || !l.Challenge.Set() {
				continue
			}
			w := httptest.NewRecorder()
			if s.Allow(w, httptest.NewRequest("POST", "/api/somewhere", nil), l, l.Everyone()) || w.Code != 503 {
				t.Fatalf("%s counted on a route that isn't Challengeable (limits off %v): %d", l.Name, limitsOff, w.Code)
			}
		}
	}
	if n := apitest.CountRateLimits(t, s.Repo.DB, "%:global%"); n != 0 {
		t.Fatalf("%d counters written", n)
	}
	s.Config.RateLimits = config.RateLimitsOn
	w := httptest.NewRecorder()
	if !s.Allow(w, httptest.NewRequest("POST", "/api/somewhere", nil), httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey("203.0.113.90")) {
		t.Fatalf("auth:per-ip refused on a route without an action: %d", w.Code)
	}
}

// A blocked address is refused by ClientAddress before a challengeable
// route is reached: 403, never a 428 and no CAPTCHA asked for, even above
// the challenge level and with a token.
func TestBlockedAddressBeforeTheChallenge(t *testing.T) {
	s, fake := challengeServer(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	apitest.Must(factory.IPBlocks.State(factory.BlockOf("203.0.113.0/24")).CreateOne(ctx, s.Repo.DB, f))(t)
	apitest.Must(0, s.ReloadIPBlocks(ctx))(t)
	spend(t, s, "auth:per-ip", "203.0.113.70", 5)
	h := router.New(s)
	for _, token := range []string{"", "good"} {
		w := sendFrom(h, "203.0.113.70", "POST", "/api/auth/login", loginUnknown, token)
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), httpapi.IPBlocked) {
			t.Fatalf("token %q: %d %s", token, w.Code, w.Body)
		}
	}
	if fake.calls() != 0 {
		t.Fatalf("the provider was asked %d times", fake.calls())
	}
}
