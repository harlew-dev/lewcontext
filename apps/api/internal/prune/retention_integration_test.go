package prune

import (
	"context"
	"crypto/rand"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/database/dbtest"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"time"
)

// migratedStore is a store on a migrated schema of the test's own.
func migratedStore(t *testing.T) *store.Store {
	t.Helper()
	db := dbtest.Open(t)
	if _, err := database.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return &store.Store{DB: db}
}

// event adds an event named name, age old, with payload and ip.
func event(t *testing.T, s *store.Store, name string, age time.Duration, payload map[string]any, ip string) factory.Event {
	t.Helper()
	e, err := factory.Events.State(func(e *factory.Event) {
		e.Name, e.CreatedAt, e.IP = name, time.Now().Add(-age), ip
		if payload != nil {
			e.Payload = payload
		}
	}).CreateOne(context.Background(), s.DB, factory.TestFaker(t))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// names are the names of the events left, in id order.
func names(t *testing.T, s *store.Store) []string {
	t.Helper()
	var out []string
	if err := s.DB.NewSelect().Table("events").Column("name").Where("name <> 'prune.completed'").OrderExpr("id").Scan(context.Background(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Audit events are kept a year (audit_events), the rest 90 days (events):
// neither target touches the other's rows, and both record prune.completed.
func TestRetentionClasses(t *testing.T) {
	s := migratedStore(t)
	ctx := context.Background()
	for _, age := range []time.Duration{100 * day, 400 * day} {
		for _, name := range []string{"auth.logged_in", "admin.user.viewed", "server.started", "workspace.created", "rate_limit.reached", "email.blocked"} {
			event(t, s, name, age, nil, "")
		}
	}
	events, _ := Find("events")
	audit, _ := Find("audit_events")
	r, err := Run(ctx, s, events, Options{Confirm: true})
	if err != nil || r.Deleted != 6 {
		t.Fatalf("events: %+v %v", r, err)
	}
	if left := strings.Join(names(t, s), " "); left != "auth.logged_in admin.user.viewed server.started auth.logged_in admin.user.viewed server.started" {
		t.Fatalf("after events: %s", left)
	}
	r, err = Run(ctx, s, audit, Options{Confirm: true})
	if err != nil || r.Deleted != 3 {
		t.Fatalf("audit_events: %+v %v", r, err)
	}
	if left := strings.Join(names(t, s), " "); left != "auth.logged_in admin.user.viewed server.started" {
		t.Fatalf("after audit_events: %s", left)
	}
	// Each run's evidence is an audit event itself, so it is kept a year.
	completed := completedRuns(t, s)
	if len(completed) != 2 || !strings.Contains(completed[0], `"target": "events"`) || !strings.Contains(completed[1], `"target": "audit_events"`) {
		t.Fatalf("prune.completed: %v", completed)
	}
}

// testKeys are keys from a random APP_KEY.
func testKeys(t *testing.T) *auth.Keys {
	t.Helper()
	key := make([]byte, auth.AppKeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	keys, err := auth.NewKeys(key)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

// minimisedRow is an event as the pseudonymise test reads it back.
type minimisedRow struct {
	Name      string
	IP        string
	Payload   string
	Minimised bool
}

func rowsByName(t *testing.T, s *store.Store) map[string]minimisedRow {
	t.Helper()
	var rows []minimisedRow
	err := s.DB.NewSelect().Table("events").
		ColumnExpr("name, coalesce(text(ip), '') AS ip, payload::text AS payload, minimised_at IS NOT NULL AS minimised").
		Where("name <> 'prune.completed'").
		Scan(context.Background(), &rows)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]minimisedRow{}
	for _, row := range rows {
		out[row.Name] = row
	}
	return out
}

// Past 30 days, IPs are cut to their /24 or /48 and emails replaced by the
// keyed pseudonym (the payload's, an Events search's, a list search holding
// an "@"); a second run changes nothing; recent events, and events whose
// listener hasn't run, are left alone; nothing typed survives.
func TestPseudonymise(t *testing.T) {
	s := migratedStore(t)
	ctx := context.Background()
	keys := testKeys(t)
	old := 40 * day
	event(t, s, "auth.login_attempted", old, map[string]any{"email": "Ada@Example.test", "result": "wrong_password"}, "198.51.100.23")
	event(t, s, "workspace.invitation.created", old, map[string]any{"email": "bo@example.test", "invitation": "inv_00000000000001", "workspace": "wsp_00000000000001", "role": "member"}, "2001:db8:1234:5678::9")
	event(t, s, "admin.event.listed", old, map[string]any{"admin": "usr_00000000000001", "filters": map[string]any{"email": "cy@example.test", "ip": "203.0.113.77", "window": "24h"}}, "198.51.100.200")
	event(t, s, "admin.user.listed", old, map[string]any{"admin": "usr_00000000000001", "q": "dee@example.test", "sort": "name", "page": 1}, "")
	event(t, s, "admin.workspace.listed", old, map[string]any{"admin": "usr_00000000000001", "q": "acme", "sort": "name", "page": 12345678901}, "")
	event(t, s, "auth.registered", 10*day, map[string]any{"email": "eve@example.test", "name": "Eve"}, "198.51.100.5")
	pending := event(t, s, "auth.password_reset.requested", old, map[string]any{"email": "fay@example.test"}, "198.51.100.6")
	if _, err := factory.DeliveryOf(pending.ID, "mail.password_reset").CreateOne(ctx, s.DB, factory.TestFaker(t)); err != nil {
		t.Fatal(err)
	}

	target, _ := Find("pseudonymise")
	if _, err := Run(ctx, s, target, Options{Confirm: true}); err == nil {
		t.Fatal("minimised without the pseudonyms' key")
	}
	preview, err := Run(ctx, s, target, Options{Pseudonym: keys.EmailPseudonym})
	if err != nil || preview.Deleted != 5 || preview.Kept != 1 || !strings.HasPrefix(preview.String(), "pseudonymise: would minimise 5 ") {
		t.Fatalf("preview: %+v %v", preview, err)
	}
	r, err := Run(ctx, s, target, Options{Confirm: true, Batch: 2, Pseudonym: keys.EmailPseudonym})
	if err != nil || r.Deleted != 5 || !strings.HasPrefix(r.String(), "pseudonymise: minimised 5 ") {
		t.Fatalf("run: %+v %v", r, err)
	}
	rows := rowsByName(t, s)
	p := keys.EmailPseudonym
	checks := map[string]func(minimisedRow) bool{
		"auth.login_attempted": func(r minimisedRow) bool {
			return r.IP == "198.51.100.0/24" && strings.Contains(r.Payload, `"email": "`+p("ada@example.test")+`"`)
		},
		"workspace.invitation.created": func(r minimisedRow) bool {
			return r.IP == "2001:db8:1234::/48" && strings.Contains(r.Payload, `"email": "`+p("bo@example.test")+`"`) && strings.Contains(r.Payload, `"role": "member"`)
		},
		"admin.event.listed": func(r minimisedRow) bool {
			return r.IP == "198.51.100.0/24" && strings.Contains(r.Payload, `"email": "`+p("cy@example.test")+`"`) && strings.Contains(r.Payload, `"ip": "203.0.113.0/24"`) && strings.Contains(r.Payload, `"window": "24h"`)
		},
		"admin.user.listed": func(r minimisedRow) bool { return strings.Contains(r.Payload, `"q": "`+p("dee@example.test")+`"`) },
		"admin.workspace.listed": func(r minimisedRow) bool {
			return strings.Contains(r.Payload, `"q": "acme"`) && strings.Contains(r.Payload, `"page": 12345678901`)
		},
	}
	for name, ok := range checks {
		row := rows[name]
		if !row.Minimised || !ok(row) || strings.Contains(row.Payload, "@") || strings.Contains(row.Payload, "198.51.100.23") {
			t.Errorf("%s: %+v", name, row)
		}
	}
	// Recent, and waiting for its listener: as they were.
	if row := rows["auth.registered"]; row.Minimised || row.IP != "198.51.100.5/32" || !strings.Contains(row.Payload, "eve@example.test") {
		t.Errorf("a recent event: %+v", row)
	}
	if row := rows["auth.password_reset.requested"]; row.Minimised || !strings.Contains(row.Payload, "fay@example.test") {
		t.Errorf("an event a listener still needs: %+v", row)
	}
	// Idempotent: nothing left to minimise, nothing changed.
	again, err := Run(ctx, s, target, Options{Confirm: true, Pseudonym: keys.EmailPseudonym})
	if err != nil || again.Deleted != 0 {
		t.Fatalf("second run: %+v %v", again, err)
	}
	for name, row := range rowsByName(t, s) {
		if row != rows[name] {
			t.Errorf("%s changed on the second run: %+v → %+v", name, rows[name], row)
		}
	}
	// Another APP_KEY can't recognise an address: pseudonyms depend on it.
	if strings.Contains(rows["auth.login_attempted"].Payload, testKeys(t).EmailPseudonym("ada@example.test")) {
		t.Error("the pseudonym doesn't depend on APP_KEY")
	}
}
