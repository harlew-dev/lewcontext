package store

import (
	"slices"
	"testing"
	"time"
)

func summaryAt(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}

// Buckets cover the window, aligned to UTC, every one listed.
func TestBucketStarts(t *testing.T) {
	w := SummaryWindow{From: summaryAt("2026-11-01T13:47:00Z"), To: summaryAt("2026-11-01T14:47:12Z"), Bucket: time.Minute}
	starts := BucketStarts(w)
	if len(starts) != 61 || !starts[0].Equal(w.From) || !starts[60].Equal(summaryAt("2026-11-01T14:47:00Z")) {
		t.Fatalf("an hour by the minute: %d, %v … %v", len(starts), starts[0], starts[len(starts)-1])
	}
	// An exclusive end on a boundary opens no bucket of its own.
	day := SummaryWindow{From: summaryAt("2026-10-31T00:00:00Z"), To: summaryAt("2026-11-01T00:00:00Z"), Bucket: 30 * time.Minute}
	if n := len(BucketStarts(day)); n != 48 {
		t.Fatalf("yesterday: %d buckets", n)
	}
	// A range that doesn't start on a boundary starts with the bucket holding it.
	week := SummaryWindow{From: summaryAt("2026-10-25T13:10:00Z"), To: summaryAt("2026-11-01T13:10:00Z"), Bucket: 3 * time.Hour}
	if s := BucketStarts(week); len(s) != 57 || !s[0].Equal(summaryAt("2026-10-25T12:00:00Z")) {
		t.Fatalf("a week: %d from %v", len(s), s[0])
	}
}

// Folding: four groups and Other past five, ties in catalogue order, by
// name inside one group, the top ten, the tiles' numbers and failures.
func TestFoldSummary(t *testing.T) {
	w := SummaryWindow{From: summaryAt("2026-11-01T12:00:00Z"), To: summaryAt("2026-11-01T13:00:00Z"), Bucket: 30 * time.Minute}
	first, second := w.From, w.From.Add(30*time.Minute)
	counts := []bucketCount{
		{first, "auth.login_attempted", 10},
		{second, "auth.logged_in", 5},
		{first, "workspace.created", 4},
		{second, "project.created", 3},
		{first, "rate_limit.reached", 2},
		{second, "email.blocked", 2},
		{second, "admin.logged_in", 1},
	}
	failures := []bucketCount{{first, FailureSignIn, 6}, {second, FailureAdminDoor, 1}}
	s := foldSummary(w, nil, counts, failures, true)
	// rate_limit.* and email.* tie at 2: catalogue order puts rate_limit first.
	if s.StackedBy != StackedByGroup || !slices.Equal(s.Series, []string{"auth.*", "workspace.*", "project.*", "rate_limit.*", SeriesOther}) {
		t.Fatalf("series: %s %v", s.StackedBy, s.Series)
	}
	if s.Total != 27 || len(s.Buckets) != 2 || !slices.Equal(s.Buckets[0].Counts, []int{10, 4, 0, 2, 0}) ||
		!slices.Equal(s.Buckets[1].Counts, []int{5, 0, 3, 0, 3}) || s.BucketSeconds != 1800 {
		t.Fatalf("buckets: %+v", s.Buckets)
	}
	if s.LimitsReached != 2 || s.SignInAttempts != 10 || s.TopNames[0] != (NameCount{"auth.login_attempted", 10}) || len(s.TopNames) != 7 {
		t.Fatalf("tiles: %d %d %v", s.LimitsReached, s.SignInAttempts, s.TopNames)
	}
	// email.blocked and rate_limit.reached tie at 2: catalogue order.
	if s.TopNames[4].Name != "rate_limit.reached" || s.TopNames[5].Name != "email.blocked" {
		t.Fatalf("ties: %v", s.TopNames)
	}
	if s.Failures == nil || s.Failures.Total != 7 || !slices.Equal(s.Failures.Buckets[0].Counts, []int{6, 0, 0, 0, 0}) ||
		!slices.Equal(s.Failures.Buckets[1].Counts, []int{0, 0, 0, 0, 1}) || !slices.Equal(s.Failures.Series, FailureKinds) {
		t.Fatalf("failures: %+v", s.Failures)
	}

	// Five groups or fewer: no Other.
	five := foldSummary(w, nil, counts[:6], nil, false)
	if len(five.Series) != 5 || slices.Contains(five.Series, SeriesOther) || five.Failures != nil {
		t.Fatalf("five series: %v %+v", five.Series, five.Failures)
	}

	// Every picked name in one group: stacked by name.
	picked := []string{"auth.login_attempted", "auth.logged_in", "auth.logged_out"}
	byName := foldSummary(w, picked, counts[:2], nil, true)
	if byName.StackedBy != StackedByName || !slices.Equal(byName.Series, []string{"auth.login_attempted", "auth.logged_in"}) ||
		byName.Failures == nil || byName.Failures.Total != 0 {
		t.Fatalf("by name: %+v", byName)
	}
	if mixed := foldSummary(w, []string{"auth.login_attempted", "project.created"}, counts, nil, true); mixed.StackedBy != StackedByGroup {
		t.Fatalf("two groups picked: %s", mixed.StackedBy)
	}

	// Ten names at most.
	var many []bucketCount
	for i, name := range []string{"auth.registered", "auth.login_attempted", "auth.logged_in", "auth.logged_out", "auth.other_sessions_revoked",
		"auth.password_checked", "auth.password_changed", "auth.profile_updated", "auth.account_deleted", "project.created", "project.updated", "project.deleted"} {
		many = append(many, bucketCount{first, name, 20 - i})
	}
	if top := foldSummary(w, nil, many, nil, true).TopNames; len(top) != 10 || top[9].Name != "project.created" {
		t.Fatalf("top ten: %v", top)
	}

	// Nothing: every bucket listed, empty.
	empty := foldSummary(w, nil, nil, nil, true)
	if empty.Total != 0 || len(empty.Series) != 0 || len(empty.Buckets) != 2 || len(empty.Buckets[0].Counts) != 0 ||
		len(empty.TopNames) != 0 || empty.Failures.Total != 0 || len(empty.Failures.Buckets) != 2 {
		t.Fatalf("empty: %+v", empty)
	}
}
