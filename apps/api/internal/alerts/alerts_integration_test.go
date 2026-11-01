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
	// Ok for 10 minutes: resolved, once.
	f.set("test.rate", 0)
	for m := 23; m < 33; m++ {
		if out := evaluate(t, e, start.Add(time.Duration(m)*time.Minute)); out.Resolved != 0 {
			t.Fatalf("resolved after %d minutes at ok", m-23)
		}
	}
	if out := evaluate(t, e, start.Add(33*time.Minute)); out.Resolved != 1 {
		t.Fatalf("not resolved: %+v", out)
	}
	resolved := apitest.OnlyEvent(t, s.DB, "alert.resolved")
	if resolved.Payload["openFor"] != float64(22*60) || resolved.Payload["mail"] != true || resolved.Payload["level"] != "critical" {
		t.Fatalf("alert.resolved: %+v", resolved.Payload)
	}
	if len(openAlerts(t, s)) != 0 || len(apitest.EventsNamed(t, s.DB, "alert.opened")) != 1 {
		t.Fatal("still open, or opened twice")
	}
}

// A signal past its budget changes nothing that minute; a rule's level
// narrows the signal's; hourly rules are read once an hour, and resolve at
// the next hourly check at ok.
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
	if out := evaluate(t, e, start.Add(time.Hour)); !out.Hourly || out.Resolved != 1 {
		t.Fatalf("the hour on: %+v", out)
	}
}

// How each opening is told: emailed and posted; the sixth within 10
// minutes held for a digest (posted, not emailed), digests at most every
// 10 minutes; an opening within the hour of its signal's last one quiet;
// a silenced signal quiet, its silence carried to its next alert.
func TestAlertNotices(t *testing.T) {
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
	// test.a back at ok, resolved, and up again within the hour: quiet.
	f.set("test.a", 0)
	evaluate(t, e, start.Add(11*time.Minute))
	evaluate(t, e, start.Add(21*time.Minute))
	f.set("test.a", 2)
	evaluate(t, e, start.Add(22*time.Minute))
	again := apitest.EventsNamed(t, s.DB, "alert.opened")
	last := again[len(again)-1]
	if last.Payload["signal"] != "test.a" || last.Payload["mail"] != false || last.Payload["webhook"] != false {
		t.Fatalf("the reopening: %+v", last.Payload)
	}
	// A silence of test.b carries over to its next alert, which is quiet,
	// and its resolution tells no one.
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
	f.set("test.b", 0)
	evaluate(t, e, start.Add(23*time.Minute))
	evaluate(t, e, start.Add(33*time.Minute))
	resolvedB := apitest.EventsNamed(t, s.DB, "alert.resolved")
	if lastB := resolvedB[len(resolvedB)-1]; lastB.Payload["signal"] != "test.b" || lastB.Payload["mail"] != false || lastB.Payload["webhook"] != false {
		t.Fatalf("a silenced resolution: %+v", lastB.Payload)
	}
	f.set("test.b", 2)
	evaluate(t, e, start.Add(2*time.Hour))
	opened := apitest.EventsNamed(t, s.DB, "alert.opened")
	if lastB := opened[len(opened)-1]; lastB.Payload["signal"] != "test.b" || lastB.Payload["mail"] != false {
		t.Fatalf("an opening while its signal is silenced: %+v", lastB.Payload)
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
	first := &Runner{DB: s.DB, Evaluator: e, Heartbeat: &Heartbeat{URL: hb.URL, Client: NewClient(HeartbeatTimeout, false)}, Now: func() time.Time { return start }}
	second := &Runner{DB: s.DB, Evaluator: other, Heartbeat: &Heartbeat{URL: hb.URL, Client: NewClient(HeartbeatTimeout, false)}, Now: func() time.Time { return start }}
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
	settings := map[string]string{}
	for _, setting := range []string{"all", "critical", "none"} {
		u := apitest.Must(factory.Users.CreateOne(ctx, s.DB, fk))(t)
		apitest.Must(factory.Admins.State(factory.AdminFor(u.ID)).State(factory.AlertEmail(setting)).CreateOne(ctx, s.DB, fk))(t)
		settings[u.Email] = setting
	}
	rc := &receiver{status: 204}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	m, box := mailer(t, s)
	bus := events.NewBus()
	Listen(bus, s, m, &Webhook{URL: srv.URL + secretPath, Format: FormatJSON, Client: NewClient(WebhookTimeout, false)})
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
		got[settings[msg.To]] = append(got[settings[msg.To]], msg.Subject)
	}
	if len(got["all"]) != 2 || len(got["critical"]) != 1 || got["critical"][0] != "Critical alert: test.critical" || len(got["none"]) != 0 {
		t.Fatalf("emails: %v", got)
	}
	if len(rc.bodies) != 2 {
		t.Fatalf("posts: %d", len(rc.bodies))
	}
	// The receiver fails: the delivery is retried, its error without the
	// URL's secret.
	rc.status = 503
	f.set("test.warning", 0)
	evaluate(t, e, start.Add(time.Minute))
	evaluate(t, e, start.Add(11*time.Minute))
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var failed struct {
		Attempts  int    `bun:"attempts"`
		LastError string `bun:"last_error"`
	}
	if err := s.DB.NewSelect().TableExpr("event_deliveries").Column("attempts", "last_error").
		Where("listener = 'alert.webhook_resolved'").Scan(ctx, &failed); err != nil {
		t.Fatal(err)
	}
	if failed.Attempts != 1 || !strings.Contains(failed.LastError, "answered") || strings.Contains(failed.LastError, "SECRET") {
		t.Fatalf("the failed post: %+v", failed)
	}
	if subject := box.Sent[len(box.Sent)-1].Subject; subject != "Resolved: test.warning" {
		t.Fatalf("the resolution: %s", subject)
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
	hooks := []*Webhook{
		{URL: srv.URL, Format: FormatJSON, Secret: []byte(strings.Repeat("k", 32)), Client: NewClient(WebhookTimeout, false)},
		{URL: srv.URL, Format: FormatSlack, Client: NewClient(WebhookTimeout, false)},
	}
	var digest events.AlertDigested
	for i, r := range monitor.AlertRules {
		at := start.Add(-time.Duration(i+1) * 2 * time.Hour)
		a, err := s.OpenAlert(ctx, store.NewAlert{Signal: r.Signal, Level: "critical", Value: 1234.5, At: at,
			Details: map[string]any{"samples": []string{admin.PublicID}, "drill": map[string]string{"email": admin.Email}}})
		if err != nil {
			t.Fatal(err)
		}
		digest.Alerts = append(digest.Alerts, events.DigestedAlert{Alert: a.PublicID, Signal: r.Signal, Level: "critical", Value: 1234.5})
		for _, kind := range []string{"alert.opened", "alert.escalated", "alert.reminded"} {
			c := change{kind, a.PublicID, r.Signal, "critical", 1234.5, true, true}
			if err := emailChange(ctx, s, m, c); err != nil {
				t.Fatal(err)
			}
			for _, h := range hooks {
				if err := postChange(ctx, s, h, c); err != nil {
					t.Fatal(err)
				}
			}
		}
		if _, err := s.ResolveAlert(ctx, a.ID, at.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		c := change{"alert.resolved", a.PublicID, r.Signal, "critical", 0, true, true}
		if err := emailChange(ctx, s, m, c); err != nil {
			t.Fatal(err)
		}
		for _, h := range hooks {
			if err := postChange(ctx, s, h, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := emailDigest(ctx, s, m, digest); err != nil {
		t.Fatal(err)
	}
	want := len(monitor.AlertRules)*4 + 1
	if len(box.Sent) != want || len(rc.bodies) != len(monitor.AlertRules)*4*2 {
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
