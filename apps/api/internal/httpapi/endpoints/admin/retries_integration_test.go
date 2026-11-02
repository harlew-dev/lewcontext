package admin_test

import (
	"context"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"regexp"
	"strings"
	"testing"
	"time"
)

// resetLink finds a reset email's token.
var resetLink = regexp.MustCompile(`/auth/forgot-password/([A-Za-z0-9_-]{43})`)

// Retrying a failed reset delivery runs its listener again: a fresh link
// is made and sent, and the old one stops working, so an old link is never
// resent. Only a failed delivery can be retried (409 otherwise), and an
// unknown one is 404. Recorded as admin.delivery.retried.
func TestAdminRetryDelivery(t *testing.T) {
	s, db, box, deliver := mailServer(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "POST", "/api/auth/password-reset", fmt.Sprintf(`{"email":%q}`, user.Email), "", nil, 202)
	var event string
	apitest.Must(0, db.NewSelect().TableExpr("events").Column("public_id").Where("name = ?", "auth.password_reset.requested").Scan(ctx, &event))(t)
	path := "/api/admin/deliveries/" + event + "/mail.password_reset/retry"
	// Pending: 409.
	if w := adminCall(t, h, "POST", path, reasonBody(reason), session.CSRFToken, 409, session.Cookie()); !strings.Contains(w.Body.String(), "hasn’t failed") {
		t.Fatalf("pending: %s", w.Body.String())
	}
	deliver()
	first := resetLink.FindStringSubmatch(box.Sent[0].HTML)[1]
	// It was delivered, but say the SMTP server lost it and it was marked
	// failed: the retry makes a new link.
	apitest.Must(db.NewUpdate().TableExpr("event_deliveries").Set("delivered_at = NULL").Set("failed_at = now()").Set("attempts = 8").Set("last_error = 'smtp: timeout'").Where("listener = 'mail.password_reset'").Exec(ctx))(t)
	adminCall(t, h, "POST", path, reasonBody(reason), session.CSRFToken, 204, session.Cookie())
	var attempts int
	apitest.Must(0, db.NewSelect().TableExpr("event_deliveries").Column("attempts").Where("listener = 'mail.password_reset'").Scan(ctx, &attempts))(t)
	if attempts != 0 {
		t.Fatalf("attempts after the retry: %d", attempts)
	}
	deliver()
	if len(box.Sent) != 2 {
		t.Fatalf("sent: %d", len(box.Sent))
	}
	second := resetLink.FindStringSubmatch(box.Sent[1].HTML)[1]
	if second == first {
		t.Fatal("the old link was sent again")
	}
	apitest.Call(t, h, "POST", "/api/auth/password-reset/check", apitest.TokenBody(first), "", nil, 422)
	apitest.Call(t, h, "POST", "/api/auth/password-reset/check", apitest.TokenBody(second), "", nil, 204)
	e := apitest.OnlyEvent(t, db, "admin.delivery.retried")
	if e.Payload["event"] != event || e.Payload["listener"] != "mail.password_reset" || e.Payload["count"] != float64(1) || !apitest.Is(e.ActorID, operator.User.ID) {
		t.Fatalf("admin.delivery.retried: %+v", e.Payload)
	}
	// Unknown: 404 (an event, a listener, a malformed ID).
	for _, p := range []string{
		"/api/admin/deliveries/evt_2yEHYLwiwPVhLW/mail.password_reset/retry",
		"/api/admin/deliveries/" + event + "/mail.nothing/retry",
		"/api/admin/deliveries/nope/mail.password_reset/retry",
	} {
		adminCall(t, h, "POST", p, reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	}
}

// A retried invitation to an address blocked meanwhile isn't sent:
// mailer.Send checks the block list on every send.
func TestAdminRetryToABlockedAddress(t *testing.T) {
	s, db, box, deliver := mailServer(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	apitest.Call(t, h, "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), `{"email":"ada@example.com","role":"member"}`, owner.CSRFToken, owner.Cookie(), 201)
	apitest.Must(db.NewUpdate().TableExpr("event_deliveries").Set("failed_at = now()").Where("listener = 'mail.workspace_invitation'").Exec(ctx))(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked("ada@example.com")).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "POST", "/api/admin/deliveries/retry", reasonBody(reason, "listener", "mail.workspace_invitation", "failedSince", time.Now().Add(-time.Hour).Format(time.RFC3339)), session.CSRFToken, 200, session.Cookie())
	deliver()
	if len(box.Sent) != 0 {
		t.Fatalf("sent to a blocked address: %+v", box.Sent)
	}
}

// The bulk retry: one listener's failures since a time no more than 7 days
// ago, the most recent 1,000 at most, with the count in its answer and its
// event; 409 when none failed; 422 for an unknown listener or a time out of
// bounds.
func TestAdminRetryFailedDeliveries(t *testing.T) {
	s, db, _, _ := mailServer(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	events := apitest.Must(factory.Events.Create(ctx, db, f, 1003))(t)
	recently := time.Now().Add(-10 * time.Minute)
	for i, e := range events {
		failedAt := recently.Add(time.Duration(i) * time.Millisecond)
		if i == 1002 {
			failedAt = time.Now().Add(-2 * time.Hour) // before failedSince
		}
		apitest.Must(factory.DeliveryOf(e.ID, "mail.password_reset").State(func(d *factory.EventDelivery) { d.FailedAt = failedAt }).CreateOne(ctx, db, f))(t)
	}
	// Another listener's failure isn't touched.
	apitest.Must(factory.DeliveryOf(events[0].ID, "mail.workspace_invitation").State(func(d *factory.EventDelivery) { d.FailedAt = recently }).CreateOne(ctx, db, f))(t)
	since := time.Now().Add(-time.Hour).Format(time.RFC3339)
	retry := func(listener, failedSince string, want int) string {
		t.Helper()
		return adminCall(t, h, "POST", "/api/admin/deliveries/retry", reasonBody(reason, "listener", listener, "failedSince", failedSince), session.CSRFToken, want, session.Cookie()).Body.String()
	}
	if body := retry("mail.password_reset", since, 200); strings.TrimSpace(body) != `{"count":1000}` {
		t.Fatalf("the first retry: %s", body)
	}
	// The oldest of the 1,002 since then is left for a second run.
	if body := retry("mail.password_reset", since, 200); strings.TrimSpace(body) != `{"count":2}` {
		t.Fatalf("the second retry: %s", body)
	}
	retry("mail.password_reset", since, 409)
	failed := apitest.Must(db.NewSelect().TableExpr("event_deliveries").Where("failed_at IS NOT NULL").Count(ctx))(t)
	if failed != 2 {
		t.Fatalf("failures left: %d", failed)
	}
	retried := apitest.EventsNamed(t, db, "admin.delivery.retried")
	if len(retried) != 2 || retried[0].Payload["count"] != float64(1000) || retried[0].Payload["failedSince"] == nil || retried[0].Payload["event"] != nil {
		t.Fatalf("admin.delivery.retried: %+v", retried)
	}
	if w := adminCall(t, h, "POST", "/api/admin/deliveries/retry", reasonBody(reason, "listener", "mail.nothing", "failedSince", since), session.CSRFToken, 422, session.Cookie()); apitest.ErrorField(t, w) != "listener" {
		t.Fatalf("an unknown listener: %s", w.Body.String())
	}
	for _, at := range []string{time.Now().Add(-8 * 24 * time.Hour).Format(time.RFC3339), time.Now().Add(time.Hour).Format(time.RFC3339)} {
		if w := adminCall(t, h, "POST", "/api/admin/deliveries/retry", reasonBody(reason, "listener", "mail.password_reset", "failedSince", at), session.CSRFToken, 422, session.Cookie()); apitest.ErrorField(t, w) != "failedSince" {
			t.Fatalf("failedSince %s: %s", at, w.Body.String())
		}
	}
	adminCall(t, h, "POST", "/api/admin/deliveries/retry", reasonBody(reason, "listener", "mail.password_reset", "failedSince", "yesterday"), session.CSRFToken, 400, session.Cookie())
}
