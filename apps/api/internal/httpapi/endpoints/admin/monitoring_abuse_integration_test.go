package admin_test

import (
	"context"
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/monitor"
	"lewcontext/api/internal/store"
	"net/http"
	"testing"
	"time"
)

// The Monitoring pages of part 2 (docs/api/monitoring.md): IP addresses,
// one address, Accounts, Workspaces and Usage. Each signal just under and
// at its level, and the lists behind them.

// levelOf checks a signal's level and value on a page.
func levelOf(t *testing.T, v monitoringView, name string, want monitor.Level, value float64) {
	t.Helper()
	got := v.signal(t, name)
	if got.Level != want || got.Value != value {
		t.Fatalf("%s: %v at %v, want %v at %v", name, got.Level, got.Value, want, value)
	}
}

// olderThanADay makes a fake account a month old, so it's never "new".
func olderThanADay(u *factory.User) { u.CreatedAt = time.Now().AddDate(0, -1, 0) }

// ipsSections is the IP addresses page's sections.
type ipsSections struct {
	Addresses []store.AddressRow       `json:"addresses"`
	Clusters  []store.SignUpClusterRow `json:"clusters"`
}

// Sign-up clusters (an IPv6 /64 as one address), the busiest address's
// share, and the top addresses both ways; the admin's own reads don't
// count.
func TestAdminMonitoringIPs(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	ips := func() monitoringView { return showMonitoring(t, h, session.Cookie(), "ips", "") }
	registered := func(i int) map[string]any { return map[string]any{"user": fmt.Sprintf("usr_%014d", i)} }

	// 4 accounts from one address is fine; the 5th a warning.
	m.many(4, "auth.registered", registered, same("198.51.100.77"), time.Hour)
	levelOf(t, ips(), "ips.sign_up_cluster", monitor.LevelOK, 4)
	m.make("auth.registered", registered(4), "198.51.100.77", 2*time.Hour)
	levelOf(t, ips(), "ips.sign_up_cluster", monitor.LevelWarn, 5)
	// 20 from one /64, each address different: critical.
	m.many(20, "auth.registered", registered, func(i int) string { return fmt.Sprintf("2001:db8:5:5::%x", i+1) }, 30*time.Minute)
	v := ips()
	levelOf(t, v, "ips.sign_up_cluster", monitor.LevelCritical, 20)
	page := sections[ipsSections](t, v)
	if len(page.Clusters) != 2 || page.Clusters[0].IP != "2001:db8:5:5::/64" || page.Clusters[0].Accounts != 20 || page.Clusters[1].Accounts != 5 {
		t.Fatalf("clusters: %+v", page.Clusters)
	}
	// Both ways: the /64 as one row, and each of its addresses.
	var network, single int
	for _, a := range page.Addresses {
		if a.Network && a.IP == "2001:db8:5:5::/64" && a.SignUps == 20 && a.Events == 20 {
			network++
		}
		if !a.Network && a.IP == "2001:db8:5:5::1" && a.SignUps == 1 {
			single++
		}
		if a.IP == "192.0.2.1" || a.IP == "127.0.0.1" {
			t.Fatalf("the admin's own reads counted: %+v", a)
		}
	}
	if network != 1 || single != 1 {
		t.Fatalf("addresses: %+v", page.Addresses)
	}

	// The busiest address's share: 25 events is too few to say; 100 more
	// from one address make it 80% (a proxy's client IP unset).
	levelOf(t, v, "ips.busiest_share", monitor.LevelOK, 20)
	m.many(100, "auth.logged_in", func(int) map[string]any { return map[string]any{"user": "usr_00000000000001"} }, same("10.0.0.1"), 10*time.Minute)
	levelOf(t, ips(), "ips.busiest_share", monitor.LevelWarn, 80)
	// Failed sign-ins and limits by address; the top list keeps the
	// busiest address on top.
	m.many(3, "auth.login_attempted", func(int) map[string]any { return map[string]any{"email": "a@example.test", "result": "wrong_password"} }, same("203.0.113.5"), 5*time.Minute)
	m.make("rate_limit.reached", map[string]any{"limit": "auth:per-ip:block", "window": 900}, "203.0.113.5", 5*time.Minute)
	page = sections[ipsSections](t, ips())
	for _, a := range page.Addresses {
		if a.IP == "203.0.113.5" && a.Network && (a.FailedSignIns != 3 || a.LimitsReached != 1 || a.Events != 4) {
			t.Fatalf("203.0.113.5: %+v", a)
		}
	}
	if page.Addresses[0].IP != "10.0.0.1" || !page.Addresses[0].Network {
		t.Fatalf("busiest first: %+v", page.Addresses[0])
	}
}

// One address's page: what it did in the window (counts by name, the
// accounts seen from it, emails tried with no account as a count, limits),
// an IPv6 address's /64 and the /64's addresses; recorded as
// admin.ip.viewed with the address and window; anything but an address or
// a /64 is 404, and nothing is recorded for a refusal.
func TestAdminMonitoringAddress(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	admin, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	m := eventMaker{t, db, f}
	u := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	ofU := func(e *factory.Event) { e.UserID, e.ActorID = u.ID, u.ID }
	ip := "203.0.113.5"
	m.make("auth.registered", map[string]any{"user": u.PublicID, "email": u.Email}, ip, 3*time.Hour, ofU)
	m.make("auth.login_attempted", map[string]any{"user": u.PublicID, "email": u.Email, "result": "wrong_password"}, ip, 2*time.Hour)
	m.make("auth.logged_in", map[string]any{"user": u.PublicID}, ip, time.Hour, ofU)
	for _, email := range []string{"x@example.test", "y@example.test", "X@example.test"} {
		m.make("auth.login_attempted", map[string]any{"email": email, "result": "unknown_account"}, ip, 30*time.Minute)
	}
	m.make("rate_limit.reached", map[string]any{"limit": "auth:per-ip:block", "window": 900}, ip, 20*time.Minute)
	m.make("auth.logged_in", map[string]any{"user": u.PublicID}, ip, 48*time.Hour, ofU)
	m.make("auth.logged_in", map[string]any{"user": u.PublicID}, "203.0.113.6", time.Hour, ofU)

	show := func(path string) monitor.AddressView {
		t.Helper()
		w := adminCall(t, h, "GET", "/api/admin/ips/"+path, "", "", 200, cookie)
		return decodeAdmin[monitor.AddressView](t, w.Body.Bytes())
	}
	v := show(ip)
	if v.IP != ip || v.Network != "" || v.Events != 7 || v.FirstSeen == nil || v.LastSeen == nil || v.Window.Name != "24h" {
		t.Fatalf("address: %+v", v)
	}
	if len(v.Accounts) != 1 || v.Accounts[0].Account.ID != u.PublicID || v.Accounts[0].Account.Name != u.Name ||
		v.Accounts[0].SignedIn != 1 || v.Accounts[0].Registered != 1 || v.Accounts[0].Failed != 1 {
		t.Fatalf("accounts: %+v", v.Accounts)
	}
	if v.UnknownEmails != 2 || len(v.Limits) != 1 || v.Limits[0].Limit != "auth:per-ip" || v.Limits[0].Windows != 1 {
		t.Fatalf("unknown emails %d, limits %+v", v.UnknownEmails, v.Limits)
	}
	if v.ByName[0].Name != "auth.login_attempted" || v.ByName[0].Count != 4 {
		t.Fatalf("by name: %+v", v.ByName)
	}
	recorded := apitest.EventsNamed(t, db, "admin.ip.viewed")
	if len(recorded) != 1 || recorded[0].Payload["address"] != ip || recorded[0].Payload["window"] != "24h" ||
		recorded[0].Payload["admin"] != admin.User.PublicID || recorded[0].UserID.Valid {
		t.Fatalf("recorded: %+v", recorded)
	}
	// A week reaches the sign-in two days ago.
	if week := show(ip + "?window=7d"); week.Events != 8 {
		t.Fatalf("7d: %d events", week.Events)
	}
	// An IPv6 address offers its /64; the /64 (its slash escaped) lists
	// its addresses.
	m.make("auth.logged_in", map[string]any{"user": u.PublicID}, "2001:db8:7:7::1", time.Hour, ofU)
	m.make("auth.logged_in", map[string]any{"user": u.PublicID}, "2001:db8:7:7::2", time.Hour, ofU)
	if one := show("2001:db8:7:7::1"); one.Network != "2001:db8:7:7::/64" || one.Events != 1 {
		t.Fatalf("IPv6 address: %+v", one)
	}
	if net := show("2001:db8:7:7::%2F64"); net.IP != "2001:db8:7:7::/64" || net.Events != 2 || len(net.Addresses) != 2 {
		t.Fatalf("IPv6 /64: %+v", net)
	}
	// Nothing from an address: zero, not an error.
	if none := show("192.0.2.200"); none.Events != 0 || none.FirstSeen != nil {
		t.Fatalf("an unseen address: %+v", none)
	}
	before := len(apitest.EventsNamed(t, db, "admin.ip.viewed"))
	for _, path := range []string{"not-an-ip", "203.0.113.0%2F24", "2001:db8::%2F48", "fe80::1%25eth0"} {
		adminCall(t, h, "GET", "/api/admin/ips/"+path, "", "", 404, cookie)
	}
	adminCall(t, h, "GET", "/api/admin/ips/"+ip+"?window=30d", "", "", 400, cookie)
	adminCall(t, h, "GET", "/api/admin/ips/"+ip+"?q=1", "", "", 400, cookie)
	adminCall(t, h, "GET", "/api/admin/ips/"+ip, "", "", 401)
	if n := len(apitest.EventsNamed(t, db, "admin.ip.viewed")); n != before {
		t.Fatalf("refusals recorded %d reads", n-before)
	}
	// Busy: nothing recorded.
	release, ok := s.Summaries.Acquire(ctx, admin.User.PublicID)
	if !ok {
		t.Fatal("no slot")
	}
	adminCall(t, h, "GET", "/api/admin/ips/"+ip, "", "", http.StatusServiceUnavailable, cookie)
	release()
	if n := len(apitest.EventsNamed(t, db, "admin.ip.viewed")); n != before {
		t.Fatal("a busy read was recorded")
	}
}

// accountsSections is the Accounts page's sections.
type accountsSections struct {
	Review     []store.AccountReview `json:"review"`
	TopWriters []store.AccountCount  `json:"topWriters"`
	TopOwners  []store.AccountCount  `json:"topOwners"`
}

// Each account signal just under and at its threshold, and the review
// list with every signal an account tripped.
func TestAdminMonitoringAccounts(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	accounts := func() monitoringView { return showMonitoring(t, h, session.Cookie(), "accounts", "") }
	user := func(states ...func(*factory.User)) factory.User {
		return apitest.Must(factory.Users.State(olderThanADay).Sequence(func(_ int, u *factory.User) {
			for _, st := range states {
				st(u)
			}
		}).CreateOne(ctx, db, f))(t)
	}
	by := func(u factory.User) func(*factory.Event) { return func(e *factory.Event) { e.ActorID = u.ID } }
	acts := func(u factory.User, n int, name string, payload func(i int) map[string]any, ago time.Duration) {
		t.Helper()
		if n == 0 {
			return
		}
		apitest.Must(factory.Events.Sequence(func(i int, e *factory.Event) {
			e.Name, e.ActorID, e.Payload, e.CreatedAt = name, u.ID, payload(i), time.Now().Add(-ago)
		}).Create(ctx, db, f, n))(t)
	}
	plain := func(int) map[string]any { return map[string]any{"seeded": true} }

	// Workspaces: 4 in a day is fine, 5 a warning.
	maker := user()
	acts(maker, 4, "workspace.created", func(i int) map[string]any {
		return map[string]any{"workspace": fmt.Sprintf("wsp_%014d", i), "name": "W"}
	}, time.Hour)
	levelOf(t, accounts(), "accounts.workspace_sprees", monitor.LevelOK, 0)
	m.make("workspace.created", map[string]any{"workspace": "wsp_00000000000099", "name": "W"}, "", time.Hour, by(maker))
	levelOf(t, accounts(), "accounts.workspace_sprees", monitor.LevelWarn, 1)

	// Invitations: 25 to 10 addresses isn't a spree; 19 to 19 isn't
	// either; the 20th to a 20th address is.
	invite := func(emails int) func(int) map[string]any {
		return func(i int) map[string]any {
			return map[string]any{"invitation": fmt.Sprintf("inv_%014d", i), "email": fmt.Sprintf("to%d@example.test", i%emails)}
		}
	}
	scattered := user()
	acts(scattered, 25, "workspace.invitation.created", invite(10), time.Hour)
	inviter := user()
	acts(inviter, 19, "workspace.invitation.created", invite(19), time.Hour)
	levelOf(t, accounts(), "accounts.invitation_sprees", monitor.LevelOK, 0)
	m.make("workspace.invitation.created", map[string]any{"invitation": "inv_00000000000777", "email": "last@example.test"}, "", time.Hour, by(inviter))
	levelOf(t, accounts(), "accounts.invitation_sprees", monitor.LevelWarn, 1)

	// Acceptance over 7 days: 2 of 20 accepted (10%) is fine, 1 of 20 not.
	// The two above, none of theirs accepted, are already counted.
	sendAndAccept := func(u factory.User, prefix string, accepted int) {
		acts(u, 20, "workspace.invitation.created", func(i int) map[string]any {
			return map[string]any{"invitation": fmt.Sprintf("inv_%s%012d", prefix, i), "email": fmt.Sprintf("%s%d@example.test", prefix, i)}
		}, 3*24*time.Hour)
		acts(u, accepted, "workspace.invitation.accepted", func(i int) map[string]any {
			return map[string]any{"invitation": fmt.Sprintf("inv_%s%012d", prefix, i)}
		}, 2*24*time.Hour)
	}
	sendAndAccept(user(), "ok", 2)
	levelOf(t, accounts(), "accounts.low_acceptance", monitor.LevelWarn, 2)
	ignored := user()
	sendAndAccept(ignored, "no", 1)
	levelOf(t, accounts(), "accounts.low_acceptance", monitor.LevelWarn, 3)

	// Projects: 199 in a day is fine, 200 a warning.
	filler := user()
	acts(filler, 199, "project.created", plain, 2*time.Hour)
	levelOf(t, accounts(), "accounts.project_sprees", monitor.LevelOK, 0)
	acts(filler, 1, "project.created", plain, time.Hour)
	levelOf(t, accounts(), "accounts.project_sprees", monitor.LevelWarn, 1)

	// Links in names: a new account named with a link, and an owner whose
	// workspace's name holds one, both having named something today.
	spammer := apitest.Must(factory.Users.State(func(u *factory.User) {
		u.Name, u.CreatedAt = "Visit http://cheap.example", time.Now().Add(-2*time.Hour)
	}).CreateOne(ctx, db, f))(t)
	m.make("auth.registered", map[string]any{"user": spammer.PublicID}, "", 2*time.Hour, func(e *factory.Event) { e.ActorID, e.UserID = spammer.ID, spammer.ID })
	owner := user()
	spam := apitest.Must(factory.WorkspaceFor(ctx, db, f, owner.ID, factory.Named("Free money at www.cheap.example")))(t)
	m.make("workspace.renamed", map[string]any{"workspace": spam.PublicID}, "", time.Hour, by(owner))
	// Named today too, but nothing links: not flagged.
	quiet := user()
	m.make("workspace.created", map[string]any{"workspace": "wsp_00000000000123"}, "", time.Hour, by(quiet))
	levelOf(t, accounts(), "accounts.links_in_names", monitor.LevelWarn, 2)
	// New and busy: the spammer, made 2 hours ago.
	levelOf(t, accounts(), "accounts.new_and_busy", monitor.LevelWarn, 1)

	// A per-user limit reached: counted, with no level.
	limited := user()
	m.make("rate_limit.reached", map[string]any{"limit": "project:per-user:block", "window": 3600}, "", time.Hour, func(e *factory.Event) { e.UserID = limited.ID })
	levelOf(t, accounts(), "accounts.limits_reached", monitor.LevelOK, 1)

	// Failed checks: 2 is fine; a third (a code this time) a warning.
	guessed := user()
	check := func(name string, ok bool) {
		m.make(name, map[string]any{"user": guessed.PublicID, "ok": ok}, "192.0.2.10", 30*time.Minute)
	}
	check("auth.password_checked", false)
	check("auth.password_checked", false)
	check("auth.mfa.checked", true)
	levelOf(t, accounts(), "accounts.failed_checks", monitor.LevelOK, 0)
	check("auth.mfa.checked", false)
	v := accounts()
	levelOf(t, v, "accounts.failed_checks", monitor.LevelWarn, 1)

	// The review list: the new account first among those with one signal,
	// each with what it tripped.
	page := sections[accountsSections](t, v)
	find := func(u factory.User) store.AccountReview {
		t.Helper()
		for _, r := range page.Review {
			if r.Account.ID == u.PublicID {
				return r
			}
		}
		t.Fatalf("%s isn't on the review list: %+v", u.Name, page.Review)
		return store.AccountReview{}
	}
	if r := find(spammer); !r.New || len(r.Signals) != 1 || r.Signals[0].Signal != store.ReviewLinks || r.Signals[0].Workspace != nil {
		t.Fatalf("spammer: %+v", r)
	}
	if r := find(owner); r.New || r.Signals[0].Workspace == nil || r.Signals[0].Workspace.ID != spam.PublicID {
		t.Fatalf("owner: %+v", r)
	}
	if r := find(ignored); r.Signals[0].Signal != store.ReviewAcceptance || r.Signals[0].Count != 1 || r.Signals[0].Of != 20 {
		t.Fatalf("low acceptance: %+v", r)
	}
	if r := find(inviter); r.Signals[0].Count != 20 || r.Signals[0].Of != 20 {
		t.Fatalf("inviter: %+v", r)
	}
	if r := find(filler); r.Signals[0].Count != 200 {
		t.Fatalf("filler: %+v", r)
	}
	if r := find(guessed); r.Signals[0].Signal != store.ReviewChecks || r.Signals[0].Count != 3 {
		t.Fatalf("checks: %+v", r)
	}
	find(maker)
	find(limited)
	// Most signals first (the inviter: a spree and nothing accepted), then
	// the new account.
	if page.Review[0].Account.ID != inviter.PublicID || len(page.Review[0].Signals) != 2 || page.Review[1].Account.ID != spammer.PublicID {
		t.Fatalf("order: %+v", page.Review[:2])
	}
	if r := find(scattered); r.Signals[0].Signal != store.ReviewAcceptance {
		t.Fatalf("scattered: %+v", r)
	}
	if len(page.Review) != 9 {
		t.Fatalf("%d on the review list", len(page.Review))
	}
	// The busiest writers: projects and workspaces, never sign-ins.
	if len(page.TopWriters) == 0 || page.TopWriters[0].Account.ID != filler.PublicID || page.TopWriters[0].Count != 200 {
		t.Fatalf("writers: %+v", page.TopWriters)
	}
	// Owners, of those creating a workspace in the window: the quiet one
	// owns none; the maker's events name no real workspace.
	if len(page.TopOwners) != 0 {
		t.Fatalf("owners: %+v", page.TopOwners)
	}
	// Outside the window (25 hours ago) nothing counts.
	old := user()
	acts(old, 6, "workspace.created", plain, 25*time.Hour)
	levelOf(t, accounts(), "accounts.workspace_sprees", monitor.LevelWarn, 1)
}

// workspacesSections is the Workspaces page's sections.
type workspacesSections struct {
	Quotas struct {
		Caps       monitor.Quotas     `json:"caps"`
		Workspaces []store.QuotaRow   `json:"workspaces"`
		Owners     []store.OwnerQuota `json:"owners"`
	} `json:"quotas"`
	Rows struct {
		Tables     []store.WorkspaceTable `json:"tables"`
		Workspaces []store.WorkspaceRows  `json:"workspaces"`
	} `json:"rows"`
	Growth      []store.WorkspaceGrowth      `json:"growth"`
	Invitations []store.WorkspaceInvitations `json:"invitations"`
	Throwaways  []store.ThrowawayRow         `json:"throwaways"`
	Totals      struct {
		Estimate int64               `json:"estimate"`
		Series   store.MonitorSeries `json:"series"`
	} `json:"totals"`
}

// Quota headroom (80% a warning, full critical), rows per workspace
// across the registry, growth, invitations and throwaway workspaces; only
// workspaces with an event in the window are counted.
func TestAdminMonitoringWorkspaces(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	workspaces := func() monitoringView { return showMonitoring(t, h, session.Cookie(), "workspaces", "") }
	owner := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	in := func(w factory.Workspace) func(*factory.Event) {
		return func(e *factory.Event) { e.WorkspaceID, e.ActorID = w.ID, owner.ID }
	}
	touch := func(w factory.Workspace, name string) {
		m.make(name, map[string]any{"workspace": w.PublicID}, "", 30*time.Minute, in(w))
	}

	// 799 projects is under 80%; touched today, it's counted.
	full := apitest.Must(factory.WorkspaceFor(ctx, db, f, owner.ID))(t)
	apitest.Must(factory.ProjectsIn(full.ID).Create(ctx, db, f, 799))(t)
	touch(full, "project.created")
	levelOf(t, workspaces(), "workspaces.quota_used", monitor.LevelOK, 79.9)
	apitest.Must(factory.ProjectsIn(full.ID).CreateOne(ctx, db, f))(t)
	levelOf(t, workspaces(), "workspaces.quota_used", monitor.LevelWarn, 80)
	// 50 live invitations: that quota is full.
	inviting := apitest.Must(factory.WorkspaceFor(ctx, db, f, owner.ID))(t)
	apitest.Must(factory.InvitationsTo(inviting.ID).Create(ctx, db, f, 50))(t)
	touch(inviting, "workspace.invitation.created")
	// A full workspace nobody touched today isn't counted.
	idle := apitest.Must(factory.WorkspaceFor(ctx, db, f, owner.ID))(t)
	apitest.Must(factory.InvitationsTo(idle.ID).Create(ctx, db, f, 50))(t)
	v := workspaces()
	levelOf(t, v, "workspaces.quota_used", monitor.LevelCritical, 100)
	page := sections[workspacesSections](t, v)
	if len(page.Quotas.Workspaces) != 2 || page.Quotas.Workspaces[0].Workspace.ID != inviting.PublicID || page.Quotas.Workspaces[0].Invitations != 50 ||
		page.Quotas.Workspaces[1].Projects != 800 || page.Quotas.Caps.Projects != store.MaxWorkspaceProjects {
		t.Fatalf("quotas: %+v", page.Quotas)
	}
	// Owners at 80% of 20 owned workspaces: 16, one of them touched.
	if len(page.Quotas.Owners) != 0 {
		t.Fatalf("owners at 3: %+v", page.Quotas.Owners)
	}
	for range 13 {
		apitest.Must(factory.WorkspaceFor(ctx, db, f, owner.ID))(t)
	}
	page = sections[workspacesSections](t, workspaces())
	if len(page.Quotas.Owners) != 1 || page.Quotas.Owners[0].Account.ID != owner.PublicID || page.Quotas.Owners[0].Owned != 16 || page.Quotas.Owners[0].Used != 80 {
		t.Fatalf("owners: %+v", page.Quotas.Owners)
	}

	// Rows across the registry, in its order: projects, members,
	// invitations, events.
	if len(page.Rows.Tables) != len(store.WorkspaceTables) || page.Rows.Workspaces[0].Workspace.ID != full.PublicID {
		t.Fatalf("rows: %+v", page.Rows)
	}
	want := []int64{800, 1, 0, 1}
	for i, n := range page.Rows.Workspaces[0].Counts {
		if n != want[i] {
			t.Fatalf("%s: %d rows, want %d (%+v)", page.Rows.Tables[i].Name, n, want[i], page.Rows.Workspaces[0])
		}
	}
	if page.Rows.Workspaces[0].Total != 802 || len(page.Rows.Workspaces) != 2 {
		t.Fatalf("rows: %+v", page.Rows.Workspaces)
	}
	levelOf(t, workspaces(), "workspaces.largest", monitor.LevelOK, 802)

	// Growth: what each workspace created, and its events.
	if len(page.Growth) != 2 || page.Growth[0].Created != 1 || page.Growth[0].Events != 1 {
		t.Fatalf("growth: %+v", page.Growth)
	}

	// Invitations over 7 days: sent, accepted, revoked, expired.
	hiring := apitest.Must(factory.WorkspaceFor(ctx, db, f, owner.ID))(t)
	for _, name := range []string{"workspace.invitation.created", "workspace.invitation.created", "workspace.invitation.created", "workspace.invitation.accepted", "workspace.invitation.revoked"} {
		m.make(name, map[string]any{"workspace": hiring.PublicID, "invitation": "inv_00000000000001"}, "", 3*24*time.Hour, in(hiring))
	}
	apitest.Must(factory.InvitationsTo(hiring.ID).State(func(i *factory.WorkspaceInvitation) { i.ExpiresAt = time.Now().Add(-time.Hour) }).CreateOne(ctx, db, f))(t)
	page = sections[workspacesSections](t, workspaces())
	var hired *store.WorkspaceInvitations
	for i, r := range page.Invitations {
		if r.Workspace.ID == hiring.PublicID {
			hired = &page.Invitations[i]
		}
	}
	if hired == nil || hired.Sent != 3 || hired.Accepted != 1 || hired.Revoked != 1 || hired.Expired != 1 || hired.Workspace.Name != hiring.Name {
		t.Fatalf("invitations: %+v", page.Invitations)
	}

	// Throwaway workspaces: made and deleted within a day, by their maker.
	// Two is fine, a third a warning; one kept two days isn't one.
	thrower := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	throw := func(i int, kept time.Duration, invitations int) {
		id := fmt.Sprintf("wsp_t%013d", i)
		m.make("workspace.created", map[string]any{"workspace": id, "name": "T"}, "", time.Hour+kept, func(e *factory.Event) { e.ActorID = thrower.ID })
		for range invitations {
			m.make("workspace.invitation.created", map[string]any{"workspace": id, "email": "x@example.test"}, "", time.Hour+kept/2, func(e *factory.Event) { e.ActorID = thrower.ID })
		}
		m.make("workspace.deleted", map[string]any{"workspace": id, "name": "T"}, "", time.Hour, func(e *factory.Event) { e.ActorID = thrower.ID })
	}
	throw(1, 10*time.Minute, 2)
	throw(2, 20*time.Minute, 0)
	throw(3, 48*time.Hour, 0)
	levelOf(t, workspaces(), "workspaces.throwaway_accounts", monitor.LevelOK, 0)
	throw(4, 5*time.Minute, 1)
	v = workspaces()
	levelOf(t, v, "workspaces.throwaway_accounts", monitor.LevelWarn, 1)
	page = sections[workspacesSections](t, v)
	if len(page.Throwaways) != 1 || page.Throwaways[0].Account.ID != thrower.PublicID || page.Throwaways[0].Workspaces != 3 || page.Throwaways[0].Invitations != 3 {
		t.Fatalf("throwaways: %+v", page.Throwaways)
	}
	// Totals: created and deleted per bucket.
	var created, deleted int
	for _, b := range page.Totals.Series.Buckets {
		created += b.Counts[0]
		deleted += b.Counts[1]
	}
	if created != 3 || deleted != 4 {
		t.Fatalf("totals: %d created, %d deleted", created, deleted)
	}
}

// usageSections is the Usage page's sections.
type usageSections struct {
	Active struct {
		Day, Week, Month int64
		WithMFA          int64 `json:"withMfa"`
	} `json:"active"`
	SignUps    store.MonitorSeries `json:"signUps"`
	Activation struct {
		Workspace store.Activation `json:"workspace"`
		Invited   store.Activation `json:"invited"`
	} `json:"activation"`
	Features store.MonitorSeries  `json:"features"`
	Sizes    store.WorkspaceSizes `json:"sizes"`
	Inactive struct {
		Before   time.Time               `json:"before"`
		Count    int64                   `json:"count"`
		Accounts []store.InactiveAccount `json:"accounts"`
	} `json:"inactive"`
}

// Active accounts by day, week and month, with MFA; activation of two
// cohorts; feature use; workspace sizes; and the accounts not seen for two
// years, oldest first (compliance decision 9).
func TestAdminMonitoringUsage(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	_, session := adminSignedIn(t, db, f)
	m := eventMaker{t, db, f}
	seen := func(ago time.Duration) factory.User {
		at := time.Now().Add(-ago)
		return apitest.Must(factory.Users.State(olderThanADay).State(func(u *factory.User) { u.LastSeenAt = &at }).CreateOne(ctx, db, f))(t)
	}
	withMFA := seen(time.Hour)
	seen(2 * time.Hour)
	seen(3 * 24 * time.Hour)
	seen(20 * 24 * time.Hour)
	apitest.Must(factory.MFATOTPs.State(factory.TOTPFor(withMFA.ID)).CreateOne(ctx, db, f))(t)
	// Not seen for two years: one seen 2.5 years ago, one made 3 years ago
	// and never seen; one made 3 years ago but seen today isn't.
	lastSeen := time.Now().AddDate(-2, -6, 0)
	gone := apitest.Must(factory.Users.State(func(u *factory.User) { u.CreatedAt, u.LastSeenAt = time.Now().AddDate(-4, 0, 0), &lastSeen }).CreateOne(ctx, db, f))(t)
	never := apitest.Must(factory.Users.State(func(u *factory.User) { u.CreatedAt = time.Now().AddDate(-3, 0, 0) }).CreateOne(ctx, db, f))(t)
	today := time.Now()
	apitest.Must(factory.Users.State(func(u *factory.User) { u.CreatedAt, u.LastSeenAt = time.Now().AddDate(-3, 0, 0), &today }).CreateOne(ctx, db, f))(t)

	// Activation: of those registered 3 and 4 days ago, one made a
	// workspace within a day; of one registered 10 days ago, it invited
	// someone within a week.
	register := func(ago time.Duration) factory.User {
		u := apitest.Must(factory.Users.State(func(u *factory.User) { u.CreatedAt = time.Now().Add(-ago) }).CreateOne(ctx, db, f))(t)
		m.make("auth.registered", map[string]any{"user": u.PublicID}, "", ago, func(e *factory.Event) { e.UserID, e.ActorID = u.ID, u.ID })
		return u
	}
	active := register(3 * 24 * time.Hour)
	m.make("workspace.created", map[string]any{"workspace": "wsp_00000000000001"}, "", 3*24*time.Hour-2*time.Hour, func(e *factory.Event) { e.ActorID = active.ID })
	register(4 * 24 * time.Hour)
	inviter := register(10 * 24 * time.Hour)
	m.make("workspace.invitation.created", map[string]any{"workspace": "wsp_00000000000002"}, "", 8*24*time.Hour, func(e *factory.Event) { e.ActorID = inviter.ID })
	register(time.Hour)

	// A workspace of 3 members and no projects, touched today.
	w := apitest.Must(factory.WorkspaceFor(ctx, db, f, withMFA.ID))(t)
	for range 2 {
		member := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
		apitest.Must(factory.MemberOf(ctx, db, f, member.ID, w.ID, "member"))(t)
	}
	m.make("project.updated", map[string]any{"workspace": w.PublicID}, "", time.Hour, func(e *factory.Event) { e.WorkspaceID = w.ID })

	v := showMonitoring(t, h, session.Cookie(), "usage", "")
	levelOf(t, v, "usage.inactive_accounts", monitor.LevelOK, 2)
	levelOf(t, v, "usage.active_accounts", monitor.LevelOK, 3)
	levelOf(t, v, "usage.weekly_active_accounts", monitor.LevelOK, 4)
	levelOf(t, v, "usage.sign_ups", monitor.LevelOK, 1)
	page := sections[usageSections](t, v)
	if page.Active.Day != 3 || page.Active.Week != 4 || page.Active.Month != 5 || page.Active.WithMFA != 1 {
		t.Fatalf("active: %+v", page.Active)
	}
	if a := page.Activation.Workspace; a.Accounts != 2 || a.Activated != 1 {
		t.Fatalf("activation: %+v", a)
	}
	if a := page.Activation.Invited; a.Accounts != 1 || a.Activated != 1 {
		t.Fatalf("invited: %+v", a)
	}
	var updated int
	for _, b := range page.Features.Buckets {
		updated += b.Counts[1]
	}
	if page.Features.Series[1] != "project.updated" || updated != 1 {
		t.Fatalf("features: %+v", page.Features)
	}
	if page.Sizes.Members[1].Size != "2–5" || page.Sizes.Members[1].Workspaces != 1 || page.Sizes.Projects[0].Workspaces != 1 {
		t.Fatalf("sizes: %+v", page.Sizes)
	}
	// Longest unseen first: never seen since it was made 3 years ago,
	// then last seen 2.5 years ago.
	if page.Inactive.Count != 2 || len(page.Inactive.Accounts) != 2 || page.Inactive.Accounts[0].Account.ID != never.PublicID ||
		page.Inactive.Accounts[0].LastSeenAt != nil || page.Inactive.Accounts[1].Account.ID != gone.PublicID || page.Inactive.Accounts[1].Admin {
		t.Fatalf("inactive: %+v", page.Inactive)
	}
}
