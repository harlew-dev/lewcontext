package admin_test

import (
	"context"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"
)

// eventIndex is an Events response with its window, refs and summary.
type eventIndex struct {
	store.AdminEventPage
	Window *struct {
		From *time.Time `json:"from"`
		To   time.Time  `json:"to"`
	} `json:"window"`
	Refs    store.AdminEventRefs `json:"refs"`
	Summary *store.AdminSummary  `json:"summary"`
}

func indexEvents(t *testing.T, h http.Handler, cookie *http.Cookie, query string) eventIndex {
	t.Helper()
	return decodeAdmin[eventIndex](t, adminCall(t, h, "GET", "/api/admin/events"+query, "", "", 200, cookie).Body.Bytes())
}

// sumOf adds up every column of a summary's buckets.
func sumOf(buckets []store.SummaryBucket) int {
	total := 0
	for _, b := range buckets {
		for _, n := range b.Counts {
			total += n
		}
	}
	return total
}

// The summary comes with the newest page in the same request and the same
// recorded read: zero-filled buckets sized by the window, series by group
// (or by name inside one group), the top names, failed checks by kind and
// the tiles' numbers, its totals equal to the rows the list returns.
func TestAdminEventSummary(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	now := time.Now().UTC()
	at := func(name string, payload map[string]any, ago time.Duration) {
		e := factory.Events.State(factory.EventOf(name, payload)).State(func(e *factory.Event) { e.CreatedAt = now.Add(-ago) })
		apitest.Must(e.CreateOne(ctx, db, f))(t)
	}
	for i := range 4 {
		at("auth.login_attempted", map[string]any{"email": "ada@example.test", "result": "wrong_password"}, time.Duration(i+1)*time.Minute)
	}
	at("auth.login_attempted", map[string]any{"email": "ada@example.test", "result": "succeeded"}, 2*time.Hour)
	at("auth.mfa.checked", map[string]any{"user": "usr_2yEHYLwiwPVhLW", "ok": false, "method": "totp", "step": "login"}, 3*time.Hour)
	at("auth.mfa.checked", map[string]any{"user": "usr_2yEHYLwiwPVhLW", "ok": true, "method": "totp", "step": "login"}, 3*time.Hour)
	at("auth.password_checked", map[string]any{"user": "usr_2yEHYLwiwPVhLW", "ok": false, "step": "verify"}, 4*time.Hour)
	at("auth.registration.checked", map[string]any{"email": "bob@example.test", "ok": false, "attempt": 1}, 5*time.Hour)
	at("admin.login_attempted", map[string]any{"email": "eve@example.test", "result": "not_admin"}, 6*time.Hour)
	at("admin.login_attempted", map[string]any{"email": "eve@example.test", "result": "mfa_required"}, 6*time.Hour)
	at("admin.access_refused", map[string]any{"reason": "ip_not_allowed"}, 7*time.Hour)
	at("rate_limit.reached", map[string]any{"limit": "auth:per-ip:block", "window": 900}, 8*time.Hour)
	at("workspace.created", map[string]any{"workspace": "wsp_2yEHYLwiwPVhLW", "name": "Acme"}, 9*time.Hour)
	at("project.created", map[string]any{"project": "prj_2yEHYLwiwPVhLW", "workspace": "wsp_2yEHYLwiwPVhLW", "name": "P"}, 10*time.Hour)
	at("email.blocked", map[string]any{"email": "carol@example.test"}, 11*time.Hour)
	// Outside the default window.
	at("auth.login_attempted", map[string]any{"email": "ada@example.test", "result": "wrong_password"}, 30*time.Hour)

	page := indexEvents(t, h, cookie, "?summary=include&perPage=100")
	sum := page.Summary
	if sum == nil || sum.Status != store.SummaryOK || sum.AdminEventCounts == nil {
		t.Fatalf("summary: %+v", sum)
	}
	if page.Window == nil || page.Window.From == nil || page.Window.From.Minute()%30 != 0 || page.Window.From.Second() != 0 ||
		now.Sub(*page.Window.From) < 24*time.Hour || now.Sub(*page.Window.From) > 24*time.Hour+30*time.Minute {
		t.Fatalf("window: %+v", page.Window)
	}
	// The summary counts what the list lists: the admin's own reads are hidden from both.
	if sum.Total != len(page.Data) || sum.Total != 16 || sumOf(sum.Buckets) != sum.Total {
		t.Fatalf("totals: summary %d, buckets %d, rows %d", sum.Total, sumOf(sum.Buckets), len(page.Data))
	}
	if sum.BucketSeconds != 1800 || len(sum.Buckets) < 49 || len(sum.Buckets) > 50 || !sum.Buckets[0].Start.Equal(*page.Window.From) {
		t.Fatalf("buckets: %d of %ds from %v", len(sum.Buckets), sum.BucketSeconds, sum.Buckets[0].Start)
	}
	// auth.* (9), admin.* (3), then four groups of one: Other folds the last two.
	if sum.StackedBy != store.StackedByGroup || !slices.Equal(sum.Series, []string{"auth.*", "admin.*", "workspace.*", "project.*", store.SeriesOther}) {
		t.Fatalf("series: %v", sum.Series)
	}
	if sum.TopNames[0] != (store.NameCount{Name: "auth.login_attempted", Count: 5}) || sum.LimitsReached != 1 || sum.SignInAttempts != 5 {
		t.Fatalf("tiles: %v, limits %d, sign-ins %d", sum.TopNames, sum.LimitsReached, sum.SignInAttempts)
	}
	fails := sum.Failures
	kinds := make([]int, len(store.FailureKinds))
	for _, b := range fails.Buckets {
		for i, n := range b.Counts {
			kinds[i] += n
		}
	}
	if fails.Total != 9 || !slices.Equal(kinds, []int{4, 1, 1, 1, 2}) {
		t.Fatalf("failures: %d %v", fails.Total, kinds)
	}
	// The rows' Failed badges and outcome=failed agree with the chart.
	failedRows := 0
	for _, e := range page.Data {
		if e.Failed {
			failedRows++
		}
	}
	onlyFailed := indexEvents(t, h, cookie, "?summary=include&outcome=failed")
	if failedRows != 9 || len(onlyFailed.Data) != 9 || onlyFailed.Summary.Total != 9 || onlyFailed.Summary.Failures.Total != 9 {
		t.Fatalf("failed: %d rows, %d filtered, summary %d", failedRows, len(onlyFailed.Data), onlyFailed.Summary.Total)
	}

	// Inside one group, by name; a group that can't fail has no failures chart.
	auth := indexEvents(t, h, cookie, "?summary=include&name=auth.*").Summary
	if auth.StackedBy != store.StackedByName || auth.Series[0] != "auth.login_attempted" || auth.Total != 9 || auth.Failures == nil {
		t.Fatalf("auth.*: %+v", auth.AdminEventCounts)
	}
	if projects := indexEvents(t, h, cookie, "?summary=include&name=project.*").Summary; projects.Failures != nil || projects.Total != 1 {
		t.Fatalf("project.*: %+v", projects.AdminEventCounts)
	}

	// Each window's buckets.
	for query, want := range map[string]struct{ seconds, min, max int }{
		"window=1h":    {60, 60, 61},
		"window=today": {1800, 1, 48},
		"window=7d":    {10800, 56, 57},
		"from=" + url.QueryEscape(now.Add(-3*time.Hour).Format(time.RFC3339)): {300, 36, 37},
	} {
		got := indexEvents(t, h, cookie, "?summary=include&"+query).Summary
		if got.Status != store.SummaryOK || got.BucketSeconds != want.seconds || len(got.Buckets) < want.min || len(got.Buckets) > want.max {
			t.Errorf("%s: %s, %d buckets of %ds", query, got.Status, len(got.Buckets), got.BucketSeconds)
		}
	}
	if week := indexEvents(t, h, cookie, "?summary=include&window=7d").Summary; week.Total != 17 {
		t.Fatalf("the last 7 days: %d", week.Total)
	}

	// Too wide: all time, more than 7 days, no start. The list still answers.
	eight := url.QueryEscape(now.Add(-8 * 24 * time.Hour).Format(time.RFC3339))
	for _, query := range []string{"?summary=include&window=all", "?summary=include&from=" + eight, "?summary=include&to=" + url.QueryEscape(now.Format(time.RFC3339))} {
		got := indexEvents(t, h, cookie, query)
		if got.Summary.Status != store.SummaryTooWide || got.Summary.AdminEventCounts != nil || len(got.Data) != 17 {
			t.Errorf("%s: %+v, %d rows", query, got.Summary, len(got.Data))
		}
	}
	if all := indexEvents(t, h, cookie, "?window=all"); all.Window != nil || all.Summary != nil {
		t.Fatalf("all time without a summary: %+v", all)
	}

	// Each request is one recorded read, its summary noted.
	before := len(apitest.EventsNamed(t, db, "admin.event.listed"))
	indexEvents(t, h, cookie, "?summary=include")
	listed := apitest.EventsNamed(t, db, "admin.event.listed")
	if len(listed) != before+1 || listed[len(listed)-1].Payload["filters"].(map[string]any)["summary"] != true {
		t.Fatalf("records: %d then %d, %v", before, len(listed), listed[len(listed)-1].Payload)
	}
}

// The summary's bounds: past its deadline it's too slow, and with both
// slots held it's busy; either way the list answers and one read is
// recorded.
func TestAdminEventSummaryBounds(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	apitest.Must(factory.Events.State(func(e *factory.Event) { e.CreatedAt = time.Now().Add(-time.Hour) }).CreateOne(ctx, db, f))(t)

	s.Summaries.Deadline = time.Nanosecond
	slow := indexEvents(t, h, cookie, "?summary=include")
	if slow.Summary.Status != store.SummaryTooSlow || slow.Summary.AdminEventCounts != nil || len(slow.Data) != 1 {
		t.Fatalf("too slow: %+v, %d rows", slow.Summary, len(slow.Data))
	}
	s.Summaries.Deadline = 5 * time.Second

	s.Summaries.Wait = 10 * time.Millisecond
	var releases []func()
	for range 2 {
		release, ok := s.Summaries.Acquire(ctx)
		if !ok {
			t.Fatal("a free slot wasn't given")
		}
		releases = append(releases, release)
	}
	if _, ok := s.Summaries.Acquire(ctx); ok {
		t.Fatal("a third slot was given")
	}
	busy := indexEvents(t, h, cookie, "?summary=include")
	if busy.Summary.Status != store.SummaryBusy || len(busy.Data) != 1 {
		t.Fatalf("busy: %+v", busy.Summary)
	}
	for _, release := range releases {
		release()
	}
	if ok := indexEvents(t, h, cookie, "?summary=include").Summary; ok.Status != store.SummaryOK || ok.Total != 1 {
		t.Fatalf("after release: %+v", ok)
	}
	listed := apitest.EventsNamed(t, db, "admin.event.listed")
	if len(listed) != 3 {
		t.Fatalf("%d reads recorded for 3 requests", len(listed))
	}
}

// refs name the account and workspace filters: live ones with their names,
// deleted or unknown ones by ID alone, none without the filter.
func TestAdminEventRefs(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	ada := apitest.Must(factory.Users.State(func(u *factory.User) { u.Name, u.Email = "Ada Lovelace", "ada@example.test" }).CreateOne(ctx, db, f))(t)
	gone := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	acme := apitest.Must(factory.WorkspaceFor(ctx, db, f, ada.ID, factory.Named("Acme")))(t)
	old := apitest.Must(factory.WorkspaceFor(ctx, db, f, ada.ID, factory.Named("Old")))(t)
	apitest.Must(db.NewDelete().Model((*factory.User)(nil)).Where("id = ?", gone.ID).Exec(ctx))(t)
	apitest.Must(0, s.Repo.DeleteWorkspace(ctx, store.Workspace{ID: old.ID, PublicID: old.PublicID}, "Old"))(t)

	if none := indexEvents(t, h, cookie, ""); none.Refs.Account != nil || none.Refs.Workspace != nil {
		t.Fatalf("refs without filters: %+v", none.Refs)
	}
	live := indexEvents(t, h, cookie, "?account="+ada.PublicID+"&workspace="+acme.PublicID).Refs
	if live.Account == nil || *live.Account != (store.AdminRef{PublicID: ada.PublicID, Name: "Ada Lovelace", Email: "ada@example.test"}) ||
		live.Workspace == nil || *live.Workspace != (store.AdminRef{PublicID: acme.PublicID, Name: "Acme"}) {
		t.Fatalf("live refs: %+v %+v", live.Account, live.Workspace)
	}
	for _, c := range []struct{ account, workspace string }{{gone.PublicID, old.PublicID}, {"usr_2yEHYLwiwPVhLW", "wsp_2yEHYLwiwPVhLW"}} {
		refs := indexEvents(t, h, cookie, "?account="+c.account+"&workspace="+c.workspace).Refs
		if refs.Account == nil || *refs.Account != (store.AdminRef{PublicID: c.account}) || refs.Workspace == nil || *refs.Workspace != (store.AdminRef{PublicID: c.workspace}) {
			t.Errorf("refs for %v: %+v %+v", c, refs.Account, refs.Workspace)
		}
	}
}
