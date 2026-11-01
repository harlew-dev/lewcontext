package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"slices"
	"strings"
	"testing"
	"time"
)

// Part 3's pages: Integrity, Performance, Errors (and one group) and
// Configuration (docs/api/monitoring.md).

// integritySections is the Integrity page's sections.
type integritySections struct {
	Checks            []monitor.CheckRows `json:"checks"`
	Duplicates        store.Duplicates    `json:"duplicates"`
	DuplicateSends    store.CheckResult   `json:"duplicateSends"`
	RetriedDeliveries store.CheckResult   `json:"retriedDeliveries"`
	TimedOutWrites    []store.RouteCount  `json:"timedOutWrites"`
}

// samples is a check's rows as the page names them.
func (s integritySections) samples(t *testing.T, name string) []string {
	t.Helper()
	for _, c := range s.Checks {
		if c.Name == name {
			return c.Samples
		}
	}
	t.Fatalf("no check %s", name)
	return nil
}

// Every check passes on a clean database and catches one planted
// violation, naming it; rows just inside each check's grace aren't
// counted. The summaries count the failing checks by level.
func TestAdminMonitoringIntegrity(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	integrity := func() monitoringView { return showMonitoring(t, h, session.Cookie(), "integrity", "") }

	v := integrity()
	for _, sig := range v.Signals {
		if sig.Value != 0 || sig.Level != monitor.LevelOK || sig.Unavailable {
			t.Fatalf("a clean database: %+v", sig)
		}
	}
	if len(v.Signals) != 12 {
		t.Fatalf("%d signals", len(v.Signals))
	}

	user := func() factory.User { return apitest.Must(factory.Users.CreateOne(ctx, db, f))(t) }
	touched := func(w factory.Workspace) {
		m.make("project.updated", map[string]any{"workspace": w.PublicID}, "", time.Hour, func(e *factory.Event) { e.WorkspaceID = w.ID })
	}
	// A workspace whose only member isn't its owner.
	ownerless := apitest.Must(factory.Workspaces.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.WorkspaceUsers.State(func(mu *factory.WorkspaceUser) {
		mu.WorkspaceID, mu.UserID, mu.Role = ownerless.ID, user().ID, auth.RoleMember
	}).CreateOne(ctx, db, f))(t)
	touched(ownerless)
	// One over its live-invitation quota (51 of 50), one at it, and one
	// over it nobody touched in the window.
	crowded := apitest.Must(factory.WorkspaceFor(ctx, db, f, user().ID))(t)
	apitest.Must(factory.InvitationsTo(crowded.ID).Create(ctx, db, f, store.MaxPendingInvitations+1))(t)
	touched(crowded)
	atQuota := apitest.Must(factory.WorkspaceFor(ctx, db, f, user().ID))(t)
	apitest.Must(factory.InvitationsTo(atQuota.ID).Create(ctx, db, f, store.MaxPendingInvitations))(t)
	touched(atQuota)
	quiet := apitest.Must(factory.WorkspaceFor(ctx, db, f, user().ID))(t)
	apitest.Must(factory.InvitationsTo(quiet.ID).Create(ctx, db, f, store.MaxPendingInvitations+1))(t)
	// A live invitation to someone already a member (an expired one isn't).
	member := user()
	joined := apitest.Must(factory.WorkspaceFor(ctx, db, f, member.ID))(t)
	apitest.Must(factory.InvitationsTo(joined.ID).State(factory.InvitedAs(member.Email, auth.RoleMember)).CreateOne(ctx, db, f))(t)
	touched(joined)
	// An admin without MFA whose window closed; one inside it isn't.
	stranded := user()
	apitest.Must(factory.Admins.State(factory.AdminFor(stranded.ID)).State(factory.ClosedSetupWindow).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.Admins.State(factory.AdminFor(user().ID)).State(factory.SetupWindow).CreateOne(ctx, db, f))(t)
	// A session ended 40 minutes ago; a reset link expired 10 minutes ago
	// is still the cleanup's.
	apitest.Must(factory.Sessions.State(func(se *factory.Session) {
		se.UserID, se.ExpiresAt = user().ID, time.Now().Add(-40*time.Minute)
	}).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.PasswordResets.State(factory.ResetFor(user().ID)).State(func(r *factory.PasswordReset) {
		r.ExpiresAt = time.Now().Add(-10 * time.Minute)
	}).CreateOne(ctx, db, f))(t)
	// A limit window ended 40 minutes ago, one 10 minutes ago.
	for _, ago := range []time.Duration{40 * time.Minute, 10 * time.Minute} {
		apitest.Must(factory.RateLimits.State(func(l *factory.RateLimit) { l.ResetsAt = time.Now().Add(-ago) }).CreateOne(ctx, db, f))(t)
	}
	// A delivery overdue by 15 minutes; one by 5 isn't yet.
	overdue := m.make("auth.password_reset.requested", map[string]any{"email": "a@example.test"}, "", 20*time.Minute)
	apitest.Must(factory.DeliveryOf(overdue.ID, "mail.password_reset").State(func(d *factory.EventDelivery) {
		d.NextAttemptAt = time.Now().Add(-15 * time.Minute)
	}).CreateOne(ctx, db, f))(t)
	late := m.make("auth.password_reset.requested", map[string]any{"email": "b@example.test"}, "", 6*time.Minute)
	apitest.Must(factory.DeliveryOf(late.ID, "mail.password_reset").State(func(d *factory.EventDelivery) {
		d.NextAttemptAt = time.Now().Add(-5 * time.Minute)
	}).CreateOne(ctx, db, f))(t)
	// A delivery failed for want of a listener, and one failed otherwise.
	orphan := m.make("workspace.created", map[string]any{"workspace": "wsp_00000000000001"}, "", 2*time.Hour)
	apitest.Must(factory.DeliveryOf(orphan.ID, "mail.renamed").State(func(d *factory.EventDelivery) {
		d.FailedAt, d.LastError = time.Now().Add(-time.Hour), `no listener "…" for workspace.created`
	}).CreateOne(ctx, db, f))(t)
	refused := m.make("auth.password_reset.requested", map[string]any{"email": "c@example.test"}, "", 2*time.Hour)
	apitest.Must(factory.DeliveryOf(refused.ID, "mail.password_reset").State(func(d *factory.EventDelivery) {
		d.FailedAt, d.LastError = time.Now().Add(-time.Hour), "smtp: <n> mailbox unavailable"
	}).CreateOne(ctx, db, f))(t)
	// An event named outside the catalogue.
	unknown := m.make("legacy.thing.happened", map[string]any{}, "", time.Hour)
	// Two live servers on two versions, the newer started 40 minutes ago,
	// and one built ahead of its database for an hour.
	apitest.Must(factory.ServerInstances.Sequence(func(i int, si *factory.ServerInstance) {
		si.Version = []string{"aaaaaaa1111", "bbbbbbb2222", "bbbbbbb2222"}[i]
		si.StartedAt = time.Now().Add(-[]time.Duration{3 * time.Hour, 40 * time.Minute, time.Hour}[i])
		si.Config = json.RawMessage([]string{`{"migrations":{"applied":25,"embedded":25}}`, `{"migrations":{"applied":25,"embedded":25}}`, `{"migrations":{"applied":24,"embedded":25}}`}[i])
	}).Create(ctx, db, f, 3))(t)

	v = integrity()
	page := sections[integritySections](t, v)
	for _, c := range []struct {
		name    string
		level   monitor.Level
		value   float64
		samples []string
	}{
		{"integrity.ownerless_workspaces", monitor.LevelCritical, 1, []string{ownerless.PublicID}},
		{"integrity.workspaces_over_quota", monitor.LevelCritical, 1, []string{crowded.PublicID}},
		{"integrity.member_invitations", monitor.LevelWarn, 1, []string{joined.PublicID}},
		{"integrity.admins_without_mfa", monitor.LevelCritical, 1, []string{stranded.PublicID}},
		{"integrity.expired_rows", monitor.LevelWarn, 1, []string{"sessions"}},
		{"integrity.ended_limit_windows", monitor.LevelWarn, 1, []string{}},
		{"integrity.overdue_deliveries", monitor.LevelCritical, 1, []string{overdue.PublicID}},
		{"integrity.unmatched_deliveries", monitor.LevelWarn, 1, []string{orphan.PublicID}},
		{"integrity.unknown_events", monitor.LevelWarn, 1, []string{unknown.PublicID}},
		{"integrity.servers_out_of_step", monitor.LevelWarn, 2, nil},
		{"integrity.critical_checks", monitor.LevelCritical, 4, nil},
		{"integrity.warning_checks", monitor.LevelWarn, 6, nil},
	} {
		got := v.signal(t, c.name)
		if got.Level != c.level || got.Value != c.value {
			t.Errorf("%s: %v at %v, want %v at %v", c.name, got.Level, got.Value, c.level, c.value)
		}
		if c.samples != nil && !slices.Equal(page.samples(t, c.name), c.samples) {
			t.Errorf("%s: samples %v, want %v", c.name, page.samples(t, c.name), c.samples)
		}
	}
	if steps := page.samples(t, "integrity.servers_out_of_step"); len(steps) != 2 || !strings.HasPrefix(steps[0], "srv_") {
		t.Errorf("servers out of step: %v", steps)
	}
}

// Duplicates and retries over the page's window: a create repeated within
// 10 seconds by its actor (not 20 seconds later, nor by someone else), a
// reset sent twice for one request, a delivery that took two attempts, and
// writes ended by the timeout (never reads).
func TestAdminMonitoringDuplicates(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	alice := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	bob := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	create := func(u factory.User, name string, ago time.Duration) factory.Event {
		return m.make("project.created", map[string]any{"workspace": "wsp_00000000000042", "name": name}, "", ago, func(e *factory.Event) { e.ActorID = u.ID })
	}
	create(alice, "Launch", time.Hour)
	twice := create(alice, "Launch", time.Hour-3*time.Second)
	create(alice, "Roadmap", 2*time.Hour)
	create(alice, "Roadmap", 2*time.Hour-20*time.Second)
	create(bob, "Launch", time.Hour-time.Second)
	m.make("workspace.created", map[string]any{"workspace": "wsp_00000000000043", "name": "Acme"}, "", 3*time.Hour, func(e *factory.Event) { e.ActorID = bob.ID })
	again := m.make("workspace.created", map[string]any{"workspace": "wsp_00000000000044", "name": "acme"}, "", 3*time.Hour-2*time.Second, func(e *factory.Event) { e.ActorID = bob.ID })
	// A reset sent twice for one request, another once.
	for _, request := range []string{"evt_00000000000001", "evt_00000000000001", "evt_00000000000002"} {
		m.make("auth.password_reset.sent", map[string]any{"email": "a@example.test", "request": request}, "", time.Hour)
	}
	// A delivery that took two attempts, and one that took one.
	retried := m.make("auth.password_reset.requested", map[string]any{"email": "a@example.test"}, "", time.Hour)
	once := m.make("auth.password_reset.requested", map[string]any{"email": "b@example.test"}, "", time.Hour)
	for i, e := range []factory.Event{retried, once} {
		apitest.Must(factory.DeliveryOf(e.ID, "mail.password_reset").State(func(d *factory.EventDelivery) {
			d.Attempts, d.DeliveredAt = 2-i, time.Now().Add(-50*time.Minute)
		}).CreateOne(ctx, db, f))(t)
	}
	// Timeouts: a write and a read.
	server := factory.ServerID(f)
	minute := time.Now().UTC().Truncate(time.Minute).Add(-10 * time.Minute)
	apitest.Must(factory.RequestMinutes.Sequence(func(i int, rm *factory.RequestMinute) {
		rm.Server, rm.Minute = server, minute
		rm.Route = []string{"POST /api/workspaces/{workspace}/projects", "GET /api/workspaces/{workspace}/projects"}[i]
		rm.Timeouts = 2 + int64(i)
	}).Create(ctx, db, f, 2))(t)

	page := sections[integritySections](t, showMonitoring(t, h, session.Cookie(), "integrity", ""))
	if d := page.Duplicates; d.Count != 2 || !slices.Contains(d.Samples, twice.PublicID) || !slices.Contains(d.Samples, again.PublicID) || len(d.ByName) != 2 {
		t.Fatalf("duplicates: %+v", d)
	}
	if d := page.DuplicateSends; d.Count != 1 || !slices.Equal(d.Samples, []string{"evt_00000000000001"}) {
		t.Fatalf("sent twice: %+v", d)
	}
	if d := page.RetriedDeliveries; d.Count != 1 || !slices.Equal(d.Samples, []string{retried.PublicID}) {
		t.Fatalf("retried: %+v", d)
	}
	if w := page.TimedOutWrites; len(w) != 1 || w[0].Route != "POST /api/workspaces/{workspace}/projects" || w[0].Count != 2 {
		t.Fatalf("timed out: %+v", w)
	}
}

// performanceSections is what the Performance test reads of its sections.
type performanceSections struct {
	MetricsStore string `json:"metricsStore"`
	Requests     []struct {
		Requests int64   `json:"requests"`
		P95      float64 `json:"p95"`
	} `json:"requests"`
	Routes []struct {
		Route    string  `json:"route"`
		Requests int64   `json:"requests"`
		P50      float64 `json:"p50"`
		P95      float64 `json:"p95"`
		P99      float64 `json:"p99"`
	} `json:"routes"`
	Database struct {
		Operations  []monitor.LatencyRow  `json:"operations"`
		SlowQueries []store.MetricTotal   `json:"slowQueries"`
		Waits       store.PoolWaits       `json:"waits"`
		Statements  *store.StatementStats `json:"statements"`
	} `json:"database"`
	Worker struct {
		Deliveries store.MonitorSeries  `json:"deliveries"`
		Lag        []monitor.LatencyRow `json:"lag"`
	} `json:"worker"`
	SlotsBusy int64               `json:"slotsBusy"`
	Servers   []monitor.ServerRow `json:"servers"`
}

// The Performance page from the minutes: routes by p50, p95 and p99 from
// their summed histograms, the slowest route's level, query latency by
// operation and slow queries by table, pool waits from each server's
// running totals (5 of the last 6 minutes a warning, a second's average
// wait critical), the worker's deliveries and lag, hashing refusals, and
// each server's latest gauges. With METRICS_STORE off on every live
// server, the page and the Overview say so.
func TestAdminMonitoringPerformance(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	si := apitest.Must(factory.ServerInstances.State(func(si *factory.ServerInstance) {
		si.Config = json.RawMessage(`{"metricsStore":"on"}`)
	}).CreateOne(ctx, db, f))(t)
	minute := time.Now().UTC().Truncate(time.Minute)
	// A fast route and a slow one: 30 requests, 10 under 5 ms and 20
	// between 2.5 and 5 s.
	apitest.Must(factory.RequestMinutes.Sequence(func(i int, rm *factory.RequestMinute) {
		rm.Server, rm.Minute = si.ID, minute.Add(-5*time.Minute)
		rm.Route = []string{"GET /api/auth/me", "POST /api/workspaces/{workspace}/projects"}[i]
		rm.Requests = []int64{100, 30}[i]
		rm.DurationBuckets = [][]int64{{100, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, {10, 0, 0, 0, 0, 0, 0, 0, 0, 20, 0, 0}}[i]
	}).Create(ctx, db, f, 2))(t)
	metric := func(name, labels string, at time.Time, count int64, sum float64, buckets []int64) {
		apitest.Must(factory.MetricMinutes.State(func(mm *factory.MetricMinute) {
			mm.Server, mm.Minute, mm.Metric, mm.Labels, mm.Count, mm.Sum, mm.Buckets = si.ID, at, name, labels, count, sum, buckets
		}).CreateOne(ctx, db, f))(t)
	}
	// Pool waits rising in the 5 minutes ending a minute ago, from a
	// running total: 3 waits a minute, 4 s waited each minute.
	for i := 7; i >= 1; i-- {
		at := minute.Add(-time.Duration(i) * time.Minute)
		waits := float64(7-i) * 3
		if i == 7 {
			waits = 0
		}
		metric("db_pool_wait_count", "", at, 1, waits, nil)
		metric("db_pool_wait_ms", "", at, 1, waits*1333.4, nil)
	}
	gauge := func(name string, value float64) { metric(name, "", minute.Add(-time.Minute), 1, value, nil) }
	gauge("go_goroutines", 42)
	gauge("db_pool_in_use", 3)
	at := minute.Add(-3 * time.Minute)
	metric("db_query_duration_ms", `operation="select"`, at, 100, 500, []int64{90, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	metric("db_slow_queries_total", `table="events"`, at, 4, 0, nil)
	metric("delivery_lag_ms", `listener="mail.password_reset"`, at, 10, 100, []int64{0, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	metric("deliveries_total", `listener="mail.password_reset",outcome="delivered"`, at, 10, 0, nil)
	metric("password_hash_busy_total", "", at, 2, 0, nil)

	v := showMonitoring(t, h, session.Cookie(), "performance", "")
	for _, c := range []struct {
		name  string
		level monitor.Level
		check func(float64) bool
	}{
		{"http.slowest_route_p95", monitor.LevelWarn, func(x float64) bool { return x > 4000 && x <= 5000 }},
		{"http.latency_p95", monitor.LevelCritical, func(x float64) bool { return x > 3000 }},
		{"db.round_trip", monitor.LevelOK, func(x float64) bool { return x > 0 && x < 500 }},
		{"db.pool_wait_minutes", monitor.LevelWarn, func(x float64) bool { return x == 6 }},
		{"db.pool_wait_time", monitor.LevelCritical, func(x float64) bool { return x > 1000 }},
		{"events.delivery_lag_p95", monitor.LevelOK, func(x float64) bool { return x > 5 && x <= 10 }},
		{"auth.password_hash_busy", monitor.LevelWarn, func(x float64) bool { return x == 2 }},
	} {
		got := v.signal(t, c.name)
		if got.Level != c.level || !c.check(got.Value) {
			t.Errorf("%s: %v at %v", c.name, got.Level, got.Value)
		}
	}
	page := sections[performanceSections](t, v)
	if page.MetricsStore != monitor.MetricsStoreOn || len(page.Routes) != 2 || page.Routes[0].Route != "GET /api/auth/me" {
		t.Fatalf("routes: %+v", page)
	}
	if slow := page.Routes[1]; slow.P50 <= 2500 || slow.P95 <= 4000 || slow.P99 < slow.P95 {
		t.Fatalf("slow route: %+v", slow)
	}
	db1 := page.Database
	if len(db1.Operations) != 1 || db1.Operations[0].Label != "select" || db1.Operations[0].P95 > 10 || len(db1.SlowQueries) != 1 || db1.SlowQueries[0].Count != 4 {
		t.Fatalf("database: %+v", db1)
	}
	if db1.Waits.Waits != 18 || db1.Waits.Minutes != 6 || db1.Statements == nil {
		t.Fatalf("waits: %+v", db1)
	}
	if len(page.Worker.Lag) != 1 || page.Worker.Deliveries.Series[0] != "delivered" || page.SlotsBusy != 2 {
		t.Fatalf("worker: %+v", page.Worker)
	}
	if len(page.Servers) != 1 || !page.Servers[0].Live || *page.Servers[0].Goroutines != 42 || *page.Servers[0].PoolInUse != 3 || page.Servers[0].HeapBytes != nil {
		t.Fatalf("servers: %+v", page.Servers)
	}

	// Metric storage off on every live server: said on both pages.
	apitest.Must(db.NewUpdate().Model((*factory.ServerInstance)(nil)).Set("config = ?", `{"metricsStore":"off"}`).Where("id = ?", si.ID).Exec(ctx))(t)
	if got := sections[performanceSections](t, showMonitoring(t, h, session.Cookie(), "performance", "")).MetricsStore; got != monitor.MetricsStoreOff {
		t.Fatalf("metrics store: %q", got)
	}
	overview := sections[struct {
		MetricsStore string `json:"metricsStore"`
	}](t, showMonitoring(t, h, session.Cookie(), "overview", ""))
	if overview.MetricsStore != monitor.MetricsStoreOff {
		t.Fatalf("overview: %+v", overview)
	}
}

// Statements are shown only when PostgreSQL normalised them: a query or a
// data change with $n placeholders, never a utility statement, a string
// constant, a dollar quote or a comment.
func TestSafeStatement(t *testing.T) {
	for query, want := range map[string]bool{
		"SELECT u.id FROM users AS u WHERE u.email = $1":                   true,
		"  with x AS (SELECT $1)\n SELECT * FROM x":                        true,
		"(SELECT 1) UNION (SELECT 2)":                                      true,
		`UPDATE "sessions" SET last_seen_at = now() WHERE token_hash = $1`: true,
		"SELECT * FROM users WHERE email = 'ada@example.test'":             false,
		"SET application_name = 'x'":                                       false,
		"ALTER ROLE app PASSWORD $1":                                       false,
		"SELECT $tag$secret$tag$":                                          false,
		"SELECT $$secret$$":                                                false,
		"SELECT 1 -- ada@example.test":                                     false,
		"SELECT 1 /* token */":                                             false,
		`SELECT E\'x\'`:                                                    false,
		"COPY users TO STDOUT":                                             false,
		"SELECT \x00":                                                      false,
	} {
		if _, ok := store.SafeStatement(query); ok != want {
			t.Errorf("%q: %v, want %v", query, ok, want)
		}
	}
	long, ok := store.SafeStatement("SELECT " + strings.Repeat("a, ", 600) + "b FROM t")
	if !ok || len([]rune(long)) != store.MaxStatement+1 {
		t.Fatalf("a long statement: %d", len([]rune(long)))
	}
}

// errorsSections is the Errors page's sections.
type errorsSections struct {
	Release     *store.Release      `json:"release"`
	Groups      []store.ErrorGroup  `json:"groups"`
	Occurrences store.MonitorSeries `json:"occurrences"`
}

// The Errors page lists the groups seen in the window, newest first, with
// their last request ID, a release only in its shape, and a browser's
// group as reported; the release now running is the newest server's
// version from its first start. One group's page has its stack and its
// route's occurrences, records admin.error.viewed, and is 404 for an ID
// that isn't a group; nothing is recorded for a refusal.
func TestAdminMonitoringErrors(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	admin, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	m.make("server.started", map[string]any{"server": "srv_00000000000001", "version": "0000000aaaa"}, "", 3*24*time.Hour)
	m.make("server.started", map[string]any{"server": "srv_00000000000002", "version": "abc1234def56"}, "", 2*time.Hour)
	m.make("server.started", map[string]any{"server": "srv_00000000000003", "version": "abc1234def56"}, "", time.Hour)
	groups := apitest.Must(factory.AppErrors.Sequence(func(i int, e *factory.AppError) {
		e.Source = []string{"request", "client", "request"}[i]
		e.Route = []string{"POST /api/workspaces/{workspace}/projects", "/app/:workspace/projects", "GET /api/auth/me"}[i]
		e.LastVersion = []string{"abc1234def56", "Claim_your_prize", "dev"}[i]
		e.LastRequestID = []string{"req_00000000000042", "", ""}[i]
		e.Stack = "store.(*Store).CreateProject(…)\n\tprojects.go:<n>"
		e.FirstSeenAt = time.Now().Add(-[]time.Duration{30 * time.Minute, 3 * time.Hour, 5 * 24 * time.Hour}[i])
		e.LastSeenAt = time.Now().Add(-[]time.Duration{time.Minute, 2 * time.Hour, 3 * 24 * time.Hour}[i])
	}).Create(ctx, db, f, 3))(t)
	server := factory.ServerID(f)
	apitest.Must(factory.MetricMinutes.Sequence(func(i int, mm *factory.MetricMinute) {
		mm.Server, mm.Minute, mm.Metric = server, time.Now().UTC().Truncate(time.Minute).Add(-10*time.Minute), "errors_total"
		mm.Labels = []string{`route="POST /api/workspaces/{workspace}/projects",source="request"`, `route="/app/:workspace/projects",source="client"`}[i]
		mm.Count = []int64{5, 3}[i]
	}).Create(ctx, db, f, 2))(t)

	v := showMonitoring(t, h, session.Cookie(), "errors", "")
	page := sections[errorsSections](t, v)
	if page.Release == nil || page.Release.Version != "abc1234def56" || time.Since(page.Release.StartedAt) < 119*time.Minute {
		t.Fatalf("release: %+v", page.Release)
	}
	if len(page.Groups) != 2 {
		t.Fatalf("groups: %+v", page.Groups)
	}
	first, client := page.Groups[0], page.Groups[1]
	var id string
	apitest.Must(0, db.NewSelect().Model((*factory.AppError)(nil)).Column("public_id").Where("id = ?", groups[0].ID).Scan(ctx, &id))(t)
	if first.ID != id || first.LastRequestID != "req_00000000000042" || first.LastVersion != "abc1234def56" || first.Count == 0 {
		t.Fatalf("first: %+v", first)
	}
	if client.Source != "client" || client.LastVersion != "" {
		t.Fatalf("a browser's group: %+v", client)
	}
	var occurrences int
	for _, b := range page.Occurrences.Buckets {
		for _, n := range b.Counts {
			occurrences += n
		}
	}
	if occurrences != 8 || len(page.Occurrences.Series) != 2 {
		t.Fatalf("occurrences: %+v", page.Occurrences)
	}
	if strings.Contains(adminCall(t, h, "GET", "/api/admin/monitoring/errors", "", "", 200, session.Cookie()).Body.String(), "store.(*Store)") {
		t.Fatal("the list holds a stack")
	}

	// One group: its stack and its route's occurrences, recorded.
	w := adminCall(t, h, "GET", "/api/admin/errors/"+id+"?window=1h", "", "", 200, session.Cookie())
	one := decodeAdmin[monitor.ErrorView](t, w.Body.Bytes())
	if one.Group.ID != id || !strings.Contains(one.Group.Stack, "projects.go") || one.Release == nil || one.Window.Name != "1h" {
		t.Fatalf("one group: %+v", one)
	}
	if len(one.Occurrences.Series) != 1 || one.Occurrences.Series[0] != "request" {
		t.Fatalf("its occurrences: %+v", one.Occurrences)
	}
	viewed := apitest.EventsNamed(t, db, "admin.error.viewed")
	if len(viewed) != 1 || viewed[0].Payload["error"] != id || viewed[0].Payload["window"] != "1h" || viewed[0].Payload["admin"] != admin.User.PublicID || viewed[0].UserID.Valid {
		t.Fatalf("recorded: %+v", viewed)
	}
	adminCall(t, h, "GET", "/api/admin/errors/err_00000000000000", "", "", 404, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/errors/1", "", "", 404, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/errors/"+id+"?window=30d", "", "", 400, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/errors/"+id+"?page=2", "", "", 400, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/errors/"+id, "", "", 401)
	release, ok := s.Summaries.Acquire(ctx, admin.User.PublicID)
	if !ok {
		t.Fatal("no slot")
	}
	adminCall(t, h, "GET", "/api/admin/errors/"+id, "", "", 503, session.Cookie())
	release()
	if n := len(apitest.EventsNamed(t, db, "admin.error.viewed")); n != 1 {
		t.Fatalf("%d reads recorded", n)
	}
}

// configurationSections is the Configuration page's sections.
type configurationSections struct {
	Servers  []monitor.ServerInfo `json:"servers"`
	Compared []string             `json:"compared"`
	Settings []monitor.Setting    `json:"settings"`
	Warnings []monitor.Warning    `json:"warnings"`
}

// The Configuration page compares the live servers' settings from their
// summaries, field by field: a summary holding secrets (as a future
// summary might by mistake, or a row written by hand) shows none of them,
// nor a digest. Its warnings: mail off, an allowlist without the client
// address header, migrations pending, two versions live, sessions under
// the key being retired (counted under the server's own previous key),
// and the proxy's address everywhere.
func TestAdminMonitoringConfiguration(t *testing.T) {
	s, db := server(t)
	previous := "previous-admin-key-0123456789abcdefghijklmnopqrstu"
	s.Config.AdminRouteKeyPrevious = previous
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	admin, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	secrets := []string{"AppKeyS3cretValue-0123456789", "hunter2-database-password", "MailPassw0rd-smtp-secret", "smtp-username-alice", previous, auth.Digest(previous), "metrics-token-0123456789abcdef0123"}
	config := func(mail, version string, extra string) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"production":true,"mailDriver":%q,"smtp":{"host":"smtp.example.test","port":587,"tls":"starttls","username":%q,"password":%q},
			"emailVerification":"on","rateLimits":"on","clientIpHeader":"","trustedProxies":0,"adminRouteKey":true,"adminRouteKeyPrevious":true,
			"adminAllowedIps":2,"logFormat":"json","logLevel":"INFO","accessLog":"all","metricsStore":"on","metricsAddr":true,"metricsToken":true,
			"slowQueryMs":500,"shutdownDelayMs":5000,"migrations":{"applied":%s,"embedded":25},
			"appKey":%q,"databaseUrl":"postgres://lew:%s@db.internal/app","metricsTokenValue":%q,"adminKey":%q,"adminKeyDigest":%q%s}`,
			mail, secrets[3], secrets[2], version, secrets[0], secrets[1], secrets[6], previous, secrets[5], extra))
	}
	apitest.Must(factory.ServerInstances.Sequence(func(i int, si *factory.ServerInstance) {
		si.Version = []string{"abc1234def56", "def5678abc12"}[i]
		si.Config = []json.RawMessage{config("smtp", "25", ""), config("off", "24", `,"note":"x"`)}[i]
	}).Create(ctx, db, f, 2))(t)
	// Two admin sessions under the key being retired, one live.
	other := withAdmin(t, db, f)
	apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(other.User.ID)).State(factory.UnderKey(previous)).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(admin.User.ID)).State(factory.UnderKey(previous)).State(factory.AdminExpired).CreateOne(ctx, db, f))(t)
	// 60 events today, all from one address.
	m.many(60, "auth.login_attempted", func(int) map[string]any { return map[string]any{"result": "succeeded"} }, same("10.0.0.2"), time.Hour)

	w := adminCall(t, h, "GET", "/api/admin/monitoring/configuration", "", "", 200, session.Cookie())
	for _, secret := range secrets {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("the page holds a secret %q: %s", secret, w.Body.String())
		}
	}
	for _, word := range []string{"smtp-username", "databaseUrl", "appKey", "note"} {
		if strings.Contains(w.Body.String(), word) {
			t.Fatalf("the page holds a field it doesn't list: %s", word)
		}
	}
	v := decodeAdmin[monitoringView](t, w.Body.Bytes())
	page := sections[configurationSections](t, v)
	if len(page.Servers) != 2 || len(page.Compared) != 2 || !page.Servers[0].Live {
		t.Fatalf("servers: %+v", page)
	}
	setting := func(name string) monitor.Setting {
		for _, s := range page.Settings {
			if s.Name == name {
				return s
			}
		}
		t.Fatalf("no setting %s", name)
		return monitor.Setting{}
	}
	if smtp := setting("SMTP"); smtp.Values[0] != "smtp.example.test:587, starttls" || smtp.Differs {
		t.Fatalf("smtp: %+v", smtp)
	}
	if mail := setting("MAIL_DRIVER"); !mail.Differs || !slices.Contains(mail.Values, "off") {
		t.Fatalf("mail: %+v", mail)
	}
	if key := setting("ADMIN_ROUTE_KEY_PREVIOUS"); key.Values[0] != "set" {
		t.Fatalf("previous key: %+v", key)
	}
	warnings := map[string]float64{}
	for _, w := range page.Warnings {
		warnings[w.ID] = w.Count
	}
	want := map[string]float64{
		monitor.WarnMailOff: 1, monitor.WarnAllowlistNoIP: 2, monitor.WarnMigrations: 1, monitor.WarnVersions: 2,
		monitor.WarnPreviousKey: 1, monitor.WarnSharedAddress: 100,
	}
	for id, n := range want {
		if got, ok := warnings[id]; !ok || got != n {
			t.Errorf("warning %s: %v (%v), want %v", id, got, ok, n)
		}
	}
	for _, id := range []string{monitor.WarnNoAdminMFA, monitor.WarnNotProduction, monitor.WarnMetricsTokenOff} {
		if _, ok := warnings[id]; ok {
			t.Errorf("warning %s with nothing to warn of", id)
		}
	}
	if got := v.signal(t, "servers.versions"); got.Level != monitor.LevelWarn {
		t.Fatalf("versions: %+v", got)
	}
}
