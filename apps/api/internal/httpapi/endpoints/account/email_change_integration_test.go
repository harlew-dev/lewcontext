package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"strings"
	"sync"
	"testing"
	"time"
)

const emailChangePath = "/api/auth/email-change"

// All limits must still work with just one database connection. A handler
// must not hold a transaction while checking out another connection to meter
// its password, code or completion budget.
func TestEmailChangeWithSingleConnection(t *testing.T) {
	s, db, box := verificationServer(t, false)
	db.DB.SetMaxOpenConns(1)
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	h := router.New(s)
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody("single-connection@example.test"), session.CSRFToken, session.Cookie(), 202)
	deliverEmailChange(t, s, box)
	apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(box.code(t)), session.CSRFToken, session.Cookie(), 200)
}

func emailChangeBody(email string) string {
	return fmt.Sprintf(`{"email":%q,"currentPassword":%q}`, email, factory.DefaultPassword)
}

func deliverEmailChange(t *testing.T, s *httpapi.Server, box *inbox) {
	t.Helper()
	id, occurred := newestEvent(t, s.Repo.DB, "auth.email_change.requested")
	var payload []byte
	if err := s.Repo.DB.NewSelect().TableExpr("events").Column("payload").Where("public_id = ?", id).Scan(context.Background(), &payload); err != nil {
		t.Fatal(err)
	}
	var e events.EmailChangeRequested
	if err := json.Unmarshal(payload, &e); err != nil {
		t.Fatal(err)
	}
	listen := mail.EmailChange(s.Repo, box.mailer, apitest.Must(auth.NewKeys(s.Config.AppKey))(t), s.Config.Origin, s.Config.RateLimits != config.RateLimitsOff)
	if err := listen(context.Background(), e, events.Delivered{EventID: id, OccurredAt: occurred, Attempt: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestEmailChangePersistenceAndSessionBoundaries(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	other := apitest.Must(factory.SessionFor(ctx, db, f, session.UserID))(t)
	before, _, err := s.Repo.FindUser(ctx, apitest.EmailOf(t, db, session.UserID))
	if err != nil {
		t.Fatal(err)
	}
	ws := apitest.OwnedWorkspace(t, db, f, session.UserID)
	apitest.Must(factory.PasswordResets.State(factory.ResetFor(session.UserID)).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Admins.State(factory.AdminFor(session.UserID)).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(session.UserID)).CreateOne(ctx, db, f))(t)
	const destination = "replacement@example.test"
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(destination), session.CSRFToken, session.Cookie(), 202)
	if got := apitest.EmailOf(t, db, session.UserID); got != before.Email {
		t.Fatal("request changed the sign-in address")
	}
	status := apitest.Call(t, h, "GET", emailChangePath, "", "", session.Cookie(), 200)
	if !strings.Contains(status.Body.String(), destination) || !strings.Contains(status.Body.String(), "sending") {
		t.Fatal(status.Body.String())
	}
	// Another session cannot inspect, cancel or confirm the initiating one's request.
	apitest.Call(t, h, "DELETE", emailChangePath, "", other.CSRFToken, other.Cookie(), 204)
	hidden := apitest.Call(t, h, "GET", emailChangePath, "", "", other.Cookie(), 200)
	if hidden.Body.String() != "{\"pending\":null}\n" {
		t.Fatal(hidden.Body.String())
	}
	apitest.Call(t, h, "PATCH", emailChangePath, `{"code":"000000"}`, session.CSRFToken, session.Cookie(), 409)
	deliverEmailChange(t, s, box)
	code := box.code(t)
	apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(code), other.CSRFToken, other.Cookie(), 410)
	finished := apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(code), session.CSRFToken, session.Cookie(), 200)
	var body struct {
		User store.User `json:"user"`
		CSRF string     `json:"csrfToken"`
	}
	if err := json.Unmarshal(finished.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.User.PublicID != before.PublicID || body.User.Email != destination || body.User.EmailVerifiedAt == nil || body.CSRF == session.CSRFToken {
		t.Fatal(finished.Body.String())
	}
	if strings.Contains(finished.Body.String(), "emailVersion") || strings.Contains(finished.Body.String(), "emailChangedAt") {
		t.Fatal("internal generation exposed")
	}
	newCookie := mustCookie(t, finished, auth.CookieName(false))
	if newCookie.Value == session.Cookie().Value {
		t.Fatal("session wasn't rotated")
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", session.Cookie(), 401)
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", other.Cookie(), 401)
	apitest.Call(t, h, "GET", apitest.WorkspacePath(ws.PublicID, ""), "", "", newCookie, 200)
	for _, table := range []string{"admin_sessions", "password_resets", "email_changes", "mfa_challenges"} {
		n := apitest.Must(db.NewSelect().TableExpr(table).Where("user_id = ?", session.UserID).Count(ctx))(t)
		if n != 0 {
			t.Fatalf("%s credentials survived", table)
		}
	}
	// Password authentication that started with the old address cannot create
	// any kind of session/challenge after the committed generation change.
	if err := s.Repo.CreateSession(ctx, before, auth.Digest("stale"), "csrf", "", time.Now().Add(time.Hour), nil); !errors.Is(err, store.ErrNoChallenge) {
		t.Fatalf("stale app sign-in: %v", err)
	}
	if err := s.Repo.CreateMFAChallenge(ctx, before, auth.Digest("stale"), store.ChallengeLogin, time.Now().Add(time.Minute)); !errors.Is(err, store.ErrNoChallenge) {
		t.Fatalf("stale MFA sign-in: %v", err)
	}
	if err := s.Repo.CreateAdminSession(ctx, store.NewAdminSession{UserID: before.ID, EmailVersion: before.EmailVersion}, events.AdminLoggedIn{User: before.PublicID}); !errors.Is(err, store.ErrNoChallenge) {
		t.Fatalf("stale admin sign-in: %v", err)
	}
	// A queued old-address reset, including after A→B→A, cannot issue a token.
	if _, err := s.Repo.IssuePasswordReset(ctx, before.Email, "old", time.Now().Add(time.Minute), before.CreatedAt); err == nil {
		t.Fatal("old inbox got a working reset")
	}
	changedID, occurred := newestEvent(t, db, "auth.email_changed")
	for _, previous := range []bool{true, false} {
		listener := mail.EmailChangedNotice(s.Repo, box.mailer, s.Config.Origin, previous)
		if err := listener(ctx, events.EmailChanged{}, events.Delivered{EventID: changedID, OccurredAt: occurred, Attempt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if len(box.sent) != 3 || box.sent[1].To != before.Email || box.sent[2].To != destination || strings.Contains(box.sent[1].Text, destination) {
		t.Fatalf("completion mail: %+v", box.sent)
	}
	mailSummary := apitest.Must(s.Repo.FindAdminMailSummary(ctx, time.Now().Add(-time.Hour), nil))(t)
	volume := map[string]int{}
	for _, row := range mailSummary.Volume {
		volume[row.Kind] += row.Count
	}
	if volume[store.MailEmailChangeCode] != 1 || volume[store.MailEmailChangedPrevious] != 1 || volume[store.MailEmailChangedCurrent] != 1 {
		t.Fatalf("mail volume: %v", volume)
	}
	for _, row := range mailSummary.Requests {
		if row.Kind == store.MailEmailChange && (row.Requested != 1 || row.Sent != 1) {
			t.Fatalf("completion notices counted as requested codes: %+v", row)
		}
	}
	// New email signs in, old email no longer does; identity and password stay.
	apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(before.Email), "", nil, 401)
	apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(destination), "", nil, 200)
}

func TestEmailChangeCodesResendCancellationAndPrivacy(t *testing.T) {
	s, db, box := verificationServer(t, false)
	s.Config.RateLimits = config.RateLimitsOff
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	const destination = "pending-change@example.test"
	for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
		body := ""
		if method == "POST" || method == "PATCH" {
			body = "{}"
		}
		apitest.Call(t, h, method, emailChangePath, body, "", nil, 401)
		if method != "GET" {
			apitest.Call(t, h, method, emailChangePath, body, "", session.Cookie(), 403)
		}
	}
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(destination), session.CSRFToken, session.Cookie(), 202)
	id, _ := newestEvent(t, db, "auth.email_change.requested")
	deliverEmailChange(t, s, box)
	// A failed factor must not replace a usable pending request.
	bad := fmt.Sprintf(`{"email":%q,"currentPassword":"wrong"}`, destination)
	apitest.Call(t, h, "POST", emailChangePath, bad, session.CSRFToken, session.Cookie(), 422)
	if latest, _ := newestEvent(t, db, "auth.email_change.requested"); latest != id {
		t.Fatal("failed proof replaced a request")
	}
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(destination), session.CSRFToken, session.Cookie(), 202)
	if _, err := s.Repo.IssueEmailChangeCode(ctx, id, "stale"); !errors.Is(err, store.ErrNoEmailChange) {
		t.Fatalf("old delivery issued: %v", err)
	}
	deliverEmailChange(t, s, box)
	live := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t)
	latest, _ := newestEvent(t, db, "auth.email_change.requested")
	apitest.Must(s.Repo.IssueEmailChangeCode(ctx, latest, s.EmailCodeDigest("909090")))(t)
	afterRetry := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t)
	if !afterRetry.ExpiresAt.Equal(live.ExpiresAt) {
		t.Fatal("delivery retry extended the request's deadline")
	}
	// Use a definitely wrong digest instead of relying on two random codes differing.
	wrong := "000000"
	if box.code(t) == wrong {
		wrong = "111111"
	}
	for n := 0; n < 5; n++ {
		status := 422
		if n == 4 {
			status = 410
		}
		apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(wrong), session.CSRFToken, session.Cookie(), status)
	}
	if pending := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t); pending != nil {
		t.Fatal("fifth wrong code kept the request")
	}
	// A registered destination gets a notice, never a code; browser status
	// retains the same shape and the export cannot reveal the delivery outcome.
	existing := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(existing.Email), session.CSRFToken, session.Cookie(), 202)
	deliverEmailChange(t, s, box)
	if box.last(t).Template != mail.EmailChangeExists || emailedCode.MatchString(box.last(t).Text) || strings.Contains(box.last(t).Text, "permanently") == false {
		t.Fatal(box.last(t).Text)
	}
	exported := apitest.Must(s.Repo.FindAccountExport(ctx, session.UserID))(t)
	for _, event := range exported.Events {
		if event.Name == "auth.email_change.sent" {
			t.Fatal("export disclosed destination availability")
		}
	}
	apitest.Call(t, h, "DELETE", emailChangePath, "", session.CSRFToken, session.Cookie(), 204)
	apitest.Call(t, h, "DELETE", emailChangePath, "", session.CSRFToken, session.Cookie(), 204)
	if pending := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t); pending != nil {
		t.Fatal("cancel kept request")
	}
}

// Confirming a different address replaces the pending request: the page asks
// nothing about the old one, and its code, mail and deadline stop mattering.
func TestEmailChangeNewAddressReplacesThePendingRequest(t *testing.T) {
	s, db, box := verificationServer(t, false)
	s.Config.RateLimits = config.RateLimitsOff
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	const first, second = "first-choice@example.test", "second-choice@example.test"
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(first), session.CSRFToken, session.Cookie(), 202)
	firstID, _ := newestEvent(t, db, "auth.email_change.requested")
	deliverEmailChange(t, s, box)
	firstCode := box.code(t)
	// Unproven, the new address changes nothing: the first request stays usable.
	bad := fmt.Sprintf(`{"email":%q,"currentPassword":"wrong"}`, second)
	apitest.Call(t, h, "POST", emailChangePath, bad, session.CSRFToken, session.Cookie(), 422)
	if p := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t); p == nil || p.Email != first {
		t.Fatalf("a refused request replaced the first: %+v", p)
	}
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(second), session.CSRFToken, session.Cookie(), 202)
	pending := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t)
	if pending == nil || pending.Email != second {
		t.Fatalf("pending request after confirming the new address: %+v", pending)
	}
	if _, err := s.Repo.IssueEmailChangeCode(ctx, firstID, "stale"); !errors.Is(err, store.ErrNoEmailChange) {
		t.Fatalf("the replaced request's mail still issued a code: %v", err)
	}
	deliverEmailChange(t, s, box)
	if box.last(t).To != second {
		t.Fatalf("the new code went to %s", box.last(t).To)
	}
	secondCode := box.code(t)
	if firstCode == secondCode {
		t.Skip("two random codes matched")
	}
	apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(firstCode), session.CSRFToken, session.Cookie(), 422)
	apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(secondCode), session.CSRFToken, session.Cookie(), 200)
	if email := apitest.EmailOf(t, db, session.UserID); email != second {
		t.Fatalf("account email %s", email)
	}
}

func TestEmailChangeDirectAndUnavailable(t *testing.T) {
	s, db := server(t)
	s.Config.Verify, s.Config.MailDriver = false, config.MailOff
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	existing := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	blocked := apitest.Must(factory.EmailBlocks.CreateOne(ctx, db, f))(t)
	for _, email := range []string{existing.Email, blocked.Email} {
		w := apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(email), session.CSRFToken, session.Cookie(), 409)
		if !strings.Contains(w.Body.String(), "This email address cannot be used") {
			t.Fatal(w.Body.String())
		}
	}
	w := apitest.Call(t, h, "POST", emailChangePath, emailChangeBody("without-smtp@example.test"), session.CSRFToken, session.Cookie(), 200)
	var result struct {
		User store.User `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.User.EmailVerifiedAt != nil || result.User.Email != "without-smtp@example.test" {
		t.Fatal(w.Body.String())
	}
	if n := apitest.Must(db.NewSelect().Model((*factory.EmailChange)(nil)).Count(ctx))(t); n != 0 {
		t.Fatal("direct mode created a pending request")
	}
}

func TestEmailChangeReusedAddressErasure(t *testing.T) {
	s, db := server(t)
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	a := apitest.SignedIn(t, db, f)
	old := apitest.EmailOf(t, db, a.UserID)
	before := apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.Name = "auth.password_reset.requested"
		e.Payload = map[string]any{"email": old}
	}).CreateOne(ctx, db, f))(t)
	w := apitest.Call(t, h, "POST", emailChangePath, emailChangeBody("new-company@example.test"), a.CSRFToken, a.Cookie(), 200)
	b := apitest.Must(factory.Users.State(func(u *factory.User) { u.Email = old; u.CreatedAt = time.Now() }).CreateOne(ctx, db, f))(t)
	bSession := apitest.Must(factory.SessionFor(ctx, db, f, b.ID))(t)
	if _, err := s.Repo.IssuePasswordReset(ctx, old, "obsolete", time.Now().Add(time.Minute), before.CreatedAt); err == nil {
		t.Fatal("queued reset was reassigned to a later owner")
	}
	newEvent := apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.UserID = b.ID
		e.Name = "auth.login_attempted"
		e.CreatedAt = time.Now()
		e.Payload = map[string]any{"email": old}
	}).CreateOne(ctx, db, f))(t)
	anon := apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.CreatedAt = time.Now()
		e.Payload = map[string]any{"email": s.EmailPseudonym(old)}
	}).CreateOne(ctx, db, f))(t)
	cookie := mustCookie(t, w, auth.CookieName(false))
	current := apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie, 200)
	var session struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	apitest.Call(t, h, "DELETE", "/api/auth/me", fmt.Sprintf(`{"password":%q}`, factory.DefaultPassword), session.CSRF, cookie, 204)
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", bSession.Cookie(), 200)
	for _, e := range []factory.Event{newEvent, anon} {
		var payload []byte
		apitest.Must(0, db.NewSelect().Model((*factory.Event)(nil)).Column("payload").Where("id = ?", e.ID).Scan(ctx, &payload))(t)
		if strings.Contains(string(payload), "[deleted]") {
			t.Fatalf("new owner's record erased: %s", payload)
		}
	}
	var redacted []byte
	apitest.Must(0, db.NewSelect().Model((*factory.Event)(nil)).Column("payload").Where("id = ?", before.ID).Scan(ctx, &redacted))(t)
	if !strings.Contains(string(redacted), "[deleted]") {
		t.Fatal("former address reference survived erasure")
	}
}

func TestEmailChangeMFAAndCompletionLimit(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	session := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	const destination = "with-mfa@example.test"
	missing := apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(destination), session.CSRFToken, session.Cookie(), 422)
	if apitest.ErrorField(t, missing) != "code" {
		t.Fatal("password bypassed MFA")
	}
	body := fmt.Sprintf(`{"email":%q,"code":%q}`, destination, a.Codes[0].Code)
	apitest.Call(t, h, "POST", emailChangePath, body, session.CSRFToken, session.Cookie(), 202)
	apitest.Call(t, h, "POST", emailChangePath, body, session.CSRFToken, session.Cookie(), 422)
	deliverEmailChange(t, s, box)
	code := box.code(t)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("email-change-complete:per-user", a.User.PublicID)).State(factory.Exhausted(1)).CreateOne(ctx, db, f))(t)
	w := apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(code), session.CSRFToken, session.Cookie(), 429)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("completion refusal has no wait")
	}
	if pending := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t); pending == nil {
		t.Fatal("limit destroyed usable pending request")
	}
	if got := apitest.EmailOf(t, db, session.UserID); got != a.User.Email {
		t.Fatal("limit changed email")
	}
	// Unblocking the test budget lets exactly that code finish; confirmed MFA
	// and remaining recovery codes survive completion.
	if _, err := db.NewDelete().Model((*factory.RateLimit)(nil)).Where("key = ?", httpapi.EmailChangeCompletePerUser.Key(a.User.PublicID)).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(code), session.CSRFToken, session.Cookie(), 200)
	if n := apitest.Must(db.NewSelect().Model((*factory.MFATOTP)(nil)).Where("user_id = ? AND confirmed_at IS NOT NULL", a.User.ID).Count(ctx))(t); n != 1 {
		t.Fatal("MFA lost")
	}
}

func TestEmailChangeUnavailableAfterCodeAndBlockedPrivacy(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	const destination = "claimed-after-code@example.test"
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(destination), session.CSRFToken, session.Cookie(), 202)
	deliverEmailChange(t, s, box)
	code := box.code(t)
	apitest.Must(factory.Users.State(func(u *factory.User) { u.Email = destination }).CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(code), session.CSRFToken, session.Cookie(), 409)
	if pending := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t); pending != nil {
		t.Fatal("unavailable change remained active")
	}
	blocked := apitest.Must(factory.EmailBlocks.CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(blocked.Email), session.CSRFToken, session.Cookie(), 202)
	n := len(box.sent)
	deliverEmailChange(t, s, box)
	if len(box.sent) != n {
		t.Fatal("blocked inbox got mail")
	}
	status := apitest.Call(t, h, "GET", emailChangePath, "", "", session.Cookie(), 200)
	if !strings.Contains(status.Body.String(), `"state":"code"`) || strings.Contains(status.Body.String(), "blocked") && !strings.Contains(blocked.Email, "blocked") {
		t.Fatal(status.Body.String())
	}
}

func TestEmailChangeConcurrentClaims(t *testing.T) {
	s, db, box := verificationServer(t, false)
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	sessions := []factory.Session{apitest.SignedIn(t, db, f), apitest.SignedIn(t, db, f)}
	const email = "one-winner@example.test"
	codes := make([]string, 2)
	for i, session := range sessions {
		apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(email), session.CSRFToken, session.Cookie(), 202)
		deliverEmailChange(t, s, box)
		codes[i] = box.code(t)
	}
	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i, session := range sessions {
		wg.Go(func() {
			<-start
			w := apitest.Send(h, "PATCH", emailChangePath, apitest.CodeBody(codes[i]), session.CSRFToken, session.Cookie())
			statuses <- w.Code
		})
	}
	close(start)
	wg.Wait()
	close(statuses)
	winners, conflicts := 0, 0
	for status := range statuses {
		if status == 200 {
			winners++
		} else if status == 409 {
			conflicts++
		} else {
			t.Fatalf("claim returned %d", status)
		}
	}
	if winners != 1 || conflicts != 1 || countUsers(t, db, email) != 1 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
	_ = ctx
}

func TestEmailChangeAtVerificationGate(t *testing.T) {
	s, db, box := verificationServer(t, true)
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	u := apitest.Must(factory.Users.State(factory.Unverified).CreateOne(ctx, db, f))(t)
	session := apitest.Must(factory.SessionFor(ctx, db, f, u.ID))(t)
	apitest.Call(t, h, "GET", "/api/workspaces", "", "", session.Cookie(), 403)
	apitest.Call(t, h, "POST", emailChangePath, emailChangeBody("corrected@example.test"), session.CSRFToken, session.Cookie(), 202)
	deliverEmailChange(t, s, box)
	w := apitest.Call(t, h, "PATCH", emailChangePath, apitest.CodeBody(box.code(t)), session.CSRFToken, session.Cookie(), 200)
	apitest.Call(t, h, "GET", "/api/workspaces", "", "", mustCookie(t, w, auth.CookieName(false)), 200)
}

func TestEmailChangeCancelledByCredentialChanges(t *testing.T) {
	for _, action := range []string{"password", "mfa", "recovery-codes"} {
		t.Run(action, func(t *testing.T) {
			s, db, _ := verificationServer(t, false)
			ctx, f := context.Background(), factory.TestFaker(t)
			a := apitest.WithMFA(t, db, f)
			session := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
			h := router.New(s)
			body := fmt.Sprintf(`{"email":"cancelled-by-security@example.test","code":%q}`, a.Codes[0].Code)
			apitest.Call(t, h, "POST", emailChangePath, body, session.CSRFToken, session.Cookie(), 202)
			id, _ := newestEvent(t, db, "auth.email_change.requested")
			switch action {
			case "password":
				apitest.Call(t, h, "PATCH", "/api/auth/password", fmt.Sprintf(`{"newPassword":%q,"code":%q}`, apitest.NewPassword, a.Codes[1].Code), session.CSRFToken, session.Cookie(), 200)
			case "mfa":
				apitest.Call(t, h, "DELETE", "/api/auth/mfa", fmt.Sprintf(`{"password":%q,"code":%q}`, factory.DefaultPassword, a.Codes[1].Code), session.CSRFToken, session.Cookie(), 204)
			case "recovery-codes":
				apitest.Call(t, h, "POST", "/api/auth/mfa/recovery-codes", fmt.Sprintf(`{"password":%q,"code":%q}`, factory.DefaultPassword, a.Codes[1].Code), session.CSRFToken, session.Cookie(), 201)
			}
			if p := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t); p != nil {
				t.Fatal("credential change left a pending email change")
			}
			if _, err := s.Repo.IssueEmailChangeCode(ctx, id, "obsolete"); !errors.Is(err, store.ErrNoEmailChange) {
				t.Fatalf("stale mail issued after a credential change: %v", err)
			}
		})
	}
}

func TestEmailChangeRejectsPreviousOwnershipWork(t *testing.T) {
	s, db := server(t)
	s.Config.RateLimits = config.RateLimitsOff
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	email := apitest.EmailOf(t, db, session.UserID)
	u, _, err := s.Repo.FindUser(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	queuedAt := time.Now()
	w := apitest.Call(t, h, "POST", emailChangePath, emailChangeBody("temporary-owner@example.test"), session.CSRFToken, session.Cookie(), 200)
	var signed apitest.SessionResponse
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &signed))(t)
	// Reclaim the same address. An old reset job must not treat the string
	// matching again as proof this is the same ownership period.
	w = apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(email), signed.CSRF, mustCookie(t, w, auth.CookieName(false)), 200)
	if _, err := s.Repo.IssuePasswordReset(ctx, email, "stale", time.Now().Add(time.Minute), queuedAt); err == nil {
		t.Fatal("old queued reset survived A→B→A")
	}
	if _, err := s.Repo.IssueEmailVerification(ctx, u.PublicID, email, "stale", time.Now().Add(time.Minute), queuedAt); !errors.Is(err, store.ErrAlreadyVerified) {
		t.Fatalf("old verification job: %v", err)
	}
	if _, err := s.Repo.IssuePasswordReset(ctx, email, "fresh", time.Now().Add(time.Minute), time.Now()); err != nil {
		t.Fatalf("fresh reset refused: %v", err)
	}
	if _, err := s.Repo.IssueEmailVerification(ctx, u.PublicID, email, "fresh", time.Now().Add(time.Minute), time.Now()); err != nil {
		t.Fatalf("fresh verification refused: %v", err)
	}
	_ = w
}

func TestEmailChangeRequestLimitsPreservePending(t *testing.T) {
	for _, limit := range []string{"email-change:per-user", "email-change:per-email", "email-change:global", "auth:per-account"} {
		t.Run(limit, func(t *testing.T) {
			s, db, _ := verificationServer(t, false)
			h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
			session := apitest.SignedIn(t, db, f)
			email := apitest.EmailOf(t, db, session.UserID)
			var owner factory.User
			apitest.Must(0, db.NewSelect().Model(&owner).Where("id = ?", session.UserID).Scan(ctx))(t)
			pending := apitest.Must(factory.EmailChanges.State(factory.EmailChangeFor(owner, session)).CreateOne(ctx, db, f))(t)
			const destination = "limited-change@example.test"
			subject, bound, status := owner.PublicID, 6, 429
			switch limit {
			case "email-change:per-email":
				subject, bound = destination, 3
			case "email-change:global":
				subject, bound, status = "", 200, 503
			case "auth:per-account":
				subject, bound = email, 10
			}
			apitest.Must(factory.RateLimits.State(factory.ForLimit(limit, subject)).State(factory.Exhausted(bound)).CreateOne(ctx, db, f))(t)
			w := apitest.Call(t, h, "POST", emailChangePath, emailChangeBody(destination), session.CSRFToken, session.Cookie(), status)
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("missing wait")
			}
			if len(apitest.EventsNamed(t, db, "auth.email_change.requested")) != 0 {
				t.Fatal("refused request queued mail")
			}
			live := apitest.Must(s.Repo.FindEmailChange(ctx, session.UserID, session.TokenHash))(t)
			if live == nil || live.Email != pending.Email {
				t.Fatal("limit replaced pending request")
			}
		})
	}
}

func TestEmailChangeDirectCompletionLimitDoesNotConsumeRecoveryProof(t *testing.T) {
	s, db := server(t)
	ctx, f := context.Background(), factory.TestFaker(t)
	a := apitest.WithMFA(t, db, f)
	session := apitest.Must(factory.SessionFor(ctx, db, f, a.User.ID))(t)
	h := router.New(s)
	limit := httpapi.EmailChangeCompletePerUser
	apitest.Must(factory.RateLimits.State(factory.ForLimit(limit.Name, a.User.PublicID)).State(factory.Exhausted(1)).CreateOne(ctx, db, f))(t)
	body := fmt.Sprintf(`{"email":"direct-limit@example.test","code":%q}`, a.Codes[0].Code)
	for range 2 {
		apitest.Call(t, h, "POST", emailChangePath, body, session.CSRFToken, session.Cookie(), 429)
	}
	if len(apitest.EventsNamed(t, db, "auth.email_change.requested")) != 0 {
		t.Fatal("limited direct change queued mail")
	}
	reached := 0
	for _, event := range apitest.EventsNamed(t, db, "rate_limit.reached") {
		if event.Payload["limit"] == limit.Name+":block" {
			reached++
		}
	}
	if reached != 1 {
		t.Fatalf("completion refusal events: %d", reached)
	}
	apitest.Must(db.NewDelete().Model((*factory.RateLimit)(nil)).Where("key = ?", limit.Key(a.User.PublicID)).Exec(ctx))(t)
	apitest.Call(t, h, "POST", emailChangePath, body, session.CSRFToken, session.Cookie(), 200)
}

// accountWait observes the boundary after session middleware, immediately
// before the controller waits for the account lock. No timing sleeps.
type accountWait struct {
	ready chan struct{}
	once  sync.Once
}

func (q *accountWait) BeforeQuery(ctx context.Context, e *bun.QueryEvent) context.Context {
	if strings.Contains(e.Query, "FOR NO KEY UPDATE OF u") {
		q.once.Do(func() { close(q.ready) })
	}
	return ctx
}
func (*accountWait) AfterQuery(context.Context, *bun.QueryEvent) {}

func TestEmailChangeRechecksRevokedSessionAfterLock(t *testing.T) {
	s, db, _ := verificationServer(t, false)
	h, ctx, f := router.New(s), context.Background(), factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	tx := apitest.Must(db.BeginTx(ctx, nil))(t)
	defer tx.Rollback()
	var id int64
	apitest.Must(0, tx.NewSelect().Model((*factory.User)(nil)).Column("id").Where("id = ?", session.UserID).For("UPDATE").Scan(ctx, &id))(t)
	probe := &accountWait{ready: make(chan struct{})}
	db.AddQueryHook(probe)
	response := make(chan int, 1)
	go func() {
		response <- apitest.Send(h, "POST", emailChangePath, emailChangeBody("revoked-session@example.test"), session.CSRFToken, session.Cookie()).Code
	}()
	select {
	case <-probe.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached account lock")
	}
	apitest.Must(tx.NewDelete().Model((*factory.Session)(nil)).Where("token_hash = ?", session.TokenHash).Exec(ctx))(t)
	apitest.Must(0, tx.Commit())(t)
	select {
	case status := <-response:
		if status != 401 {
			t.Fatalf("revoked session issued a request: %d", status)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("request deadlocked after session revocation")
	}
	if len(apitest.EventsNamed(t, db, "auth.email_change.requested")) != 0 {
		t.Fatal("revoked session queued mail")
	}
}
