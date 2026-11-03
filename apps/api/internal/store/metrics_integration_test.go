package store

import (
	"context"
	"encoding/json"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/observe"
	"testing"
	"time"
)

func testServer(id string) ServerInstance {
	now := time.Now()
	return ServerInstance{ID: id, Hostname: "test-host", Version: "dev", StartedAt: now, LastSeenAt: now, Config: json.RawMessage(`{"production":false}`)}
}

func countRows(t *testing.T, s *Store, model any, where string, args ...any) int {
	t.Helper()
	q := s.DB.NewSelect().Model(model)
	if where != "" {
		q = q.Where(where, args...)
	}
	n, err := q.Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// A minute with 10,000 requests to one route is one request_minutes row:
// what is stored depends on the routes, never the traffic. The same
// statement writes the series and the heartbeat; a minute written again is
// left as it was (insert only); a row that breaks the tables' rules is
// left out without failing the others.
func TestRecordMetrics(t *testing.T) {
	s, ctx := queueStore(t, nil)
	r := observe.NewRegistry()
	refused := r.Counter("rate_limit_refused_total", "Refused.", "limit")
	for range 10000 {
		r.Requests.Observe("GET /api/workspaces/{workspace}/projects", 200, 3*time.Millisecond, false, false)
	}
	refused.Add(4, "auth:per-ip:block")
	server := testServer(observe.NewServerID())
	minute := time.Now().Truncate(time.Minute)
	if err := s.RecordMetrics(ctx, server, r.TakeMinute(minute)); err != nil {
		t.Fatal(err)
	}
	var rows []requestMinute
	if err := s.DB.NewSelect().Model(&rows).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Requests != 10000 || rows[0].Server != server.ID || rows[0].DurationBuckets[0] != 10000 || rows[0].DurationSumMs != 30000 {
		t.Fatalf("request_minutes: %+v", rows)
	}
	var series []metricMinute
	if err := s.DB.NewSelect().Model(&series).Where("metric = 'rate_limit_refused_total'").Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 || series[0].Count != 4 || series[0].Labels != `limit="auth:per-ip:block"` {
		t.Fatalf("metric_minutes: %+v", series)
	}
	var stored ServerInstance
	if err := s.DB.NewSelect().Model(&stored).Where("id = ?", server.ID).Scan(ctx); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	// Another minute: the heartbeat moves; the same minute again does nothing.
	time.Sleep(10 * time.Millisecond)
	r.Requests.Observe("GET /api/workspaces/{workspace}/projects", 200, time.Millisecond, false, false)
	if err := s.RecordMetrics(ctx, server, r.TakeMinute(minute)); err != nil {
		t.Fatal(err)
	}
	var later ServerInstance
	if err := s.DB.NewSelect().Model(&later).Where("id = ?", server.ID).Scan(ctx); err != nil || !later.LastSeenAt.After(stored.LastSeenAt) {
		t.Fatalf("heartbeat not moved: %v → %v (%v)", stored.LastSeenAt, later.LastSeenAt, err)
	}
	if n := countRows(t, s, (*requestMinute)(nil), ""); n != 1 {
		t.Fatalf("a minute written twice: %d rows", n)
	}
	// Bad rows are left out; the good one and the heartbeat go in.
	bad := observe.Minute{Minute: minute.Add(time.Minute), Routes: []observe.RouteTotals{
		{Route: "GET /good", Requests: 1}, {Route: "", Requests: 1}, {Route: "GET /none", Requests: 0}, {Route: "GET /\xff", Requests: 1},
	}, Series: []observe.SeriesMinute{{Metric: "Bad Name", Count: 1}, {Metric: "ok_total", Count: 0}, {Metric: "ok_total", Labels: "limit=\"\xfe\"", Count: 1}}}
	if err := s.RecordMetrics(ctx, server, bad); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, (*requestMinute)(nil), "minute = ?", bad.Minute); n != 1 {
		t.Fatalf("bad rows: %d stored", n)
	}
	if n := countRows(t, s, (*metricMinute)(nil), "minute = ?", bad.Minute); n != 0 {
		t.Fatalf("bad series: %d stored", n)
	}
	// A minute far from the database's clock (a server whose clock is
	// wrong) is dropped, ahead or behind; its heartbeat still goes in.
	// Behind: one that starts RollupDelay ago could arrive after its hour
	// was summed.
	for _, skew := range []time.Duration{10 * time.Minute, -RollupDelay - time.Minute, -15 * time.Minute, 24 * time.Hour} {
		far := observe.Minute{Minute: minute.Add(skew), Routes: []observe.RouteTotals{{Route: "GET /far", Requests: 1}}}
		if err := s.RecordMetrics(ctx, server, far); err != nil {
			t.Fatal(err)
		}
		if n := countRows(t, s, (*requestMinute)(nil), "route = 'GET /far'"); n != 0 {
			t.Fatalf("a minute %v away was stored", skew)
		}
	}
	// Only the heartbeat, as with METRICS_STORE=off.
	if err := s.RecordMetrics(ctx, testServer(observe.NewServerID()), observe.Minute{Minute: minute}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, (*ServerInstance)(nil), ""); n != 2 {
		t.Fatalf("servers: %d", n)
	}
}

// The rollup sums each finished hour across servers, once; deletes
// minutes older than 48 hours (more than one batch), never one it hasn't
// summed; and does nothing while another server holds its lock.
func TestSummarizeMetrics(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	now := time.Now().UTC()
	hour := now.Truncate(time.Hour).Add(-2 * time.Hour)
	a, b := factory.ServerID(f), factory.ServerID(f)
	route := "GET /api/auth/me"
	// Two servers, two minutes each, in the same hour.
	if _, err := factory.RequestMinutes.Sequence(func(i int, m *factory.RequestMinute) {
		m.Server = []string{a, a, b, b}[i]
		m.Minute = hour.Add(time.Duration(i%2) * time.Minute)
		m.Route, m.Requests, m.Status5xx, m.DurationSumMs = route, 10, 1, 20
		m.DurationBuckets = []int64{10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	}).Create(ctx, s.DB, f, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.MetricMinutes.Sequence(func(i int, m *factory.MetricMinute) {
		m.Server, m.Minute = []string{a, b}[i], hour.Add(5*time.Minute)
		m.Metric, m.Labels, m.Count, m.Sum = "go_goroutines", "", 1, float64(10*(i+1))
	}).Create(ctx, s.DB, f, 2); err != nil {
		t.Fatal(err)
	}
	// The current hour isn't finished: summed later.
	if _, err := factory.RequestMinutes.State(func(m *factory.RequestMinute) {
		m.Server, m.Minute, m.Route = a, now.Truncate(time.Minute), route
	}).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	// Minutes older than 48 hours: summed (they never were) then deleted,
	// more of them than one delete batch holds.
	old := now.Add(-MinuteRetention - 3*time.Hour).Truncate(time.Hour)
	if _, err := factory.RequestMinutes.Sequence(func(i int, m *factory.RequestMinute) {
		m.Server = factory.ServerID(f)
		m.Minute = old.Add(time.Duration(i%60) * time.Minute)
		m.Route = route
	}).Create(ctx, s.DB, f, minuteDeleteBatch+500); err != nil {
		t.Fatal(err)
	}

	// Another server holds the lock: nothing happens.
	holder, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if locked, err := database.TryLockMetricsRollup(ctx, holder); err != nil || !locked {
		t.Fatal(locked, err)
	}
	if r, err := s.SummarizeMetrics(ctx); err != nil || r.Locked || countRows(t, s, (*requestHour)(nil), "") != 0 {
		t.Fatalf("while locked: %+v %v", r, err)
	}
	if err := holder.Rollback(); err != nil {
		t.Fatal(err)
	}

	r, err := s.SummarizeMetrics(ctx)
	if err != nil || !r.Locked || r.MinutesDeleted != minuteDeleteBatch+500 {
		t.Fatalf("rollup: %+v %v", r, err)
	}
	var hours []requestHour
	if err := s.DB.NewSelect().Model(&hours).Order("hour").Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(hours) != 2 || !hours[0].Hour.Equal(old) || hours[0].Requests == 0 {
		t.Fatalf("hours: %+v", hours)
	}
	h := hours[1]
	if !h.Hour.Equal(hour) || h.Route != route || h.Requests != 40 || h.Status5xx != 4 || h.DurationSumMs != 80 || h.DurationBuckets[0] != 40 {
		t.Fatalf("summed hour: %+v", h)
	}
	var gauge metricHour
	if err := s.DB.NewSelect().Model(&gauge).Where("metric = 'go_goroutines'").Scan(ctx); err != nil || gauge.Count != 2 || gauge.Sum != 30 || !gauge.Hour.Equal(hour) {
		t.Fatalf("gauge hour: %+v %v", gauge, err)
	}
	if n := countRows(t, s, (*requestMinute)(nil), ""); n != 5 {
		t.Fatalf("%d minutes left, want the 5 under 48 hours", n)
	}
	// Again: nothing new to sum.
	if r, err := s.SummarizeMetrics(ctx); err != nil || r.Hours != 0 || r.RequestRows != 0 || countRows(t, s, (*requestHour)(nil), "") != 2 {
		t.Fatalf("second run: %+v %v", r, err)
	}
}

// server.started and server.stopped are recorded with the server's row;
// rows of servers silent for longer than the cutoff are deleted.
func TestServerInstances(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	server := testServer(observe.NewServerID())
	if err := s.RecordServerStart(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordServerStop(ctx, server, "signal"); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := s.DB.NewSelect().Model((*eventRecord)(nil)).Column("name").Where("payload->>'server' = ?", server.ID).Order("id").Scan(ctx, &names); err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "server.started" || names[1] != "server.stopped" {
		t.Fatalf("events: %v", names)
	}
	if _, err := factory.ServerInstances.State(factory.LastSeenAt(time.Now().Add(-25*time.Hour))).Create(ctx, s.DB, f, 2); err != nil {
		t.Fatal(err)
	}
	n, err := s.DeleteSilentServerInstances(ctx, 24*time.Hour)
	if err != nil || n != 2 || countRows(t, s, (*ServerInstance)(nil), "") != 1 {
		t.Fatalf("deleted %d (%v)", n, err)
	}
}

// Pending deliveries are counted for their gauge.
func TestCountPendingDeliveries(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	event, err := factory.Events.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.DeliveryOf(event.ID, "mail.registration").CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	if n, err := s.CountPendingDeliveries(ctx); err != nil || n != 1 {
		t.Fatalf("%d %v", n, err)
	}
}

// When a run can't sum, minutes older than MinuteCeiling are deleted
// anyway, summed or not, so the minute tables stay bounded.
func TestMinutesPastTheCeilingGo(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	now := time.Now().UTC()
	if _, err := factory.RequestMinutes.Sequence(func(i int, m *factory.RequestMinute) {
		m.Minute = now.Add(-[]time.Duration{MinuteCeiling + time.Hour, MinuteCeiling - time.Hour}[i]).Truncate(time.Minute)
	}).Create(ctx, s.DB, f, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.MetricMinutes.State(func(m *factory.MetricMinute) {
		m.Minute = now.Add(-MinuteCeiling - time.Hour).Truncate(time.Minute)
	}).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	n, err := s.deleteMinutesPastCeiling(ctx)
	if err != nil || n != 2 || countRows(t, s, (*requestMinute)(nil), "") != 1 || countRows(t, s, (*metricMinute)(nil), "") != 0 {
		t.Fatalf("deleted %d (%v)", n, err)
	}
}

// A run that runs out of time keeps the hours it finished, and the next
// run carries on from there; the minutes past the ceiling go even when the
// run's own context has ended.
func TestRollupProgressesAcrossTimeouts(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	now := time.Now().UTC()
	first := now.Truncate(time.Hour).Add(-6 * time.Hour)
	// A backlog: one minute in each of five finished hours.
	if _, err := factory.RequestMinutes.Sequence(func(i int, m *factory.RequestMinute) {
		m.Minute, m.Route = first.Add(time.Duration(i)*time.Hour), "GET /api/auth/me"
	}).Create(ctx, s.DB, f, 5); err != nil {
		t.Fatal(err)
	}
	// End the run after its second hour commits.
	run, cancel := context.WithCancel(ctx)
	hours := 0
	afterRollupHour = func() {
		if hours++; hours == 2 {
			cancel()
		}
	}
	t.Cleanup(func() { afterRollupHour = nil })
	if _, err := s.SummarizeMetrics(run); err == nil {
		t.Fatal("a canceled run reported no error")
	}
	if n := countRows(t, s, (*requestHour)(nil), ""); n != 2 {
		t.Fatalf("%d hours kept from the run that stopped, want 2", n)
	}
	afterRollupHour = nil
	if r, err := s.SummarizeMetrics(ctx); err != nil || r.Hours != 3 || countRows(t, s, (*requestHour)(nil), "") != 5 {
		t.Fatalf("the next run: %+v %v", r, err)
	}

	// A run whose deadline has passed sums nothing, but still deletes the
	// minutes past the ceiling, on a context of its own.
	if _, err := factory.RequestMinutes.State(func(m *factory.RequestMinute) {
		m.Minute = now.Add(-MinuteCeiling - time.Hour).Truncate(time.Minute)
	}).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	expired, cancelExpired := context.WithDeadline(ctx, now.Add(-time.Second))
	defer cancelExpired()
	r, err := s.SummarizeMetrics(expired)
	if err == nil || r.MinutesDeleted != 1 || countRows(t, s, (*requestMinute)(nil), "minute < ?", now.Add(-MinuteCeiling)) != 0 {
		t.Fatalf("timed out: %+v %v", r, err)
	}
	// A run canceled because the server is stopping deletes nothing more.
	if _, err := factory.RequestMinutes.State(func(m *factory.RequestMinute) {
		m.Minute = now.Add(-MinuteCeiling - 2*time.Hour).Truncate(time.Minute)
	}).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	stopping, stop := context.WithCancel(ctx)
	stop()
	if r, _ := s.SummarizeMetrics(stopping); r.MinutesDeleted != 0 {
		t.Fatalf("stopping: %+v", r)
	}
}

// Minutes dated after the database's clock (a server running ahead) are
// never summed into an hour.
func TestRollupIgnoresMinutesFromTheFuture(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	future := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	if _, err := factory.RequestMinutes.State(func(m *factory.RequestMinute) { m.Minute = future }).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Truncate(time.Hour).Add(-2 * time.Hour)
	if _, err := factory.RequestMinutes.State(func(m *factory.RequestMinute) { m.Minute = past }).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	if r, err := s.SummarizeMetrics(ctx); err != nil || r.Hours != 1 {
		t.Fatalf("rollup: %+v %v", r, err)
	}
	if n := countRows(t, s, (*requestHour)(nil), "hour >= now()"); n != 0 {
		t.Fatal("an hour from the future was summed")
	}
}
