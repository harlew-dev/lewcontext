package admin_test

import (
	"context"
	"lewcontext/api/internal/config"
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

// mailFixture is a queue in every state, for the Mail page's tests.
type mailFixture struct {
	delivered, failed, retrying, due, old, both factory.Event
}

// seedMail queues deliveries in every state, and the sent events behind the
// volume, through factories.
func seedMail(t *testing.T, s *httpapi.Server) mailFixture {
	t.Helper()
	ctx := context.Background()
	db := s.Repo.DB
	f := factory.TestFaker(t)
	now := time.Now().Truncate(time.Microsecond)
	event := func(name string, payload map[string]any, ago time.Duration) factory.Event {
		e := factory.Events.State(factory.EventOf(name, payload)).State(func(e *factory.Event) { e.CreatedAt = now.Add(-ago) })
		return apitest.Must(e.CreateOne(ctx, db, f))(t)
	}
	deliver := func(e factory.Event, listener string, state func(*factory.EventDelivery)) {
		apitest.Must(factory.DeliveryOf(e.ID, listener).State(state).CreateOne(ctx, db, f))(t)
	}
	reset := "auth.password_reset.requested"
	var m mailFixture
	m.delivered = event(reset, map[string]any{"email": "flood@example.test"}, 5*time.Hour)
	deliver(m.delivered, "mail.password_reset", func(d *factory.EventDelivery) { d.Attempts, d.DeliveredAt = 1, now.Add(-5*time.Hour) })
	m.failed = event(reset, map[string]any{"email": "flood@example.test"}, 4*time.Hour)
	deliver(m.failed, "mail.password_reset", func(d *factory.EventDelivery) {
		d.Attempts, d.FailedAt, d.LastError = 6, now.Add(-3*time.Hour), "550 5.1.1 <flood@example.test>: mailbox unavailable"
	})
	m.retrying = event(reset, map[string]any{"email": "Flood@example.test"}, 3*time.Hour)
	deliver(m.retrying, "mail.password_reset", func(d *factory.EventDelivery) {
		d.Attempts, d.NextAttemptAt, d.LastError = 2, now.Add(time.Minute), "dial tcp: connection refused"
	})
	m.due = event("workspace.invitation.created", map[string]any{"email": "once@example.test", "invitation": "inv_2yEHYLwiwPVhLW", "workspace": "wsp_2yEHYLwiwPVhLW", "role": "member"}, 2*time.Hour)
	deliver(m.due, "mail.workspace_invitation", func(d *factory.EventDelivery) { d.NextAttemptAt = now.Add(-time.Minute) })
	// Three days old: in the 7-day window only.
	m.old = event(reset, map[string]any{"email": "old@example.test"}, 72*time.Hour)
	deliver(m.old, "mail.password_reset", func(d *factory.EventDelivery) {
		d.Attempts, d.FailedAt, d.LastError = 6, now.Add(-71*time.Hour), "old failure"
	})
	// One event, two listeners: the cursor's last key.
	m.both = event("auth.password_reset.requested", map[string]any{"email": "both@example.test"}, time.Hour)
	deliver(m.both, "mail.password_reset", func(d *factory.EventDelivery) { d.Attempts, d.DeliveredAt = 1, now.Add(-time.Hour) })
	deliver(m.both, "mail.workspace_invitation", func(d *factory.EventDelivery) { d.Attempts, d.DeliveredAt = 1, now.Add(-time.Hour) })

	event("auth.password_reset.sent", map[string]any{"email": "flood@example.test", "user": "usr_2yEHYLwiwPVhLW", "request": m.delivered.PublicID}, 5*time.Hour)
	event("workspace.invitation.sent", map[string]any{"invitation": "inv_2yEHYLwiwPVhLW", "workspace": "wsp_2yEHYLwiwPVhLW"}, 90*time.Minute)
	event("auth.registration.requested", map[string]any{"email": "new@example.test"}, 2*time.Hour)
	event("auth.registration.sent", map[string]any{"email": "new@example.test", "existingAccount": false}, 2*time.Hour)
	event("auth.registration.sent", map[string]any{"email": "taken@example.test", "existingAccount": true, "user": "usr_2yEHYLwiwPVhLW"}, 2*time.Hour)
	event("auth.email_verification.sent", map[string]any{"user": "usr_2yEHYLwiwPVhLW"}, 30*time.Hour)
	return m
}

func TestAdminMail(t *testing.T) {
	s, db, _, _ := mailServer(t)
	s.Config.Mail = config.Mail{MailDriver: config.MailSMTP, MailFromAddress: "no-reply@example.test", MailFromName: "lewcontext",
		SMTP: config.SMTP{Host: "smtp.example.test", Port: 587, TLS: config.SMTPStartTLS, Username: "relay-user", Password: "hunter2-secret"}}
	h := router.New(s)
	admin, session := adminSignedIn(t, db, factory.TestFaker(t))
	cookie := session.Cookie()
	m := seedMail(t, s)

	// driver is the summary's mail driver, which never holds the SMTP
	// username or password.
	type driver struct {
		Driver      string `json:"driver"`
		Host        string `json:"host"`
		Port        int    `json:"port"`
		TLS         string `json:"tls"`
		FromAddress string `json:"fromAddress"`
		FromName    string `json:"fromName"`
	}
	type summary struct {
		Window string `json:"window"`
		Driver driver `json:"driver"`
		store.AdminMailSummary
	}
	w := adminCall(t, h, "GET", "/api/admin/mail", "", "", 200, cookie)
	if body := w.Body.String(); strings.Contains(body, "hunter2") || strings.Contains(body, "relay-user") {
		t.Fatal("the SMTP credentials in the summary")
	}
	day := decodeAdmin[summary](t, w.Body.Bytes())
	if day.Window != "24h" || day.Driver.Driver != "smtp" || day.Driver.Host != "smtp.example.test" || day.Driver.Port != 587 || day.Driver.TLS != "starttls" || day.Driver.FromAddress != "no-reply@example.test" {
		t.Fatalf("window and driver: %+v", day)
	}
	if len(day.Queue) != 2 || day.Queue[0].Listener != "mail.password_reset" || day.Queue[1].Listener != "mail.workspace_invitation" {
		t.Fatalf("queue rows: %+v", day.Queue)
	}
	resets, invitations := day.Queue[0], day.Queue[1]
	// Waiting deliveries count whatever their age; finished ones only in the window.
	if resets.Due != 0 || resets.Retrying != 1 || resets.Failed != 1 || resets.Delivered != 2 ||
		resets.OldestPendingAt == nil || !resets.OldestPendingAt.Equal(m.retrying.CreatedAt) {
		t.Fatalf("reset queue: %+v", resets)
	}
	if invitations.Due != 1 || invitations.Retrying != 0 || invitations.Delivered != 1 || invitations.OldestPendingAt == nil || !invitations.OldestPendingAt.Equal(m.due.CreatedAt) {
		t.Fatalf("invitation queue: %+v", invitations)
	}
	kinds := map[string]int{}
	for _, v := range day.Volume {
		kinds[v.Kind] += v.Count
		if !v.Hour.Equal(v.Hour.Truncate(time.Hour)) {
			t.Fatalf("volume not per hour: %+v", v)
		}
	}
	if kinds[store.MailPasswordReset] != 1 || kinds[store.MailInvitation] != 1 || kinds[store.MailRegistrationCode] != 1 || kinds[store.MailAccountExists] != 1 || kinds[store.MailEmailVerification] != 0 {
		t.Fatalf("volume in 24 hours: %+v", kinds)
	}
	wantRequests := []store.AdminRequests{{Kind: "password_reset", Requested: 4, Sent: 1}, {Kind: "registration", Requested: 1, Sent: 2}, {Kind: "email_verification"}}
	if !slices.Equal(day.Requests, wantRequests) {
		t.Fatalf("requests: %+v", day.Requests)
	}
	// Only addresses asked for 3 or more times, compared lower-case.
	if len(day.TopRecipients) != 1 || day.TopRecipients[0] != (store.AdminRecipient{Email: "flood@example.test", Total: 3, Resets: 3}) {
		t.Fatalf("top recipients: %+v", day.TopRecipients)
	}
	if day.LastDelivered == nil || day.LastFailure == nil || day.LastFailure.Listener != "mail.password_reset" || !strings.Contains(day.LastFailure.Error, "mailbox unavailable") {
		t.Fatalf("last delivery and failure: %v %+v", day.LastDelivered, day.LastFailure)
	}

	week := decodeAdmin[summary](t, adminCall(t, h, "GET", "/api/admin/mail?window=7d", "", "", 200, cookie).Body.Bytes())
	if week.Window != "7d" || week.Queue[0].Failed != 2 || week.Requests[0].Requested != 5 || week.Volume[0].Kind != store.MailEmailVerification {
		t.Fatalf("seven days: %+v", week)
	}
	for _, query := range []string{"?window=1h", "?window=24h&window=7d", "?status=failed"} {
		adminCall(t, h, "GET", "/api/admin/mail"+query, "", "", 400, cookie)
	}
	viewed := apitest.EventsNamed(t, db, "admin.mail.viewed")
	if len(viewed) != 2 || viewed[0].Payload["window"] != "24h" || viewed[1].Payload["window"] != "7d" || viewed[0].Payload["admin"] != admin.User.PublicID {
		t.Fatalf("admin.mail.viewed: %+v", viewed)
	}
}

// listDeliveries loads the deliveries list with query.
func listDeliveries(t *testing.T, h http.Handler, cookie *http.Cookie, query string) store.AdminDeliveryPage {
	t.Helper()
	return decodeAdmin[store.AdminDeliveryPage](t, adminCall(t, h, "GET", "/api/admin/deliveries"+query, "", "", 200, cookie).Body.Bytes())
}

// deliveryKeys are a page's rows as event:listener.
func deliveryKeys(page store.AdminDeliveryPage) []string {
	keys := make([]string, len(page.Data))
	for i, d := range page.Data {
		keys[i] = d.Event.PublicID + ":" + d.Listener
	}
	return keys
}

func TestAdminDeliveries(t *testing.T) {
	s, db, _, _ := mailServer(t)
	h := router.New(s)
	_, session := adminSignedIn(t, db, factory.TestFaker(t))
	cookie := session.Cookie()
	m := seedMail(t, s)
	key := func(e factory.Event, listener string) string { return e.PublicID + ":" + listener }

	all := []string{
		key(m.both, "mail.workspace_invitation"), key(m.both, "mail.password_reset"),
		key(m.due, "mail.workspace_invitation"), key(m.retrying, "mail.password_reset"),
		key(m.failed, "mail.password_reset"), key(m.delivered, "mail.password_reset"), key(m.old, "mail.password_reset"),
	}
	first := listDeliveries(t, h, cookie, "")
	if got := deliveryKeys(first); !slices.Equal(got, all) || first.Next != nil {
		t.Fatalf("every delivery: %v, want %v", got, all)
	}
	row := first.Data[3]
	if row.Status != store.DeliveryRetrying || row.To == nil || *row.To != "Flood@example.test" || row.NextAttemptAt == nil || row.Event.Name != "auth.password_reset.requested" || row.LastError == "" {
		t.Fatalf("the retrying row: %+v", row)
	}

	// The cursor keeps one event's two deliveries apart.
	var walked []string
	query := "?perPage=1"
	for range len(all) + 1 {
		page := listDeliveries(t, h, cookie, query)
		walked = append(walked, deliveryKeys(page)...)
		if page.Next == nil {
			break
		}
		query = "?perPage=1&before=" + *page.Next
	}
	if !slices.Equal(walked, all) {
		t.Fatalf("walking pages: %v, want %v", walked, all)
	}

	for query, want := range map[string][]string{
		"?status=pending":  {key(m.due, "mail.workspace_invitation")},
		"?status=retrying": {key(m.retrying, "mail.password_reset")},
		"?status=failed":   {key(m.failed, "mail.password_reset"), key(m.old, "mail.password_reset")},
		"?status=delivered&listener=mail.workspace_invitation":                            {key(m.both, "mail.workspace_invitation")},
		"?status=failed&from=" + time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339): {key(m.failed, "mail.password_reset")},
	} {
		if got := deliveryKeys(listDeliveries(t, h, cookie, query)); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", query, got, want)
		}
	}
	for _, query := range []string{
		"?status=bounced", "?listener=mail.nope", "?before=" + m.due.PublicID, "?before=evt_x:mail.password_reset",
		"?before=evt_2yEHYLwiwPVhLW:mail.password_reset", "?perPage=101", "?from=today", "?page=2",
	} {
		adminCall(t, h, "GET", "/api/admin/deliveries"+query, "", "", 400, cookie)
	}
	listed := apitest.EventsNamed(t, db, "admin.delivery.listed")
	var failedFilter bool
	for _, e := range listed {
		if filters, _ := e.Payload["filters"].(map[string]any); filters["status"] == "failed" && filters["listener"] == nil {
			failedFilter = true
		}
	}
	if !failedFilter || listed[0].UserID.Valid || listed[0].WorkspaceID.Valid {
		t.Fatalf("admin.delivery.listed: %+v", listed)
	}
}
