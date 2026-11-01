package admin

import (
	"fmt"
	"lewcontext/api/internal/events"
	"slices"
	"strings"
	"testing"
	"time"
)

func at(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}

// Every relative window, resolved from a fixed instant: each starts on its
// first bucket's boundary, at midnight and across a month's end.
func TestResolveWindow(t *testing.T) {
	for _, c := range []struct {
		now, name, from, to string
		bucket              time.Duration
	}{
		{"2026-11-01T14:47:12Z", "1h", "2026-11-01T13:47:00Z", "2026-11-01T14:47:12Z", time.Minute},
		{"2026-11-01T14:47:12Z", "24h", "2026-10-31T14:30:00Z", "2026-11-01T14:47:12Z", 30 * time.Minute},
		{"2026-11-01T14:47:12Z", "today", "2026-11-01T00:00:00Z", "2026-11-01T14:47:12Z", 30 * time.Minute},
		{"2026-11-01T14:47:12Z", "yesterday", "2026-10-31T00:00:00Z", "2026-11-01T00:00:00Z", 30 * time.Minute},
		{"2026-11-01T14:47:12Z", "7d", "2026-10-25T12:00:00Z", "2026-11-01T14:47:12Z", 3 * time.Hour},
		{"2026-11-01T00:00:00Z", "today", "2026-11-01T00:00:00Z", "2026-11-01T00:00:00Z", 30 * time.Minute},
		{"2026-11-01T00:00:00Z", "yesterday", "2026-10-31T00:00:00Z", "2026-11-01T00:00:00Z", 30 * time.Minute},
		{"2026-03-01T00:10:00Z", "yesterday", "2026-02-28T00:00:00Z", "2026-03-01T00:00:00Z", 30 * time.Minute},
		{"2026-03-01T00:10:00Z", "24h", "2026-02-28T00:00:00Z", "2026-03-01T00:10:00Z", 30 * time.Minute},
		{"2026-03-01T01:10:00+01:00", "today", "2026-03-01T00:00:00Z", "2026-03-01T00:10:00Z", 30 * time.Minute},
	} {
		now := at(c.now)
		w, ok := resolveWindow(now, c.name)
		if !ok || w.From == nil || !w.From.Equal(at(c.from)) || !w.To.Equal(at(c.to)) {
			t.Errorf("%s at %s: %v %v–%v, want %s–%s", c.name, c.now, ok, w.From, w.To, c.from, c.to)
			continue
		}
		summary, ok := w.summaryWindow(now)
		if !ok || summary.Bucket != c.bucket {
			t.Errorf("%s at %s: bucket %v (%v), want %v", c.name, c.now, summary.Bucket, ok, c.bucket)
		}
	}
	now := at("2026-11-01T14:47:12Z")
	if all, ok := resolveWindow(now, "all"); !ok || all.From != nil || all.To != nil || all.echo(now) != nil {
		t.Fatalf("all time: %+v", all)
	}
	if _, ok := resolveWindow(now, "30d"); ok {
		t.Fatal("an unknown window resolved")
	}
	// The list of a window running to now has no upper bound; yesterday's has.
	day, _ := resolveWindow(now, "24h")
	yesterday, _ := resolveWindow(now, "yesterday")
	if day.listTo() != nil || yesterday.listTo() == nil {
		t.Fatalf("list bounds: %v, %v", day.listTo(), yesterday.listTo())
	}
}

// Charts cover 7 days or less, in buckets sized by the window's length.
func TestSummaryWindow(t *testing.T) {
	now := at("2026-11-01T14:47:12Z")
	for length, want := range map[time.Duration]time.Duration{
		30 * time.Minute:                 time.Minute,
		time.Hour:                        time.Minute,
		90 * time.Minute:                 5 * time.Minute,
		6 * time.Hour:                    5 * time.Minute,
		48 * time.Hour:                   30 * time.Minute,
		49 * time.Hour:                   3 * time.Hour,
		7 * 24 * time.Hour:               3 * time.Hour,
		7*24*time.Hour + time.Nanosecond: 0,
		8 * 24 * time.Hour:               0,
	} {
		from := now.Add(-length)
		w := eventWindow{From: &from, To: &now}
		got, ok := w.summaryWindow(now)
		if ok != (want != 0) || got.Bucket != want {
			t.Errorf("%v: bucket %v (%v), want %v", length, got.Bucket, ok, want)
		}
	}
	// A range with no start is too wide; one with no end runs to now.
	if _, ok := (eventWindow{To: &now}).summaryWindow(now); ok {
		t.Fatal("a range with no start was summarised")
	}
	from := now.Add(-2 * time.Hour)
	if w, ok := (eventWindow{From: &from}).summaryWindow(now); !ok || !w.To.Equal(now) {
		t.Fatalf("a range with no end: %+v", w)
	}
}

// Name tokens expand to their names' union, in catalogue order; anything
// else is refused.
func TestEventNames(t *testing.T) {
	names, tokens, ok := eventNames("workspace.invitation.created,auth.mfa.*")
	if !ok || !slices.Equal(tokens, []string{"workspace.invitation.created", "auth.mfa.*"}) ||
		!slices.Equal(names, []string{"auth.mfa.enabled", "auth.mfa.disabled", "auth.mfa.checked", "workspace.invitation.created"}) {
		t.Fatalf("names %v, tokens %v", names, tokens)
	}
	// A group and a name inside it: the union.
	if names, _, ok := eventNames("auth.mfa.*,auth.mfa.checked"); !ok || len(names) != 3 {
		t.Fatalf("overlapping tokens: %v", names)
	}
	if names, _, _ := eventNames("admin.*"); slices.ContainsFunc(names, events.AdminRead) || len(names) == 0 {
		t.Fatalf("admin.* holds reads: %v", names)
	}
	if names, _, _ := eventNames("admin-reads"); !slices.Equal(names, events.AdminReadNames()) {
		t.Fatalf("admin-reads: %v", names)
	}
	all := make([]string, 0, 64)
	for _, g := range events.Groups() {
		all = append(all, g.ID)
	}
	if names, _, ok := eventNames(strings.Join(all, ",")); !ok || !slices.Equal(names, events.Names()) {
		t.Fatalf("every group: %d names", len(names))
	}
	for _, bad := range []string{
		"auth.nope", "auth.", "auth", "auth.*.*", "nope.*", "%", "auth.*,", ",auth.*", "auth.*,,project.*",
		"auth.*,auth.*", "AUTH.*", " auth.*", "auth.login_attempted ", "admin_reads",
		strings.Repeat("auth.*,", 65) + "auth.*",
		strings.Repeat("a", maxNameParam+1),
		fmt.Sprintf("%s,%s", strings.Join(events.Names(), ","), strings.Join([]string{"auth.*", "workspace.*", "project.*", "admin.*", "admin-reads"}, ",")),
	} {
		if _, _, ok := eventNames(bad); ok {
			t.Errorf("%.60q accepted", bad)
		}
	}
}

// Times are RFC 3339 in years 1970–9999.
func TestTimeFilter(t *testing.T) {
	for raw, ok := range map[string]bool{
		"":                          true,
		"2026-11-01T14:00:00Z":      true,
		"1970-01-01T00:00:00Z":      true,
		"9999-12-31T23:59:59Z":      true,
		"2026-11-01T14:00:00+14:00": true,
		"0000-01-01T00:00:00Z":      false,
		"1969-12-31T23:59:59Z":      false,
		"1970-01-01T00:00:00+01:00": false,
		"2026-11-01":                false,
	} {
		if _, got := timeFilter(raw); got != ok {
			t.Errorf("%q: %v, want %v", raw, got, ok)
		}
	}
	// A year and a day at most.
	from, to := at("2025-11-01T00:00:00Z"), at("2026-11-02T00:00:00Z")
	if _, ok := windowQuery(map[string]string{"from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339)}, to); !ok {
		t.Error("a year and a day refused")
	}
	if _, ok := windowQuery(map[string]string{"from": from.Format(time.RFC3339), "to": to.Add(time.Second).Format(time.RFC3339)}, to); ok {
		t.Error("over a year and a day accepted")
	}
}
