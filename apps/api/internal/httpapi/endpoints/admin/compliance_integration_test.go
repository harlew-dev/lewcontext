package admin_test

import (
	"context"
	"encoding/csv"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"time"
)

// The Events list as CSV: the page's filters, newest first, every cell
// safe for a spreadsheet, at most 10,000 rows (more says so), and one
// admin.event.exported with the filters and the row count. Any admin may
// (it's a read); a cursor or perPage is refused.
func TestAdminEventExport(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	at := time.Now().Add(-time.Hour)
	apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.Name, e.CreatedAt, e.IP = "auth.login_attempted", at, "198.51.100.7"
		e.Payload = map[string]any{"email": "=HYPERLINK(\"http://evil.test\")@example.test", "result": "wrong_password"}
	}).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.Name, e.CreatedAt = "project.created", at.Add(time.Minute)
		e.Payload = map[string]any{"project": "prj_00000000000001", "workspace": "wsp_00000000000001", "name": "-2+3"}
	}).CreateOne(ctx, db, f))(t)

	w := adminCall(t, h, "GET", "/api/admin/events/export?name=auth.login_attempted,project.created", "", "", 200, cookie)
	if w.Header().Get("Content-Type") != "text/csv; charset=utf-8" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), `attachment; filename="events-`) || w.Header().Get("X-Export-Truncated") != "" {
		t.Fatalf("headers: %v", w.Header())
	}
	rows := apitest.Must(csv.NewReader(strings.NewReader(w.Body.String())).ReadAll())(t)
	if len(rows) != 3 || strings.Join(rows[0], ",") != "time,event,name,failed,actor,actor_email,user,user_email,workspace,workspace_name,ip,payload" {
		t.Fatalf("rows: %q", rows)
	}
	if rows[1][2] != "project.created" || rows[2][2] != "auth.login_attempted" || rows[2][10] != "198.51.100.7" || rows[2][3] != "true" {
		t.Fatalf("not the events, newest first: %q", rows)
	}
	for _, row := range rows[1:] {
		for _, cell := range row {
			if trimmed := strings.TrimSpace(cell); trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
				t.Errorf("a cell a spreadsheet would run: %q", cell)
			}
		}
	}
	e := apitest.OnlyEvent(t, db, "admin.event.exported")
	filters, _ := e.Payload["filters"].(map[string]any)
	if !apitest.Is(e.ActorID, admin.User.ID) || e.UserID.Valid || e.Payload["rows"] != float64(2) || filters == nil || filters["window"] != "24h" {
		t.Fatalf("admin.event.exported: %s", e.Raw)
	}
	// The same filters as the list: an IP outside matches nothing.
	empty := adminCall(t, h, "GET", "/api/admin/events/export?ip=203.0.113.1", "", "", 200, cookie)
	if rows := apitest.Must(csv.NewReader(strings.NewReader(empty.Body.String())).ReadAll())(t); len(rows) != 1 {
		t.Fatalf("an IP filter: %q", rows)
	}
	for _, query := range []string{"?perPage=10", "?before=evt_00000000000001", "?summary=include", "?ip=nonsense", "?unknown=1"} {
		adminCall(t, h, "GET", "/api/admin/events/export"+query, "", "", 400, cookie)
	}
	// An app session or no key: refused like any admin read.
	adminSend(h, "GET", "/api/admin/events/export", "", "", "")
	if w := adminSend(h, "GET", "/api/admin/events/export", "", factory.AdminRouteKey, ""); w.Code != 401 {
		t.Fatalf("without a session: %d", w.Code)
	}

	// 10,000 at most: past it the answer says so.
	apitest.Must(factory.Events.State(func(e *factory.Event) { e.CreatedAt = at }).Create(ctx, db, f, 10_001))(t)
	big := adminCall(t, h, "GET", "/api/admin/events/export?name=rate_limit.reached&window=7d", "", "", 200, cookie)
	if n := strings.Count(big.Body.String(), "\r\n"); n != 10_001 || big.Header().Get("X-Export-Truncated") != "true" {
		t.Fatalf("%d lines, truncated %q", n, big.Header().Get("X-Export-Truncated"))
	}
	if exported := apitest.EventsNamed(t, db, "admin.event.exported"); len(exported) != 3 || exported[2].Payload["rows"] != float64(10_000) {
		t.Fatalf("exports recorded: %d", len(exported))
	}

	// Its query has the summaries' deadline: past it, 503 and nothing
	// recorded, and the slot is free again.
	s.Summaries.Deadline = time.Nanosecond
	slow := adminCall(t, h, "GET", "/api/admin/events/export?window=7d", "", "", 503, cookie)
	if !strings.Contains(slow.Body.String(), "Narrow the filters") {
		t.Fatalf("too slow: %s", slow.Body.String())
	}
	s.Summaries.Deadline = 5 * time.Second
	if n := len(apitest.EventsNamed(t, db, "admin.event.exported")); n != 3 {
		t.Fatalf("a refused export was recorded: %d", n)
	}
	adminCall(t, h, "GET", "/api/admin/events/export?name=auth.login_attempted", "", "", 200, cookie)
}

// An Events search finds an event over 30 days old by its minimised forms:
// the email's pseudonym, and the network an address falls in.
func TestAdminEventsFindMinimisedEvents(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	old := time.Now().AddDate(0, 0, -40)
	apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.Name, e.CreatedAt, e.IP, e.MinimisedAt = "auth.login_attempted", old, "198.51.100.0/24", time.Now()
		e.Payload = map[string]any{"email": s.EmailPseudonym("ada@example.test"), "result": "wrong_password"}
	}).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.Name, e.CreatedAt, e.IP = "auth.login_attempted", time.Now().Add(-time.Hour), "198.51.100.9"
		e.Payload = map[string]any{"email": "ada@example.test", "result": "wrong_password"}
	}).CreateOne(ctx, db, f))(t)
	list := func(query string) store.AdminEventPage {
		return decodeAdmin[store.AdminEventPage](t, adminCall(t, h, "GET", "/api/admin/events?window=all&"+query, "", "", 200, session.Cookie()).Body.Bytes())
	}
	if page := list("email=ADA@example.test"); len(page.Data) != 2 {
		t.Fatalf("by email: %d", len(page.Data))
	}
	byIP := list("ip=198.51.100.200")
	if len(byIP.Data) != 1 || byIP.Data[0].IP == nil || *byIP.Data[0].IP != "198.51.100.0/24" {
		t.Fatalf("by an address in the minimised network: %+v", byIP.Data)
	}
	if page := list("ip=198.51.100.9"); len(page.Data) != 2 {
		t.Fatalf("by the address: %d", len(page.Data))
	}
	if page := list("email=bo@example.test"); len(page.Data) != 0 {
		t.Fatalf("another address: %d", len(page.Data))
	}
}

// A delivery whose event has been minimised can't be retried: its email is
// a pseudonym. 409 for one; a bulk retry leaves it out.
func TestAdminRetryRefusesMinimisedEvents(t *testing.T) {
	s, db, _, _ := mailServer(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	e := apitest.Must(factory.Events.State(func(e *factory.Event) {
		e.Name, e.MinimisedAt = "auth.password_reset.requested", time.Now()
		e.Payload = map[string]any{"email": s.EmailPseudonym("ada@example.test")}
	}).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.DeliveryOf(e.ID, "mail.password_reset").State(func(d *factory.EventDelivery) {
		d.FailedAt, d.Attempts, d.LastError = time.Now(), 6, "smtp: timeout"
	}).CreateOne(ctx, db, f))(t)
	var publicID string
	apitest.Must(0, db.NewSelect().Table("events").Column("public_id").Where("id = ?", e.ID).Scan(ctx, &publicID))(t)
	w := adminCall(t, h, "POST", "/api/admin/deliveries/"+publicID+"/mail.password_reset/retry", reasonBody(reason), session.CSRFToken, 409, session.Cookie())
	if !strings.Contains(w.Body.String(), "over 30 days old") {
		t.Fatalf("one: %s", w.Body.String())
	}
	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	adminCall(t, h, "POST", "/api/admin/deliveries/retry", reasonBody(reason, "listener", "mail.password_reset", "failedSince", since), session.CSRFToken, 409, session.Cookie())
	if n := apitest.Must(db.NewSelect().Table("event_deliveries").Where("failed_at IS NULL").Count(ctx))(t); n != 0 {
		t.Fatal("a minimised event's delivery was queued again")
	}
}
