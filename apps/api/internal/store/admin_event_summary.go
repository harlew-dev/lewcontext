package store

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"lewcontext/api/internal/events"
	"slices"
	"time"

	"github.com/uptrace/bun"
)

// The Events page's charts (docs/api/admin.md#the-summary): events per time
// bucket stacked by group or name, failed checks per bucket by kind, the
// busiest names, and the tiles' numbers, all under the list's filters. Two
// grouped counts read the database; folding them is pure Go. httpapi bounds
// them: windows of 7 days or less, a deadline, and two at a time.

// Summary statuses: computed, or why not. The list answers either way.
const (
	SummaryOK      = "ok"
	SummaryTooWide = "too_wide" // the window is over 7 days, or unbounded
	SummaryTooSlow = "too_slow" // its deadline passed
	SummaryBusy    = "busy"     // two summaries were already running on this server
)

// How a summary stacks its columns.
const (
	StackedByGroup = "group"
	StackedByName  = "name"
)

// SeriesOther folds every series past the largest four.
const SeriesOther = "other"

// Stacking and listing limits: five series at most (chart.tsx's colours),
// the ten busiest names.
const (
	maxSeries   = 5
	maxTopNames = 10
)

// summaryOrigin aligns buckets to UTC (date_bin's origin).
const summaryOrigin = "TIMESTAMPTZ '2000-01-01 00:00:00+00'"

// SummaryWindow is what a summary covers: events from From up to To
// (exclusive), counted in buckets of Bucket aligned to UTC.
type SummaryWindow struct {
	From, To time.Time
	Bucket   time.Duration
}

// AdminSummary is a summary, or only its Status when it wasn't
// computed.
type AdminSummary struct {
	Status string `json:"status"`
	*AdminEventCounts
}

// AdminEventCounts are a computed summary. Every bucket in the window is
// listed, empty ones as zeros, and Counts line up with Series: group IDs
// (auth.*) or names, then SeriesOther. Failures is nil when no event the
// filters pick can record a failed check.
type AdminEventCounts struct {
	BucketSeconds  int             `json:"bucketSeconds"`
	Total          int             `json:"total"`
	StackedBy      string          `json:"stackedBy"`
	Series         []string        `json:"series"`
	Buckets        []SummaryBucket `json:"buckets"`
	TopNames       []NameCount     `json:"topNames"`
	Failures       *FailureCounts  `json:"failures"`
	LimitsReached  int             `json:"limitsReached"`
	SignInAttempts int             `json:"signInAttempts"`
}

// SummaryBucket is one column: its start and a count per series.
type SummaryBucket struct {
	Start  time.Time `json:"start"`
	Counts []int     `json:"counts"`
}

// NameCount is how many events had a name.
type NameCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// FailureCounts are failed checks per bucket, by kind (FailureKinds).
type FailureCounts struct {
	Total   int             `json:"total"`
	Series  []string        `json:"series"`
	Buckets []SummaryBucket `json:"buckets"`
}

// bucketCount is one grouped row: a bucket, a name or failure kind, a count.
type bucketCount struct {
	Bucket time.Time `bun:"bucket"`
	Key    string    `bun:"key"`
	Count  int       `bun:"count"`
}

// SummarizeAdminEvents counts the events f picks in w. ctx bounds it; the
// caller turns a deadline into SummaryTooSlow.
func (s *Store) SummarizeAdminEvents(ctx context.Context, f AdminEventFilter, w SummaryWindow) (AdminSummary, error) {
	f.From, f.To, f.Before = nil, nil, ""
	counts, err := s.summaryCounts(ctx, f, w)
	if err != nil {
		return AdminSummary{}, err
	}
	var failures []bucketCount
	canFail := canRecordFailure(f)
	if canFail {
		if failures, err = s.summaryFailures(ctx, f, w); err != nil {
			return AdminSummary{}, err
		}
	}
	return AdminSummary{Status: SummaryOK, AdminEventCounts: foldSummary(w, f.Names, counts, failures, canFail)}, nil
}

// summaryBase selects events in w with their bucket.
func (s *Store) summaryBase(w SummaryWindow) *bun.SelectQuery {
	interval := fmt.Sprintf("%d seconds", int(w.Bucket.Seconds()))
	return s.DB.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("date_bin(?::interval, e.created_at, "+summaryOrigin+") AS bucket", interval).
		Where("e.created_at >= ?", w.From).
		Where("e.created_at < ?", w.To)
}

// summaryCounts counts events per bucket and name.
func (s *Store) summaryCounts(ctx context.Context, f AdminEventFilter, w SummaryWindow) ([]bucketCount, error) {
	q := s.summaryBase(w).ColumnExpr("e.name AS key").ColumnExpr("count(*) AS count").GroupExpr("1, 2")
	return s.scanCounts(ctx, q, f)
}

// summaryFailures counts failed checks per bucket and kind, reading the
// payloads of only the names that can record one.
func (s *Store) summaryFailures(ctx context.Context, f AdminEventFilter, w SummaryWindow) ([]bucketCount, error) {
	f.Failed = true
	q := s.summaryBase(w).ColumnExpr("? AS key", failureKind()).ColumnExpr("count(*) AS count").GroupExpr("1, 2")
	return s.scanCounts(ctx, q, f)
}

func (s *Store) scanCounts(ctx context.Context, q *bun.SelectQuery, f AdminEventFilter) ([]bucketCount, error) {
	q, err := s.filterAdminEvents(ctx, q, f)
	if errors.Is(err, errNoMatch) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []bucketCount
	err = q.Scan(ctx, &rows)
	return rows, err
}

// canRecordFailure reports whether any event f picks can record a failed
// check; with no names picked, every name but the admin reads is.
func canRecordFailure(f AdminEventFilter) bool {
	if len(f.Names) == 0 {
		return true
	}
	return slices.ContainsFunc(FailureNames(), func(name string) bool { return slices.Contains(f.Names, name) })
}

// BucketStarts are the starts of every bucket in w, oldest first: from the
// one holding w.From up to the one holding the instant before w.To.
func BucketStarts(w SummaryWindow) []time.Time {
	var starts []time.Time
	for start := FloorTime(w.From, w.Bucket); start.Before(w.To); start = start.Add(w.Bucket) {
		starts = append(starts, start)
	}
	return starts
}

// FloorTime is the start of t's bucket of size d, aligned to UTC as
// date_bin with summaryOrigin aligns it.
func FloorTime(t time.Time, d time.Duration) time.Time {
	return t.UTC().Truncate(d)
}

// foldSummary turns grouped counts into the summary: series stacked by
// group, or by name when every picked name is in one group; the busiest
// names; the tiles' numbers; failures by kind when canFail.
func foldSummary(w SummaryWindow, picked []string, counts, failures []bucketCount, canFail bool) *AdminEventCounts {
	starts := BucketStarts(w)
	out := &AdminEventCounts{BucketSeconds: int(w.Bucket.Seconds()), StackedBy: stackedBy(picked)}
	key := func(name string) string { return events.AreaOf(name) }
	order := groupOrder()
	if out.StackedBy == StackedByName {
		key = func(name string) string { return name }
		order = nameOrder()
	}
	totals := map[string]int{}
	byName := map[string]int{}
	for _, c := range counts {
		out.Total += c.Count
		totals[key(c.Key)] += c.Count
		byName[c.Key] += c.Count
	}
	out.Series = topSeries(totals, order)
	slot := map[string]int{}
	for i, series := range out.Series {
		slot[series] = i
	}
	out.Buckets = zeroBuckets(starts, len(out.Series))
	addCounts(out.Buckets, w.Bucket, counts, func(name string) int {
		if i, ok := slot[key(name)]; ok {
			return i
		}
		if i, ok := slot[SeriesOther]; ok {
			return i
		}
		return -1
	})
	out.TopNames = topNames(byName)
	out.LimitsReached = byName["rate_limit.reached"]
	out.SignInAttempts = byName["auth.login_attempted"]
	if canFail {
		out.Failures = foldFailures(starts, w.Bucket, failures)
	}
	return out
}

// stackedBy is by name when every picked name is in one group.
func stackedBy(picked []string) string {
	if len(picked) == 0 {
		return StackedByGroup
	}
	for _, name := range picked {
		if events.AreaOf(name) != events.AreaOf(picked[0]) {
			return StackedByGroup
		}
	}
	return StackedByName
}

// topSeries are the keys with any events, largest first (ties in catalogue
// order); past five, the largest four and SeriesOther.
func topSeries(totals map[string]int, order map[string]int) []string {
	keys := make([]string, 0, len(totals))
	for k, n := range totals {
		if n > 0 {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		if c := cmp.Compare(totals[b], totals[a]); c != 0 {
			return c
		}
		return compareOrder(order, a, b)
	})
	if len(keys) > maxSeries {
		return append(keys[:maxSeries-1:maxSeries-1], SeriesOther)
	}
	return keys
}

// compareOrder sorts by position in order; keys it doesn't know (a renamed
// event still in the table) go last, by name.
func compareOrder(order map[string]int, a, b string) int {
	ia, oka := order[a]
	ib, okb := order[b]
	switch {
	case oka && okb:
		return cmp.Compare(ia, ib)
	case oka != okb:
		if oka {
			return -1
		}
		return 1
	}
	return cmp.Compare(a, b)
}

// groupOrder and nameOrder are the catalogue's order of groups and names.
func groupOrder() map[string]int {
	order := map[string]int{}
	for i, g := range events.Groups() {
		order[g.ID] = i
	}
	return order
}

func nameOrder() map[string]int {
	order := map[string]int{}
	for i, name := range events.Names() {
		order[name] = i
	}
	return order
}

// zeroBuckets is a bucket per start with series zero counts.
func zeroBuckets(starts []time.Time, series int) []SummaryBucket {
	buckets := make([]SummaryBucket, len(starts))
	for i, start := range starts {
		buckets[i] = SummaryBucket{Start: start, Counts: make([]int, series)}
	}
	return buckets
}

// addCounts adds each count to its bucket, in the series slot picks.
func addCounts(buckets []SummaryBucket, size time.Duration, counts []bucketCount, slot func(key string) int) {
	if len(buckets) == 0 {
		return
	}
	first := buckets[0].Start
	for _, c := range counts {
		i := int(c.Bucket.Sub(first) / size)
		s := slot(c.Key)
		if i < 0 || i >= len(buckets) || s < 0 || s >= len(buckets[i].Counts) {
			continue
		}
		buckets[i].Counts[s] += c.Count
	}
}

// topNames are the ten names with most events, ties in catalogue order.
func topNames(byName map[string]int) []NameCount {
	order := nameOrder()
	top := make([]NameCount, 0, len(byName))
	for name, n := range byName {
		top = append(top, NameCount{Name: name, Count: n})
	}
	slices.SortFunc(top, func(a, b NameCount) int {
		if c := cmp.Compare(b.Count, a.Count); c != 0 {
			return c
		}
		return compareOrder(order, a.Name, b.Name)
	})
	return top[:min(len(top), maxTopNames)]
}

// foldFailures lays failed checks out by kind, every bucket listed.
func foldFailures(starts []time.Time, size time.Duration, counts []bucketCount) *FailureCounts {
	out := &FailureCounts{Series: FailureKinds, Buckets: zeroBuckets(starts, len(FailureKinds))}
	for _, c := range counts {
		out.Total += c.Count
	}
	addCounts(out.Buckets, size, counts, func(kind string) int { return slices.Index(FailureKinds, kind) })
	return out
}
