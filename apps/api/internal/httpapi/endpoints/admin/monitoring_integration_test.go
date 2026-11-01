package admin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
)

// monitoringView is a Monitoring page's answer, its sections left raw.
type monitoringView struct {
	Page   string `json:"page"`
	Window struct {
		Name          string    `json:"name"`
		From          time.Time `json:"from"`
		To            time.Time `json:"to"`
		BucketSeconds int       `json:"bucketSeconds"`
	} `json:"window"`
	Signals  []monitor.Result `json:"signals"`
	Sections json.RawMessage  `json:"sections"`
}

// showMonitoring loads a Monitoring page and decodes it, checking it holds
// no field an admin response mustn't.
func showMonitoring(t *testing.T, h http.Handler, cookie *http.Cookie, page, query string) monitoringView {
	t.Helper()
	w := adminCall(t, h, "GET", "/api/admin/monitoring/"+page+query, "", "", 200, cookie)
	return decodeAdmin[monitoringView](t, w.Body.Bytes())
}

// signal finds a signal in a page's answer.
func (v monitoringView) signal(t *testing.T, name string) monitor.Result {
	t.Helper()
	for _, s := range v.Signals {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no signal %s on %s", name, v.Page)
	return monitor.Result{}
}

// sections decodes a page's sections.
func sections[T any](t *testing.T, v monitoringView) T {
	t.Helper()
	var out T
	apitest.Must(0, json.Unmarshal(v.Sections, &out))(t)
	return out
}

// eventMaker writes events through the factory: a name, its payload, the
// address it came from and how long ago.
type eventMaker struct {
	t  *testing.T
	db bun.IDB
	f  *gofakeit.Faker
}

func (m eventMaker) make(name string, payload map[string]any, ip string, ago time.Duration, state ...func(*factory.Event)) factory.Event {
	m.t.Helper()
	e := factory.Events.State(factory.EventOf(name, payload)).State(func(e *factory.Event) {
		e.IP, e.CreatedAt = ip, time.Now().Add(-ago)
	})
	for _, s := range state {
		e = e.State(s)
	}
	return apitest.Must(e.CreateOne(context.Background(), m.db, m.f))(m.t)
}

// many writes n copies of an event, each from ip(i).
func (m eventMaker) many(n int, name string, payload func(i int) map[string]any, ip func(i int) string, ago time.Duration) {
	m.t.Helper()
	if n == 0 {
		return
	}
	apitest.Must(factory.Events.Sequence(func(i int, e *factory.Event) {
		e.Name, e.Payload, e.IP, e.CreatedAt = name, payload(i), ip(i), time.Now().Add(-ago)
	}).Create(context.Background(), m.db, m.f, n))(m.t)
}

func same(ip string) func(int) string { return func(int) string { return ip } }

// Every page answers each window, records exactly one read naming the
// page and window, and refuses an unknown page, window or parameter.
// A viewer (the factory's admin) may read every page.
func TestAdminMonitoringPages(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	admin, session := adminSignedIn(t, db, factory.TestFaker(t))
	cookie := session.Cookie()
	for _, page := range monitor.PageNames() {
		for _, window := range []string{"", "1h", "24h", "7d"} {
			query := ""
			if window != "" {
				query = "?window=" + window
			}
			before := len(apitest.EventsNamed(t, db, "admin.monitoring.viewed"))
			v := showMonitoring(t, h, cookie, page, query)
			want := window
			if want == "" {
				want = "24h"
			}
			if v.Page != page || v.Window.Name != want || !v.Window.From.Before(v.Window.To) || v.Window.BucketSeconds == 0 || len(v.Signals) == 0 {
				t.Fatalf("%s %s: %+v", page, window, v)
			}
			recorded := apitest.EventsNamed(t, db, "admin.monitoring.viewed")
			if len(recorded) != before+1 {
				t.Fatalf("%s %s: %d reads recorded", page, window, len(recorded)-before)
			}
			last := recorded[len(recorded)-1]
			if last.Payload["admin"] != admin.User.PublicID || last.Payload["page"] != page || last.Payload["window"] != want || last.ActorID.Int64 != admin.User.ID || last.UserID.Valid {
				t.Fatalf("%s %s: recorded %+v", page, window, last)
			}
		}
	}
	before := len(apitest.EventsNamed(t, db, "admin.monitoring.viewed"))
	adminCall(t, h, "GET", "/api/admin/monitoring/ips", "", "", 404, cookie)
	adminCall(t, h, "GET", "/api/admin/monitoring/security?window=30d", "", "", 400, cookie)
	adminCall(t, h, "GET", "/api/admin/monitoring/security?window=all", "", "", 400, cookie)
	adminCall(t, h, "GET", "/api/admin/monitoring/security?page=2", "", "", 400, cookie)
	adminCall(t, h, "GET", "/api/admin/monitoring/security?window=1h&window=7d", "", "", 400, cookie)
	adminCall(t, h, "GET", "/api/admin/monitoring/security", "", "", 401)
	if n := len(apitest.EventsNamed(t, db, "admin.monitoring.viewed")); n != before {
		t.Fatalf("refused requests recorded %d reads", n-before)
	}
}

// Past its bounds a page answers 503 and records nothing: busy while the
// admin already has one in flight, too slow past the deadline.
func TestAdminMonitoringBounds(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	admin, session := adminSignedIn(t, db, factory.TestFaker(t))
	release, ok := s.Summaries.Acquire(context.Background(), admin.User.PublicID)
	if !ok {
		t.Fatal("no slot")
	}
	w := adminCall(t, h, "GET", "/api/admin/monitoring/security", "", "", 503, session.Cookie())
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("busy without Retry-After")
	}
	release()
	s.Summaries.Deadline = time.Nanosecond
	adminCall(t, h, "GET", "/api/admin/monitoring/security", "", "", 503, session.Cookie())
	if n := len(apitest.EventsNamed(t, db, "admin.monitoring.viewed")); n != 0 {
		t.Fatalf("%d reads recorded for pages not served", n)
	}
}

// Each Security signal is ok just under its threshold and at its level
// just over it, and its drill-down lists exactly the rows it counted.
func TestAdminMonitoringSecuritySignals(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	m := eventMaker{t, db, f}
	ctx := context.Background()
	security := func() monitoringView { return showMonitoring(t, h, cookie, "security", "?window=1h") }
	level := func(name string, want monitor.Level, value float64) {
		t.Helper()
		got := security().signal(t, name)
		if got.Level != want || got.Value != value {
			t.Fatalf("%s: %v at %v, want %v at %v", name, got.Level, got.Value, want, value)
		}
	}
	attempt := func(email, result string) func(int) map[string]any {
		return func(int) map[string]any { return map[string]any{"email": email, "result": result} }
	}

	// Failed sign-ins: 99 is fine, the 100th is a warning; an hour ago
	// doesn't count.
	m.many(99, "auth.login_attempted", attempt("victim@example.test", "wrong_password"), func(i int) string { return fmt.Sprintf("198.51.100.%d", i%4) }, 10*time.Minute)
	m.many(30, "auth.login_attempted", attempt("old@example.test", "wrong_password"), same("198.51.100.200"), 2*time.Hour)
	level("auth.failed_sign_ins", monitor.LevelOK, 99)
	m.make("auth.login_attempted", map[string]any{"email": "nobody@example.test", "result": "unknown_account"}, "198.51.100.9", 5*time.Minute)
	level("auth.failed_sign_ins", monitor.LevelWarn, 100)
	// Its drill-down finds exactly those 100.
	failed := security().signal(t, "auth.failed_sign_ins")
	query := url.Values{"summary": {"include"}}
	for k, v := range failed.Drill {
		query.Set(k, v)
	}
	if page := indexEvents(t, h, cookie, "?"+query.Encode()); page.Summary == nil || page.Summary.AdminEventCounts == nil || page.Summary.Total != 100 {
		t.Fatalf("drill-down %v: %+v", query, page.Summary)
	}
	// Distributed guessing: victim@ failed from 4 addresses; a fifth makes
	// it a warning. The same /64 is one address.
	level("auth.distributed_guessing", monitor.LevelOK, 0)
	m.make("auth.login_attempted", map[string]any{"email": "victim@example.test", "result": "wrong_password"}, "2001:db8:1:2::1", 5*time.Minute)
	m.make("auth.login_attempted", map[string]any{"email": "victim@example.test", "result": "wrong_password"}, "2001:db8:1:2::ffff", 5*time.Minute)
	level("auth.distributed_guessing", monitor.LevelWarn, 1)

	// Credential stuffing: 9 emails from one address is fine; the 10th is
	// a warning; a success among them is critical.
	stuffer := "203.0.113.66"
	m.many(9, "auth.login_attempted", func(i int) map[string]any {
		return map[string]any{"email": fmt.Sprintf("list%d@example.test", i), "result": "unknown_account"}
	}, same(stuffer), 20*time.Minute)
	level("auth.stuffing_addresses", monitor.LevelOK, 0)
	m.make("auth.login_attempted", map[string]any{"email": "LIST0@example.test", "result": "unknown_account"}, stuffer, 20*time.Minute)
	level("auth.stuffing_addresses", monitor.LevelOK, 0)
	m.make("auth.login_attempted", map[string]any{"email": "list9@example.test", "result": "wrong_password"}, stuffer, 20*time.Minute)
	level("auth.stuffing_addresses", monitor.LevelWarn, 1)
	level("auth.stuffing_successes", monitor.LevelOK, 0)
	m.make("auth.login_attempted", map[string]any{"email": "list3@example.test", "result": "succeeded", "user": "usr_2yEHYLwiwPVhLW"}, stuffer, 15*time.Minute)
	level("auth.stuffing_successes", monitor.LevelCritical, 1)
	v := sections[struct {
		Stuffing []store.StuffingRow `json:"stuffing"`
		Unknown  store.SignInShare   `json:"unknown"`
	}](t, security())
	if len(v.Stuffing) != 1 || v.Stuffing[0].IP != stuffer || v.Stuffing[0].Emails != 10 || v.Stuffing[0].Attempts != 12 || v.Stuffing[0].Successes != 1 {
		t.Fatalf("stuffing list: %+v", v.Stuffing)
	}

	// Unknown accounts: 11 of 114 is fine; 92 more make it half.
	share := security().signal(t, "auth.unknown_account_share")
	if share.Level != monitor.LevelOK || share.Sample != 114 || v.Unknown.Attempts != 114 || v.Unknown.Unknown != 11 {
		t.Fatalf("unknown share: %+v %+v", share, v.Unknown)
	}
	m.many(92, "auth.login_attempted", attempt("ghost@example.test", "unknown_account"), same("192.0.2.77"), 3*time.Minute)
	level("auth.unknown_account_share", monitor.LevelWarn, 50)

	// Sign-ins after failures: 4 wrong passwords then a success is fine;
	// 5 is critical.
	account := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	tries := func(n int) {
		m.many(n, "auth.login_attempted", func(int) map[string]any {
			return map[string]any{"email": account.Email, "user": account.PublicID, "result": "wrong_password"}
		}, same("192.0.2.10"), 30*time.Minute)
		m.make("auth.login_attempted", map[string]any{"email": account.Email, "user": account.PublicID, "result": "mfa_required"}, "192.0.2.10", 25*time.Minute)
	}
	tries(4)
	level("auth.success_after_failures", monitor.LevelOK, 0)
	tries(1)
	level("auth.success_after_failures", monitor.LevelCritical, 1)
	after := sections[struct {
		Rows []store.SuccessAfterFailuresRow `json:"successAfterFailures"`
	}](t, security())
	if len(after.Rows) != 1 || after.Rows[0].Account.ID != account.PublicID || after.Rows[0].Account.Name != account.Name || after.Rows[0].Failures != 5 {
		t.Fatalf("success after failures: %+v", after.Rows)
	}

	// Second factors and password checks: two failures are fine, three a
	// warning.
	for _, c := range []struct{ event, signal string }{
		{"auth.mfa.checked", "auth.second_factor_failures"},
		{"auth.password_checked", "auth.reauthentication_failures"},
	} {
		m.many(2, c.event, func(int) map[string]any { return map[string]any{"user": account.PublicID, "ok": false} }, same("192.0.2.10"), 10*time.Minute)
		m.make(c.event, map[string]any{"user": account.PublicID, "ok": true}, "192.0.2.10", 10*time.Minute)
		level(c.signal, monitor.LevelOK, 0)
		m.make(c.event, map[string]any{"user": account.PublicID, "ok": false}, "192.0.2.10", 10*time.Minute)
		level(c.signal, monitor.LevelWarn, 1)
	}

	// The shared limits: reset reached is critical; sign-ups at half
	// register:global a warning, all of it critical.
	level("auth.reset_limit_reached", monitor.LevelOK, 0)
	// Its challenge level crossed only asks for a CAPTCHA: not reached.
	m.make("rate_limit.reached", map[string]any{"limit": "password-reset:global:challenge", "window": 3600}, "192.0.2.1", 5*time.Minute)
	level("auth.reset_limit_reached", monitor.LevelOK, 0)
	m.make("rate_limit.reached", map[string]any{"limit": "password-reset:global:block", "window": 3600}, "192.0.2.1", 5*time.Minute)
	m.make("rate_limit.reached", map[string]any{"limit": "password-reset:per-ip:block", "window": 3600}, "192.0.2.1", 5*time.Minute)
	level("auth.reset_limit_reached", monitor.LevelCritical, 1)
	half := httpapi.RegisterGlobal.Block.At() / 2
	apitest.Must(factory.RateLimits.State(factory.ForLimit("register:global", "")).State(factory.Exhausted(half-1)).CreateOne(ctx, db, f))(t)
	level("auth.sign_up_limit_used", monitor.LevelOK, float64(half-1)*100/float64(httpapi.RegisterGlobal.Block.At()))
	apitest.Must(db.NewUpdate().Model((*factory.RateLimit)(nil)).Set("hits = ?", half).Where("key = 'register:global'").Exec(ctx))(t)
	level("auth.sign_up_limit_used", monitor.LevelWarn, 50)
	apitest.Must(db.NewUpdate().Model((*factory.RateLimit)(nil)).Set("hits = ?", httpapi.RegisterGlobal.Block.At()+3).Where("key = 'register:global'").Exec(ctx))(t)
	level("auth.sign_up_limit_used", monitor.LevelCritical, float64(httpapi.RegisterGlobal.Block.At()+3)*100/float64(httpapi.RegisterGlobal.Block.At()))
	// Above the challenge level with a provider, only the requests that
	// passed count toward the block level: unsolved ones block nobody.
	apitest.Must(factory.RateLimits.State(factory.ForLimit("register:global:passed", "")).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	through := httpapi.RegisterGlobal.Challenge.At() + 10
	level("auth.sign_up_limit_used", monitor.LevelOK, float64(through)*100/float64(httpapi.RegisterGlobal.Block.At()))

	// The admin door: a wrong password is a warning, a right password
	// from a non-admin critical; a code step is neither.
	m.make("admin.login_attempted", map[string]any{"email": "admin@example.test", "result": "mfa_required"}, "192.0.2.2", time.Hour)
	level("admin.door_failures", monitor.LevelOK, 0)
	m.make("admin.login_attempted", map[string]any{"email": "admin@example.test", "result": "wrong_password"}, "192.0.2.2", 3*time.Hour)
	level("admin.door_failures", monitor.LevelWarn, 1)
	level("admin.door_refusals", monitor.LevelOK, 0)
	m.make("admin.access_refused", map[string]any{"reason": "ip_not_allowed"}, "192.0.2.3", 30*time.Hour)
	level("admin.door_refusals", monitor.LevelOK, 0)
	m.make("admin.login_attempted", map[string]any{"email": "user@example.test", "result": "not_admin"}, "192.0.2.4", 20*time.Hour)
	level("admin.door_refusals", monitor.LevelCritical, 1)
	door := security().signal(t, "admin.door_refusals")
	query = url.Values{}
	for k, val := range door.Drill {
		query.Set(k, val)
	}
	if page := listEvents(t, h, cookie, "?"+query.Encode()); len(page.Data) != 2 {
		t.Fatalf("door drill-down %v: %d rows, want the refusal and the failure", query, len(page.Data))
	}
	recent := sections[struct {
		AdminDoor struct {
			Recent []store.DoorEvent `json:"recent"`
		} `json:"adminDoor"`
	}](t, showMonitoring(t, h, cookie, "security", "?window=7d"))
	if len(recent.AdminDoor.Recent) != 3 || recent.AdminDoor.Recent[0].Outcome != "wrong_password" || recent.AdminDoor.Recent[2].Outcome != "access_refused" {
		t.Fatalf("door events: %+v", recent.AdminDoor.Recent)
	}
}

// A possible takeover: a sensitive change within an hour of a sign-in
// from a network the account hadn't used in 30 days, though it had signed
// in; never one from a known network, nor an account with no history.
func TestAdminMonitoringTakeovers(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	ctx := context.Background()
	user := func() factory.User { return apitest.Must(factory.Users.CreateOne(ctx, db, f))(t) }
	signIn := func(u factory.User, ip string, ago time.Duration) {
		m.make("auth.logged_in", map[string]any{"user": u.PublicID}, ip, ago, func(e *factory.Event) { e.UserID = u.ID })
	}
	change := func(u factory.User, name string, ago time.Duration) {
		m.make(name, map[string]any{"user": u.PublicID}, "", ago, func(e *factory.Event) { e.UserID = u.ID })
	}
	takeovers := func() []store.TakeoverRow {
		v := showMonitoring(t, h, session.Cookie(), "security", "")
		if got := v.signal(t, "auth.possible_takeovers"); got.Level == monitor.LevelOK && got.Value > 0 {
			t.Fatalf("takeovers counted at ok: %+v", got)
		}
		return sections[struct {
			Rows []store.TakeoverRow `json:"takeovers"`
		}](t, v).Rows
	}
	// Taken over: home is 10.1.2.x; then a sign-in from another network
	// and the password changed.
	victim := user()
	signIn(victim, "10.1.2.3", 15*24*time.Hour)
	signIn(victim, "203.0.113.9", 40*time.Minute)
	change(victim, "auth.password_changed", 10*time.Minute)
	// The same network, another address: fine.
	home := user()
	signIn(home, "10.9.9.1", 10*24*time.Hour)
	signIn(home, "10.9.9.200", 30*time.Minute)
	change(home, "auth.mfa.disabled", 20*time.Minute)
	// No sign-in in 30 days to compare with: not counted.
	fresh := user()
	signIn(fresh, "198.51.100.1", 40*24*time.Hour)
	signIn(fresh, "203.0.113.10", 30*time.Minute)
	change(fresh, "auth.recovery_codes.created", 20*time.Minute)
	// A change two hours after the sign-in: not counted.
	late := user()
	signIn(late, "10.3.3.3", 5*24*time.Hour)
	signIn(late, "203.0.113.11", 3*time.Hour)
	change(late, "auth.other_sessions_revoked", 30*time.Minute)

	rows := takeovers()
	if len(rows) != 1 || rows[0].Account.ID != victim.PublicID || rows[0].Change != "auth.password_changed" || rows[0].Network != "203.0.113.0/24" {
		t.Fatalf("takeovers: %+v", rows)
	}
	v := showMonitoring(t, h, session.Cookie(), "security", "")
	if got := v.signal(t, "auth.possible_takeovers"); got.Level != monitor.LevelWarn || got.Value != 1 {
		t.Fatalf("signal: %+v", got)
	}
}

// The Rate limits page: windows reached by limit, the shared limits
// against their caps (critical when reached), subjects at a limit now,
// who reached the most, budgets, refusals, quotas and routes.
func TestAdminMonitoringLimits(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	ctx := context.Background()
	scraper := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	reached := func(limit string, ago time.Duration, userID int64) {
		m.make("rate_limit.reached", map[string]any{"limit": limit + ":block", "window": 900}, "203.0.113.50", ago, func(e *factory.Event) { e.UserID = userID })
	}
	reached("auth:per-ip", 10*time.Minute, 0)
	reached("auth:per-ip", 40*time.Minute, 0)
	reached("api:per-user", 5*time.Minute, scraper.ID)
	reached("api-read:per-user", 2*time.Hour, scraper.ID)
	// A challenge level crossed asks for a CAPTCHA and refuses nobody: not
	// a window reached, and no shared limit reached.
	m.make("rate_limit.reached", map[string]any{"limit": "register:global:challenge", "window": 3600}, "203.0.113.50", 3*time.Minute)
	v := showMonitoring(t, h, session.Cookie(), "limits", "")
	if got := v.signal(t, "limits.shared_reached"); got.Level != monitor.LevelOK {
		t.Fatalf("shared reached: %+v", got)
	}
	if got := v.signal(t, "limits.windows_reached"); got.Value != 3 {
		t.Fatalf("windows reached in the hour: %+v", got)
	}
	reached("invitation:global", 20*time.Minute, 0)
	apitest.Must(factory.RateLimits.State(factory.ForLimit("invitation:global", "")).State(factory.Exhausted(httpapi.InvitationGlobal.Block.At()+1)).CreateOne(ctx, db, f))(t)
	// Two addresses at auth:per-ip now, one under it, one whose window ended.
	for i, hits := range []int{httpapi.AuthPerIP.Block.At(), httpapi.AuthPerIP.Block.At() + 5, httpapi.AuthPerIP.Block.At() - 1} {
		apitest.Must(factory.RateLimits.State(factory.ForLimit("auth:per-ip", fmt.Sprintf("192.0.2.%d", i))).State(factory.Exhausted(hits)).CreateOne(ctx, db, f))(t)
	}
	apitest.Must(factory.RateLimits.State(factory.ForLimit("auth:per-ip", "192.0.2.99")).State(factory.Exhausted(httpapi.AuthPerIP.Block.At())).State(func(l *factory.RateLimit) {
		l.ResetsAt = time.Now().Add(-time.Minute)
	}).CreateOne(ctx, db, f))(t)
	// Refusals, quotas and a route's 429s, from the minutes.
	minute := time.Now().UTC().Truncate(time.Minute).Add(-5 * time.Minute)
	server := factory.ServerID(f)
	apitest.Must(factory.MetricMinutes.Sequence(func(i int, mm *factory.MetricMinute) {
		mm.Server, mm.Minute = server, minute
		mm.Metric = []string{"rate_limit_refused_total", "rate_limit_refused_total", "quota_refused_total"}[i]
		mm.Labels = []string{`limit="auth:per-ip:block"`, `limit="invitation:global:block"`, `quota="workspace_projects"`}[i]
		mm.Count, mm.Sum, mm.Buckets = []int64{40, 7, 2}[i], 0, nil
	}).Create(ctx, db, f, 3))(t)
	apitest.Must(factory.RequestMinutes.State(func(rm *factory.RequestMinute) {
		rm.Server, rm.Minute, rm.Route, rm.Requests, rm.Status4xx, rm.Status429 = server, minute, "POST /api/auth/login", 100, 40, 40
	}).CreateOne(ctx, db, f))(t)

	v = showMonitoring(t, h, session.Cookie(), "limits", "")
	if got := v.signal(t, "limits.shared_reached"); got.Level != monitor.LevelCritical || got.Value != 1 {
		t.Fatalf("shared reached: %+v", got)
	}
	type blocked struct {
		Name     string `json:"name"`
		Subjects *int64 `json:"subjects"`
	}
	got := sections[struct {
		Reached      store.MonitorSeries    `json:"reached"`
		ByLimit      []store.LimitReached   `json:"byLimit"`
		Refused      []store.MetricTotal    `json:"refused"`
		Shared       []monitor.SharedLimit  `json:"shared"`
		Blocked      []blocked              `json:"blocked"`
		TopAccounts  []store.AccountWindows `json:"topAccounts"`
		TopAddresses []store.AddressCount   `json:"topAddresses"`
		Budgets      []store.AccountWindows `json:"budgets"`
		Quotas       []store.MetricTotal    `json:"quotas"`
		Routes       []store.RouteRefusals  `json:"routes"`
	}](t, v)
	if len(got.ByLimit) != 4 || got.ByLimit[0].Limit != "auth:per-ip" || got.ByLimit[0].Windows != 2 {
		t.Fatalf("by limit: %+v", got.ByLimit)
	}
	total := 0
	for _, b := range got.Reached.Buckets {
		for _, n := range b.Counts {
			total += n
		}
	}
	if total != 5 || len(got.Reached.Series) != 4 || got.Reached.Series[0] != "auth:per-ip" {
		t.Fatalf("reached chart: %d in %v", total, got.Reached.Series)
	}
	var invitations *monitor.SharedLimit
	for i := range got.Shared {
		if got.Shared[i].Name == "invitation:global" {
			invitations = &got.Shared[i]
		}
	}
	if len(got.Shared) < 4 || invitations == nil || invitations.Hits != int64(httpapi.InvitationGlobal.Block.At()+1) || invitations.Max != httpapi.InvitationGlobal.Block.At() || invitations.Reached != 1 || invitations.ResetsAt == nil {
		t.Fatalf("shared: %+v", got.Shared)
	}
	for _, b := range got.Blocked {
		switch b.Name {
		case "auth:per-ip":
			if b.Subjects == nil || *b.Subjects != 2 {
				t.Fatalf("blocked auth:per-ip: %+v", b)
			}
		case "api-read:per-user":
			if b.Subjects != nil {
				t.Fatal("a limit in memory counted")
			}
		case "invitation:global":
			t.Fatal("a shared limit listed as blocking subjects")
		}
	}
	if len(got.TopAccounts) != 1 || got.TopAccounts[0].Account.ID != scraper.PublicID || got.TopAccounts[0].Windows != 2 {
		t.Fatalf("top accounts: %+v", got.TopAccounts)
	}
	if len(got.Budgets) != 2 || got.Budgets[0].Account.Name != scraper.Name {
		t.Fatalf("budgets: %+v", got.Budgets)
	}
	if len(got.TopAddresses) != 1 || got.TopAddresses[0].IP != "203.0.113.50" || got.TopAddresses[0].Count != 5 {
		t.Fatalf("top addresses: %+v", got.TopAddresses)
	}
	if len(got.Refused) != 2 || got.Refused[0].Label != "auth:per-ip:block" || got.Refused[0].Count != 40 {
		t.Fatalf("refused: %+v", got.Refused)
	}
	if len(got.Quotas) != 1 || got.Quotas[0].Label != "workspace_projects" || got.Quotas[0].Count != 2 {
		t.Fatalf("quotas: %+v", got.Quotas)
	}
	if len(got.Routes) != 1 || got.Routes[0].Route != "POST /api/auth/login" || got.Routes[0].Refused != 40 {
		t.Fatalf("routes: %+v", got.Routes)
	}
}

// The Overview: live servers and versions, requests, server errors and
// latency from the minutes (and the hours over 7 days), the mail queue,
// grouped errors, sign-ups and active accounts, each with its level.
func TestAdminMonitoringOverview(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	ctx := context.Background()
	m := eventMaker{t, db, f}
	overview := func(query string) monitoringView { return showMonitoring(t, h, session.Cookie(), "overview", query) }

	// Two live servers on two versions, one gone.
	apitest.Must(factory.ServerInstances.Sequence(func(i int, si *factory.ServerInstance) {
		si.Version = []string{"abc123def456", "def456abc123", "abc123def456"}[i]
		si.LastSeenAt = time.Now().Add(-[]time.Duration{10 * time.Second, 30 * time.Second, 10 * time.Minute}[i])
	}).Create(ctx, db, f, 3))(t)
	// 1,000 requests in the last hour, 20 of them 5xx, 900 under 50 ms
	// and 100 between 1 and 2.5 s, so the 95th percentile is 1.75 s.
	server := factory.ServerID(f)
	minute := time.Now().UTC().Truncate(time.Minute)
	apitest.Must(factory.RequestMinutes.Sequence(func(i int, rm *factory.RequestMinute) {
		rm.Server, rm.Route, rm.Minute = server, "GET /api/auth/me", minute.Add(-time.Duration(i+2)*time.Minute)
		rm.Requests, rm.Status5xx = 500, 10
		rm.DurationBuckets = []int64{0, 0, 0, 450, 0, 0, 0, 0, 50, 0, 0, 0}
	}).Create(ctx, db, f, 2))(t)
	// Over a week, an hour already summed, five days ago.
	apitest.Must(factory.RequestHours.State(func(rh *factory.RequestHour) {
		rh.Hour, rh.Route, rh.Requests, rh.Status5xx = minute.Truncate(time.Hour).Add(-120*time.Hour), "GET /api/auth/me", 7000, 0
		rh.DurationBuckets = []int64{7000, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	}).CreateOne(ctx, db, f))(t)
	// A delivery waiting 15 minutes, and one failed today.
	waiting := m.make("auth.password_reset.requested", map[string]any{"email": "a@example.test"}, "", 15*time.Minute)
	apitest.Must(factory.DeliveryOf(waiting.ID, "mail.password_reset").CreateOne(ctx, db, f))(t)
	failed := m.make("auth.password_reset.requested", map[string]any{"email": "b@example.test"}, "", 3*time.Hour)
	apitest.Must(factory.DeliveryOf(failed.ID, "mail.password_reset").State(func(d *factory.EventDelivery) { d.FailedAt = time.Now().Add(-2 * time.Hour) }).CreateOne(ctx, db, f))(t)
	// Error groups: a new panic, an old open one, an old resolved one.
	apitest.Must(factory.AppErrors.Sequence(func(i int, e *factory.AppError) {
		e.Source = []string{"panic", "request", "request"}[i]
		e.FirstSeenAt = time.Now().Add(-[]time.Duration{time.Hour, 10 * 24 * time.Hour, 10 * 24 * time.Hour}[i])
		e.LastSeenAt = time.Now().Add(-time.Minute)
		if i == 2 {
			e.ResolvedAt = time.Now()
		}
	}).Create(ctx, db, f, 3))(t)
	// Two sign-ups today; three accounts seen in the last day.
	m.many(2, "auth.registered", func(int) map[string]any { return map[string]any{"user": "usr_2yEHYLwiwPVhLW"} }, same("192.0.2.5"), time.Hour)
	seen := time.Now().Add(-3 * time.Hour)
	apitest.Must(factory.Users.State(func(u *factory.User) { u.LastSeenAt = &seen }).Create(ctx, db, f, 3))(t)
	old := time.Now().Add(-3 * 24 * time.Hour)
	apitest.Must(factory.Users.State(func(u *factory.User) { u.LastSeenAt = &old }).Create(ctx, db, f, 2))(t)

	v := overview("")
	for _, c := range []struct {
		name  string
		level monitor.Level
		check func(float64) bool
	}{
		{"servers.live", monitor.LevelOK, func(x float64) bool { return x == 2 }},
		{"servers.versions", monitor.LevelWarn, func(x float64) bool { return x == 2 }},
		{"http.requests_per_minute", monitor.LevelOK, func(x float64) bool { return x > 15 && x < 17 }},
		{"http.server_error_share", monitor.LevelWarn, func(x float64) bool { return x == 2 }},
		{"http.latency_p95", monitor.LevelWarn, func(x float64) bool { return x == 1750 }},
		{"mail.oldest_pending", monitor.LevelCritical, func(x float64) bool { return x >= 15 && x < 16 }},
		{"mail.failed_deliveries", monitor.LevelWarn, func(x float64) bool { return x == 1 }},
		{"errors.new", monitor.LevelWarn, func(x float64) bool { return x == 1 }},
		{"errors.new_panics", monitor.LevelCritical, func(x float64) bool { return x == 1 }},
		{"errors.open", monitor.LevelOK, func(x float64) bool { return x == 2 }},
		{"usage.sign_ups", monitor.LevelOK, func(x float64) bool { return x == 2 }},
		{"usage.active_accounts", monitor.LevelOK, func(x float64) bool { return x == 3 }},
		{"auth.failed_sign_ins", monitor.LevelOK, func(x float64) bool { return x == 0 }},
	} {
		got := v.signal(t, c.name)
		if got.Level != c.level || !c.check(got.Value) {
			t.Errorf("%s: %v at %v", c.name, got.Level, got.Value)
		}
	}
	tiles := sections[struct {
		Tiles []monitor.Tile `json:"tiles"`
	}](t, v).Tiles
	if len(tiles) != 10 || tiles[0].ID != "servers" || tiles[0].Series != nil {
		t.Fatalf("tiles: %+v", tiles)
	}
	total := func(id string, tiles []monitor.Tile) float64 {
		var n float64
		for _, tile := range tiles {
			if tile.ID == id {
				for _, p := range tile.Series.Points {
					n += p.Value
				}
			}
		}
		return n
	}
	if n := total("requests", tiles); n != 1000 {
		t.Fatalf("requests sparkline: %v", n)
	}
	if n := total("sign_ups", tiles); n != 2 {
		t.Fatalf("sign-ups sparkline: %v", n)
	}
	// Over a week, the summed hour joins the minutes.
	week := sections[struct {
		Tiles []monitor.Tile `json:"tiles"`
	}](t, overview("?window=7d")).Tiles
	if n := total("requests", week); n != 8000 {
		t.Fatalf("a week's requests: %v", n)
	}
}
