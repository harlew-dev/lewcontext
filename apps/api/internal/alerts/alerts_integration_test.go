package alerts

import (
	"context"
	"encoding/json"
	"io"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

// fakes are signals whose values a test sets: warn at 1, critical at 10.
type fakes struct {
	mu     sync.Mutex
	values map[string]float64
	slow   map[string]bool
}

func (f *fakes) set(name string, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.values[name] = v
}

func (f *fakes) find(name string) (monitor.Signal, bool) {
	if !strings.HasPrefix(name, "test.") {
		return monitor.FindSignal(name)
	}
	return monitor.Signal{Name: name, Page: "test", Label: "Test signal", Unit: monitor.UnitCount, Window: time.Hour, Warn: 1, Critical: 10,
		Measure: func(ctx context.Context, env monitor.Env, sp monitor.Span) (monitor.Reading, error) {
			f.mu.Lock()
			v, slow := f.values[name], f.slow[name]
			f.mu.Unlock()
			if slow {
				<-ctx.Done()
				return monitor.Reading{}, ctx.Err()
			}
			return monitor.Reading{Value: v, Sample: 1}, nil
		}}, true
}

// setup is a migrated database with a store, and an evaluator of rules on
// fake signals.
func setup(t *testing.T, rules ...monitor.AlertRule) (*store.Store, *Evaluator, *fakes) {
	t.Helper()
	db := apitest.Database(t)
	s := &store.Store{DB: db, Listeners: events.NewBus()}
	f := &fakes{values: map[string]float64{}, slow: map[string]bool{}}
	return s, &Evaluator{Store: s, Rules: rules, Signals: f.find, Budget: 200 * time.Millisecond}, f
}

func evaluate(t *testing.T, e *Evaluator, at time.Time) Evaluation {
	t.Helper()
	out, err := e.Evaluate(context.Background(), at)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func openAlerts(t *testing.T, s *store.Store) []store.Alert {
	t.Helper()
	return apitest.Must(s.IndexOpenAlerts(context.Background()))(t)
}

var start = time.Now().UTC().Truncate(time.Second)

// An alert opens only after its signal stayed at its level for For; it
// doesn't flap across the 10 minutes back at ok; a warning that becomes
// critical is raised in place; then it resolves.
func TestAlertLifecycle(t *testing.T) {
	// Resolving checks freshness on the database's clock, so the last turn is
	// a few seconds ago.
	start := time.Now().UTC().Truncate(time.Second).Add(-33*time.Minute - 20*time.Second)
	s, e, f := setup(t, monitor.AlertRule{Signal: "test.rate", For: 5 * time.Minute})
	f.set("test.rate", 2)
	for _, m := range []int{0, 1, 4} {
		if out := evaluate(t, e, start.Add(time.Duration(m)*time.Minute)); out.Opened != 0 {
			t.Fatalf("opened after %d minutes", m)
		}
	}
	// Back at ok for a minute: For starts again.
	f.set("test.rate", 0)
	evaluate(t, e, start.Add(5*time.Minute))
	f.set("test.rate", 2)
	evaluate(t, e, start.Add(6*time.Minute))
	if out := evaluate(t, e, start.Add(10*time.Minute)); out.Opened != 0 {
		t.Fatal("opened before For, counted again")
	}
	if out := evaluate(t, e, start.Add(11*time.Minute)); out.Opened != 1 {
		t.Fatalf("not opened after For: %+v", out)
	}
	opened := apitest.OnlyEvent(t, s.DB, "alert.opened")
	if opened.Payload["signal"] != "test.rate" || opened.Payload["level"] != "warn" || opened.Payload["mail"] != true || opened.Payload["webhook"] != true || opened.ActorID.Valid {
		t.Fatalf("alert.opened: %+v", opened)
	}
	// Critical: escalated, the same alert.
	f.set("test.rate", 12)
	if out := evaluate(t, e, start.Add(12*time.Minute)); out.Escalated != 1 || out.Opened != 0 {
		t.Fatalf("escalating: %+v", out)
	}
	open := openAlerts(t, s)
	if len(open) != 1 || open[0].Level != "critical" || open[0].PeakValue != 12 {
		t.Fatalf("open: %+v", open)
	}
	if e := apitest.OnlyEvent(t, s.DB, "alert.escalated"); e.Payload["from"] != "warn" || e.Payload["to"] != "critical" || e.Payload["mail"] != true {
		t.Fatalf("alert.escalated: %+v", e.Payload)
	}
	// Ok for 9 minutes, then warn again: still open, no new alert.
	f.set("test.rate", 0)
	for m := 13; m <= 21; m++ {
		evaluate(t, e, start.Add(time.Duration(m)*time.Minute))
	}
	f.set("test.rate", 3)
	evaluate(t, e, start.Add(22*time.Minute))
	if open := openAlerts(t, s); len(open) != 1 || open[0].Level != "critical" || open[0].LastValue != 3 || open[0].PeakValue != 12 {
		t.Fatalf("after 9 minutes at ok: %+v", open)
	}
	// Ok for 11 distinct minutes spanning 10: recovery is confirmed, once. The
	// alert stays open: only an operator resolves it.
	f.set("test.rate", 0)
	for m := 23; m < 33; m++ {
		if out := evaluate(t, e, start.Add(time.Duration(m)*time.Minute)); out.Recovered != 0 {
			t.Fatalf("recovered after %d minutes at ok", m-23)
		}
	}
	if out := evaluate(t, e, start.Add(33*time.Minute)); out.Recovered != 1 || out.Resolved != 0 {
		t.Fatalf("not recovered: %+v", out)
	}
	recovered := apitest.OnlyEvent(t, s.DB, "alert.recovered")
	if recovered.Payload["mail"] != true || recovered.Payload["level"] != "critical" {
		t.Fatalf("alert.recovered: %+v", recovered.Payload)
	}
	if len(apitest.EventsNamed(t, s.DB, "alert.resolved")) != 0 {
		t.Fatal("an alert resolved itself")
	}
	if open := openAlerts(t, s); len(open) != 1 || open[0].State != "recovered" {
		t.Fatalf("a recovered alert stays open: %+v", open)
	}
	// An operator resolves it with a comment of the current cycle.
	ctx := context.Background()
	admin := adminUser(t, s.DB)
	open = openAlerts(t, s)
	target := store.IncidentTarget{Kind: "alert", PublicID: open[0].PublicID}
	thread := apitest.Must(s.FindIncidentThread(ctx, target))(t)
	message := apitest.Must(s.CreateIncidentMessage(ctx, target, thread.CurrentCycle.PublicID, "Cause found and fixed.", admin))(t)
	if _, err := s.ResolveRecoveredAlert(ctx, open[0].PublicID, store.AlertResolution{Cycle: thread.CurrentCycle.PublicID, Message: message.PublicID, Revision: open[0].Revision}, admin); err != nil {
		t.Fatal(err)
	}
	resolved := apitest.OnlyEvent(t, s.DB, "alert.resolved")
	if resolved.Payload["source"] != "admin" || resolved.Payload["level"] != "critical" || resolved.ActorID.Valid {
		t.Fatalf("alert.resolved: %+v", resolved.Payload)
	}
	if len(openAlerts(t, s)) != 0 || len(apitest.EventsNamed(t, s.DB, "alert.opened")) != 1 {
		t.Fatal("still open, or opened twice")
	}
}

// A signal past its budget changes nothing that minute; a rule's level
// narrows the signal's; hourly rules are read once an hour, and recover at
// the first hourly check at ok (an operator resolves them).
func TestAlertRulesAndBounds(t *testing.T) {
	s, e, f := setup(t,
		monitor.AlertRule{Signal: "test.slow"},
		monitor.AlertRule{Signal: "test.cluster", From: monitor.LevelCritical, Cap: monitor.LevelWarn},
		monitor.AlertRule{Signal: "test.hourly", Hourly: true})
	f.set("test.slow", 50)
	f.slow["test.slow"] = true
	f.set("test.cluster", 5)
	f.set("test.hourly", 1)
	out := evaluate(t, e, start)
	if out.Unavailable != 1 || out.Opened != 1 || !out.Hourly {
		t.Fatalf("first: %+v", out)
	}
	if open := openAlerts(t, s); len(open) != 1 || open[0].Signal != "test.hourly" {
		t.Fatalf("open: %+v", open)
	}
	f.set("test.cluster", 20)
	f.set("test.hourly", 0)
	out = evaluate(t, e, start.Add(time.Minute))
	if out.Hourly || out.Opened != 1 {
		t.Fatalf("a minute on: %+v", out)
	}
	for _, a := range openAlerts(t, s) {
		if a.Signal == "test.cluster" && a.Level != "warn" {
			t.Fatalf("the cluster's level: %+v", a)
		}
	}
	if out := evaluate(t, e, start.Add(time.Hour)); !out.Hourly || out.Recovered != 1 {
		t.Fatalf("the hour on: %+v", out)
	}
}

// How each opening is told: emailed and posted; the sixth within 10
// minutes held for a digest (posted, not emailed), digests at most every
// 10 minutes; an opening within the hour of its signal's last one quiet;
// a silenced signal quiet, its silence carried to its next alert.
func TestAlertNotices(t *testing.T) {
	// The last turn is near now: resolving checks freshness on the database's clock.
	start := time.Now().UTC().Truncate(time.Second).Add(-21*time.Minute - 30*time.Second)
	var rules []monitor.AlertRule
	for _, name := range []string{"test.a", "test.b", "test.c", "test.d", "test.e", "test.f", "test.g"} {
		rules = append(rules, monitor.AlertRule{Signal: name})
	}
	s, e, f := setup(t, rules...)
	for _, r := range rules[:6] {
		f.set(r.Signal, 2)
	}
	if out := evaluate(t, e, start); out.Opened != 6 || out.Digested != 1 {
		t.Fatalf("six at once: %+v", out)
	}
	notified := map[string]int{}
	for _, a := range openAlerts(t, s) {
		notified[a.Notified]++
	}
	if notified[store.NotifiedMail] != 5 || notified[store.NotifiedDigest] != 1 {
		t.Fatalf("notified: %v", notified)
	}
	digest := apitest.OnlyEvent(t, s.DB, "alert.digested")
	if alerts := digest.Payload["alerts"].([]any); len(alerts) != 1 {
		t.Fatalf("digest: %+v", digest.Payload)
	}
	held := 0
	for _, ev := range apitest.EventsNamed(t, s.DB, "alert.opened") {
		if ev.Payload["mail"] == false {
			held++
			if ev.Payload["webhook"] != true {
				t.Fatalf("a held opening isn't posted: %+v", ev.Payload)
			}
		}
	}
	if held != 1 {
		t.Fatalf("held: %d", held)
	}
	// The seventh, 5 minutes on: held, and no digest until 10 minutes
	// after the last.
	f.set("test.g", 2)
	if out := evaluate(t, e, start.Add(5*time.Minute)); out.Opened != 1 || out.Digested != 0 {
		t.Fatalf("the seventh: %+v", out)
	}
	if out := evaluate(t, e, start.Add(10*time.Minute)); out.Digested != 1 {
		t.Fatalf("its digest: %+v", out)
	}
	// test.a and test.b (silenced meanwhile) come back at ok for 11 minutes:
	// recovery is confirmed (quietly for the silenced one), an operator
	// resolves each, and both rise again within the hour: the same alerts,
	// new cycles, told to no one (the cooldown, and the carried silence).
	ctx := context.Background()
	admin := adminUser(t, s.DB)
	var b store.Alert
	for _, a := range openAlerts(t, s) {
		if a.Signal == "test.b" {
			b = a
		}
	}
	if _, err := s.SilenceAlert(ctx, b.PublicID, 24*time.Hour, store.AdminAction{Admin: admin, Reason: "Planned maintenance tonight"}); err != nil {
		t.Fatal(err)
	}
	f.set("test.a", 0)
	f.set("test.b", 0)
	for m := 11; m <= 21; m++ {
		evaluate(t, e, start.Add(time.Duration(m)*time.Minute))
	}
	for _, ev := range apitest.EventsNamed(t, s.DB, "alert.recovered") {
		if ev.Payload["signal"] == "test.b" && (ev.Payload["mail"] != false || ev.Payload["webhook"] != false) {
			t.Fatalf("a silenced recovery: %+v", ev.Payload)
		}
	}
	for _, name := range []string{"test.a", "test.b"} {
		for _, a := range openAlerts(t, s) {
			if a.Signal != name {
				continue
			}
			if a.State != "recovered" {
				t.Fatalf("%s: %+v", name, a)
			}
			target := store.IncidentTarget{Kind: "alert", PublicID: a.PublicID}
			thread := apitest.Must(s.FindIncidentThread(ctx, target))(t)
			message := apitest.Must(s.CreateIncidentMessage(ctx, target, thread.CurrentCycle.PublicID, "Reviewed, cause fixed.", admin))(t)
			if _, err := s.ResolveRecoveredAlert(ctx, a.PublicID, store.AlertResolution{Cycle: thread.CurrentCycle.PublicID, Message: message.PublicID, Revision: a.Revision}, admin); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.set("test.a", 2)
	f.set("test.b", 2)
	evaluate(t, e, time.Now().UTC())
	if got := len(apitest.EventsNamed(t, s.DB, "alert.opened")); got != 7 {
		t.Fatalf("a recurrence opened a new parent: %d openings", got)
	}
	recurrences := map[string]map[string]any{}
	for _, ev := range apitest.EventsNamed(t, s.DB, "alert.reopened") {
		recurrences[ev.Payload["signal"].(string)] = ev.Payload
	}
	for _, name := range []string{"test.a", "test.b"} {
		p := recurrences[name]
		if p == nil || p["cause"] != "recurrence" || p["mail"] != false || p["webhook"] != false {
			t.Fatalf("%s: a recurrence within the hour or while silenced: %+v", name, p)
		}
	}
}

// adminUser is a new operator.
func adminUser(t *testing.T, db bun.IDB) store.User {
	t.Helper()
	ctx := context.Background()
	f := factory.TestFaker(t)
	u := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Admins.State(factory.AdminFor(u.ID)).CreateOne(ctx, db, f))(t)
	return store.User{ID: u.ID, PublicID: u.PublicID, Name: u.Name, Email: u.Email}
}

// A critical alert open and unacknowledged is emailed again every 4 hours;
// not when acknowledged or silenced, nor a warning.
func TestAlertReminders(t *testing.T) {
	s, e, f := setup(t, monitor.AlertRule{Signal: "test.crit"}, monitor.AlertRule{Signal: "test.warn"}, monitor.AlertRule{Signal: "test.acked"})
	for name, v := range map[string]float64{"test.crit": 20, "test.warn": 2, "test.acked": 20} {
		f.set(name, v)
	}
	evaluate(t, e, start)
	ctx := context.Background()
	admin := adminUser(t, s.DB)
	for _, a := range openAlerts(t, s) {
		if a.Signal == "test.acked" {
			apitest.Must(s.AcknowledgeAlert(ctx, a.PublicID, admin))(t)
		}
	}
	if out := evaluate(t, e, start.Add(4*time.Hour-time.Minute)); out.Reminded != 0 {
		t.Fatalf("reminded early: %+v", out)
	}
	if out := evaluate(t, e, start.Add(4*time.Hour)); out.Reminded != 1 {
		t.Fatalf("not reminded: %+v", out)
	}
	if r := apitest.OnlyEvent(t, s.DB, "alert.reminded"); r.Payload["signal"] != "test.crit" {
		t.Fatalf("reminded: %+v", r.Payload)
	}
	if out := evaluate(t, e, start.Add(5*time.Hour)); out.Reminded != 0 {
		t.Fatal("reminded twice in 4 hours")
	}
	if out := evaluate(t, e, start.Add(8*time.Hour)); out.Reminded != 1 {
		t.Fatal("not reminded again")
	}
}

// Only the server holding the lock evaluates; another takes over when the
// first's connection ends (a crash) or it hands the lock back (shutdown),
// and the first stands by after; the heartbeat goes only after an
// evaluation that worked.
func TestAlertLeader(t *testing.T) {
	s, e, f := setup(t, monitor.AlertRule{Signal: "test.leader"})
	f.set("test.leader", 2)
	pings := 0
	hb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { pings++ }))
	defer hb.Close()
	ctx := context.Background()
	other := &Evaluator{Store: s, Rules: e.Rules, Signals: f.find}
	addHeartbeat(t, s, "Dead man's switch", hb.URL)
	beat := func() *Heartbeat {
		return &Heartbeat{Store: s, Sealer: testSealer(t), Client: NewClient(HeartbeatTimeout, false)}
	}
	first := &Runner{DB: s.DB, Evaluator: e, Heartbeat: beat(), Now: func() time.Time { return start }}
	second := &Runner{DB: s.DB, Evaluator: other, Heartbeat: beat(), Now: func() time.Time { return start }}
	defer first.Release()
	defer second.Release()
	if ran, err := first.Tick(ctx); !ran || err != nil {
		t.Fatalf("first: %v %v", ran, err)
	}
	if ran, _ := second.Tick(ctx); ran {
		t.Fatal("both evaluated")
	}
	if pings != 1 || len(openAlerts(t, s)) != 1 {
		t.Fatalf("pings %d", pings)
	}
	// The first's session ends, as when its server dies.
	var killed bool
	err := s.DB.NewSelect().TableExpr("pg_locks").ColumnExpr("bool_or(pg_terminate_backend(pid))").
		Where("locktype = 'advisory' AND granted AND objid = (hashtextextended(? || current_schema(), 0) & 4294967295)::bigint::oid", "lewcontext:alerts:").
		Scan(ctx, &killed)
	if err != nil || !killed {
		t.Fatalf("ending the first's session: %v %v", killed, err)
	}
	// PostgreSQL frees a terminated session's locks a moment later.
	took := false
	for i := 0; i < 50 && !took; i++ {
		took, _ = second.Tick(ctx)
		if !took {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if !took {
		t.Fatal("the second didn't take over")
	}
	if ran, _ := first.Tick(ctx); ran || first.Leading() {
		t.Fatal("the first still evaluates")
	}
	if pings != 2 || len(apitest.EventsNamed(t, s.DB, "alert.opened")) != 1 {
		t.Fatalf("after the handover: %d pings", pings)
	}
	// The second hands it back at shutdown; the first takes it.
	second.Release()
	if ran, _ := first.Tick(ctx); !ran {
		t.Fatal("the first didn't take it back")
	}
	// An evaluation that fails sends no heartbeat.
	first.Evaluator.Rules = []monitor.AlertRule{{Signal: "nothing.like_this"}}
	if ran, err := first.Tick(ctx); !ran || err == nil || pings != 3 {
		t.Fatalf("a failed evaluation: %v %v, %d pings", ran, err, pings)
	}
}

// mailer sends into a box, through the store's block list.
func mailer(t *testing.T, s *store.Store) (*mail.Mailer, *apitest.Mailbox) {
	box := &apitest.Mailbox{}
	return apitest.Must(mail.New(mail.Options{Driver: box, App: "lewcontext", Origin: apitest.Origin, Blocks: s}))(t), box
}

// One email per admin at their level, none for none; the webhook posts
// every alert event; a failure is retried through the delivery queue,
// its error without the URL.
func TestAlertListeners(t *testing.T) {
	s, e, f := setup(t, monitor.AlertRule{Signal: "test.warning"}, monitor.AlertRule{Signal: "test.critical"})
	ctx := context.Background()
	fk := factory.TestFaker(t)
	// Alert email is opt-in: one admin takes warnings and critical on both
	// signals, one only critical on both, one subscribed to nothing.
	levels := map[string]string{}
	for _, level := range []string{"warn", "critical", ""} {
		u := apitest.Must(factory.Users.CreateOne(ctx, s.DB, fk))(t)
		apitest.Must(factory.Admins.State(factory.AdminFor(u.ID)).CreateOne(ctx, s.DB, fk))(t)
		levels[u.Email] = level
		if level == "" {
			continue
		}
		for _, signal := range []string{"test.warning", "test.critical"} {
			apitest.Must(factory.AdminAlertSubscriptions.State(factory.SubscribedTo(u.ID, signal, level)).CreateOne(ctx, s.DB, fk))(t)
		}
	}
	rc, quiet := &receiver{status: 204}, &receiver{status: 204}
	srv, srv2 := httptest.NewServer(rc), httptest.NewServer(quiet)
	defer srv.Close()
	defer srv2.Close()
	kinds := []string{"opened", "recovered"}
	// One webhook chose both signals; another chose a signal that never alerts.
	addWebhook(t, s, "Pager", srv.URL+secretPath, FormatJSON, []string{"test.warning", "test.critical"}, kinds)
	addWebhook(t, s, "Elsewhere", srv2.URL, FormatJSON, []string{"test.other"}, kinds)
	addWebhook(t, s, "Nothing chosen", srv2.URL, FormatJSON, nil, nil)
	m, box := mailer(t, s)
	bus := events.NewBus()
	Listen(bus, s, m, &Webhook{Store: s, Sealer: testSealer(t), Client: NewClient(WebhookTimeout, false)})
	s.Listeners = bus
	worker := &events.Worker{Bus: bus, Queue: s}
	f.set("test.warning", 2)
	f.set("test.critical", 20)
	evaluate(t, e, start)
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for _, msg := range box.Sent {
		got[levels[msg.To]] = append(got[levels[msg.To]], msg.Subject)
	}
	if len(got["warn"]) != 2 || len(got["critical"]) != 1 || got["critical"][0] != "Critical alert: An alert" || len(got[""]) != 0 {
		t.Fatalf("emails: %v", got)
	}
	if len(rc.bodies) != 2 || len(quiet.bodies) != 0 {
		t.Fatalf("posts: %d, and %d to webhooks that chose nothing here", len(rc.bodies), len(quiet.bodies))
	}
	// Only the event kinds a webhook chose are posted: escalated wasn't.
	var deliveries int
	if err := s.DB.NewSelect().TableExpr("event_deliveries").ColumnExpr("count(*)").Where("target <> ''").Scan(ctx, &deliveries); err != nil || deliveries != 2 {
		t.Fatalf("webhook deliveries: %d %v", deliveries, err)
	}
	// The receiver fails: the delivery is retried, its error without the
	// URL's secret.
	rc.status = 503
	f.set("test.warning", 0)
	for m := 1; m <= 11; m++ {
		evaluate(t, e, start.Add(time.Duration(m)*time.Minute))
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var failed struct {
		Attempts  int    `bun:"attempts"`
		LastError string `bun:"last_error"`
		Target    string `bun:"target"`
	}
	if err := s.DB.NewSelect().TableExpr("event_deliveries").Column("attempts", "last_error", "target").
		Where("listener = 'alert.webhook_recovered'").Scan(ctx, &failed); err != nil {
		t.Fatal(err)
	}
	if failed.Attempts != 1 || failed.Target == "" || !strings.Contains(failed.LastError, "answered") || strings.Contains(failed.LastError, "SECRET") {
		t.Fatalf("the failed post: %+v", failed)
	}
	if subject := box.Sent[len(box.Sent)-1].Subject; subject != "Recovery confirmed: An alert" {
		t.Fatalf("the recovery notice: %s", subject)
	}
}

// Nobody is emailed and nothing is posted until someone subscribes: with
// no subscription and no webhook, every kind of event goes nowhere.
func TestAlertsGoNowhereUntilSomeoneSubscribes(t *testing.T) {
	s, e, f := setup(t, monitor.AlertRule{Signal: "test.quiet"})
	ctx := context.Background()
	admin := adminUser(t, s.DB)
	m, box := mailer(t, s)
	bus := events.NewBus()
	Listen(bus, s, m, &Webhook{Store: s, Sealer: testSealer(t), Client: NewClient(WebhookTimeout, false)})
	s.Listeners = bus
	worker := &events.Worker{Bus: bus, Queue: s}
	f.set("test.quiet", 20)
	evaluate(t, e, start)
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(box.Sent) != 0 {
		t.Fatalf("emailed %d without a subscription", len(box.Sent))
	}
	// Subscribing to a different signal changes nothing either.
	apitest.Must(factory.AdminAlertSubscriptions.State(factory.SubscribedTo(admin.ID, "test.other", "warn")).CreateOne(ctx, s.DB, factory.TestFaker(t)))(t)
	f.set("test.quiet", 0)
	for m := 1; m <= 12; m++ {
		evaluate(t, e, start.Add(time.Duration(m)*time.Minute))
	}
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if len(box.Sent) != 0 {
		t.Fatalf("emailed %d for a signal nobody subscribed to", len(box.Sent))
	}
}

// A webhook is posted to at most 12 times an hour per signal.
func TestWebhookPostsAreCappedPerSignal(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	w := addWebhook(t, s, "Busy", "https://hooks.example.net/x", FormatJSON, []string{"test.cap"}, []string{"reminded"})
	bus := events.NewBus()
	events.Listen(bus, "alert.webhook_reminded", func(context.Context, events.AlertReminded, events.Delivered) error { return nil })
	s.Listeners = bus
	for i := 0; i < 15; i++ {
		e := events.AlertReminded{Alert: "alt_0123456789abcd", Signal: "test.cap", Level: "critical", Webhook: true}
		if err := s.Record(ctx, e, store.EventScope{}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := s.DB.NewSelect().TableExpr("event_deliveries").ColumnExpr("count(*)").Where("target = ?", w.ID).Scan(ctx, &n); err != nil || n != 12 {
		t.Fatalf("%d deliveries (%v), want 12", n, err)
	}
}

// Nothing an alert sends out (every template, each kind, the digest, the
// webhook in both formats, for every rule's signal) holds the admin key,
// a link, an email address, an IP address or an admin's name: the
// details stay in the dashboard (decision 6).
func TestAlertContentHoldsNoSecretsOrPersonalData(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	admin := adminUser(t, s.DB)
	m, box := mailer(t, s)
	rc := &receiver{status: 204}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	signals := []string{}
	for _, r := range monitor.AlertRules {
		signals = append(signals, r.Signal)
		apitest.Must(factory.AdminAlertSubscriptions.State(factory.SubscribedTo(admin.ID, r.Signal, "warn")).CreateOne(ctx, s.DB, factory.TestFaker(t)))(t)
	}
	hooks := []store.AlertWebhook{
		addWebhook(t, s, "JSON", srv.URL, FormatJSON, signals, events.AlertWebhookKinds),
		addWebhook(t, s, "Slack", srv.URL, FormatSlack, signals, events.AlertWebhookKinds),
	}
	sender := Webhook{Store: s, Sealer: testSealer(t), Client: NewClient(WebhookTimeout, false)}
	var digest events.AlertDigested
	for i, r := range monitor.AlertRules {
		at := start.Add(-time.Duration(i+1) * 2 * time.Hour)
		a, err := s.OpenAlert(ctx, store.NewAlert{Signal: r.Signal, Level: "critical", Value: 1234.5, At: at,
			Details: map[string]any{"samples": []string{admin.PublicID}, "drill": map[string]string{"email": admin.Email}}})
		if err != nil {
			t.Fatal(err)
		}
		digest.Alerts = append(digest.Alerts, events.DigestedAlert{Alert: a.PublicID, Signal: r.Signal, Level: "critical", Value: 1234.5})
		for _, kind := range []string{"alert.opened", "alert.escalated", "alert.reminded", "alert.recovered", "alert.reopened", "alert.resolved"} {
			c := change{kind, a.PublicID, r.Signal, "critical", 1234.5, true, true, nil}
			if err := emailChange(ctx, s, m, c); err != nil {
				t.Fatal(err)
			}
			for _, h := range hooks {
				if err := postChange(ctx, s, &sender, queuedFor(h), c); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := emailDigest(ctx, s, m, digest); err != nil {
		t.Fatal(err)
	}
	want := len(monitor.AlertRules)*6 + 1
	if len(box.Sent) != want || len(rc.bodies) != len(monitor.AlertRules)*6*2 {
		t.Fatalf("sent %d emails (want %d) and %d posts", len(box.Sent), want, len(rc.bodies))
	}
	email := regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)
	ipv4 := regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	ipv6 := regexp.MustCompile(`\b[0-9a-fA-F]{1,4}(:[0-9a-fA-F]{0,4}){2,7}\b`)
	check := func(what, text string) {
		t.Helper()
		for _, bad := range []string{factory.AdminRouteKey, apitest.Origin, "http://", "https://", "/admin", admin.PublicID, admin.Name, admin.Email, "usr_"} {
			if bad != "" && strings.Contains(text, bad) {
				t.Errorf("%s holds %q:\n%s", what, bad, text)
			}
		}
		for _, re := range []*regexp.Regexp{email, ipv4, ipv6} {
			if found := re.FindString(text); found != "" {
				t.Errorf("%s holds %q:\n%s", what, found, text)
			}
		}
	}
	for _, msg := range box.Sent {
		check(msg.Subject+" (subject)", msg.Subject)
		// The HTML's only link-like thing is the embedded logo, cid:logo.
		check(msg.Subject+" (HTML)", strings.ReplaceAll(msg.HTML, `src="cid:logo"`, ""))
		check(msg.Subject+" (text)", msg.Text)
		if msg.Unsubscribe != "" || strings.Contains(msg.HTML, "href=") {
			t.Errorf("%s has a link", msg.Subject)
		}
	}
	for _, body := range rc.bodies {
		check("webhook body", string(body))
		var fields map[string]any
		if err := json.Unmarshal(body, &fields); err != nil {
			t.Fatal(err)
		}
	}
	_ = io.Discard
	_ = database.LockAlertWrites
}

// A signal driven warn, critical, ok, warn, critical, ok again and again
// within the hour can't email or post past its caps, whatever the kind of
// transition: it is one alert and one cycle throughout (a relapse after a
// confirmed recovery reopens it, never a new alert), so the signal's
// emails of every kind stay at 3 an hour, and its webhook posts at 12.
func TestAlertFlappingStaysWithinItsCaps(t *testing.T) {
	s, e, f := setup(t, monitor.AlertRule{Signal: "test.flap"})
	kinds := []string{"alert.opened", "alert.escalated", "alert.recovered", "alert.reopened", "alert.reminded", "alert.resolved"}
	count := func(field string) (n int) {
		for _, name := range kinds {
			for _, ev := range apitest.EventsNamed(t, s.DB, name) {
				if v, ok := ev.Payload[field]; !ok || v == true {
					n++
				}
			}
		}
		return n
	}
	minute := 0
	step := func(v float64, minutes int) {
		f.set("test.flap", v)
		for range minutes {
			evaluate(t, e, start.Add(time.Duration(minute)*time.Minute))
			minute++
		}
	}
	for range 4 {
		step(2, 1)  // bad: opens, or reopens after a confirmed recovery
		step(20, 1) // critical
		step(0, 11) // ok for 10 minutes: recovery is confirmed
	}
	if got := count("mail"); got != store.MaxSignalMails {
		t.Fatalf("%d emails for one signal in %d minutes, want the cap, %d", got, minute, store.MaxSignalMails)
	}
	if got := count("webhook"); got > 12 {
		t.Fatalf("%d webhook posts for one signal in %d minutes, cap 12", got, minute)
	}
	if got := len(apitest.EventsNamed(t, s.DB, "alert.opened")); got != 1 {
		t.Fatalf("a relapse opened another alert: %d openings", got)
	}
	if got := len(apitest.EventsNamed(t, s.DB, "alert.reopened")); got != 3 {
		t.Fatalf("relapses: %d, want 3", got)
	}
	if got := len(apitest.EventsNamed(t, s.DB, "alert.recovered")); got != 4 {
		t.Fatalf("recoveries: %d, want one per confirmed run", got)
	}
}

// A leader whose lock check times out lets the lock go (closing its
// connection) and leads again from a fresh one, so a hung connection can't
// hold the lock while evaluating nothing.
func TestAlertLeaderLetsGoWhenItsCheckTimesOut(t *testing.T) {
	s, e, _ := setup(t, monitor.AlertRule{Signal: "test.none"})
	ctx := context.Background()
	r := &Runner{DB: s.DB, Evaluator: e, Now: func() time.Time { return start }}
	defer r.Release()
	if ran, err := r.Tick(ctx); !ran || err != nil {
		t.Fatalf("first: %v %v", ran, err)
	}
	before := r.leader
	r.CheckTimeout = time.Nanosecond
	if ran, _ := r.Tick(ctx); !ran || r.leader == nil || r.leader == before {
		t.Fatalf("the timed-out leader wasn't replaced: %v %v", ran, r.leader == before)
	}
	// Only one session holds the lock.
	var holders int
	err := s.DB.NewSelect().TableExpr("pg_locks").ColumnExpr("count(*)").
		Where("locktype = 'advisory' AND granted AND objid = (hashtextextended(? || current_schema(), 0) & 4294967295)::bigint::oid", "lewcontext:alerts:").
		Scan(ctx, &holders)
	if err != nil || holders != 1 {
		t.Fatalf("%d holders (%v)", holders, err)
	}
}

// An email is only about the kinds of event the admin chose for the signal:
// opened and recovered here, so not an escalation, a reminder or a
// resolution; the digest follows "opened". One who chose none gets nothing.
func TestEmailFollowsTheEventsChosen(t *testing.T) {
	s, _, _ := setup(t)
	ctx := context.Background()
	fk := factory.TestFaker(t)
	subscribe := func(kinds ...string) string {
		u := apitest.Must(factory.Users.CreateOne(ctx, s.DB, fk))(t)
		apitest.Must(factory.Admins.State(factory.AdminFor(u.ID)).CreateOne(ctx, s.DB, fk))(t)
		apitest.Must(factory.AdminAlertSubscriptions.State(factory.SubscribedTo(u.ID, "test.kinds", "warn")).State(factory.SubscribedToEvents(kinds...)).CreateOne(ctx, s.DB, fk))(t)
		return u.Email
	}
	some := subscribe("opened", "recovered")
	none := subscribe()
	all := subscribe("opened", "escalated", "recovered", "reopened", "resolved", "reminded")
	a := apitest.Must(s.OpenAlert(ctx, store.NewAlert{Signal: "test.kinds", Level: "critical", Value: 5, At: start}))(t)
	m, box := mailer(t, s)
	for _, kind := range []string{"alert.opened", "alert.escalated", "alert.recovered", "alert.reopened", "alert.resolved", "alert.reminded"} {
		if err := emailChange(ctx, s, m, change{kind, a.PublicID, "test.kinds", "critical", 5, true, true, nil}); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]int{}
	for _, msg := range box.Sent {
		got[msg.To]++
	}
	if got[some] != 2 || got[none] != 0 || got[all] != 6 {
		t.Fatalf("emails per admin: some %d, none %d, all %d", got[some], got[none], got[all])
	}
	before := len(box.Sent)
	digest := events.AlertDigested{Alerts: []events.DigestedAlert{{Alert: a.PublicID, Signal: "test.kinds", Level: "critical", Value: 5}}}
	if err := emailDigest(ctx, s, m, digest); err != nil {
		t.Fatal(err)
	}
	if len(box.Sent)-before != 2 {
		t.Fatalf("a digest of openings went to %d admins, want the 2 who chose opened", len(box.Sent)-before)
	}
}
