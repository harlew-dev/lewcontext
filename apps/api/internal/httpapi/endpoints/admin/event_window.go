package admin

import (
	"lewcontext/api/internal/store"
	"time"
)

// The Events page's time: a relative window (the last hour, today, …)
// resolved from the server's clock, so a browser's clock can't skew it, or
// a custom range. Windows are UTC, like every time in the dashboard
// (docs/api/admin.md#events).

// Relative windows, by their query value.
const (
	windowHour      = "1h"
	windowDay       = "24h"
	windowToday     = "today"
	windowYesterday = "yesterday"
	windowWeek      = "7d"
	windowAll       = "all"
)

// defaultEventWindow is the window when none is asked for.
const defaultEventWindow = windowDay

// The other query values the list takes.
const (
	outcomeFailed  = "failed"
	summaryInclude = "include"
)

// maxSummaryWindow: charts cover 7 days or less.
const maxSummaryWindow = 7 * 24 * time.Hour

// summaryBuckets size a chart's columns by its window's length: at most
// 61, 73, 97 and 57 of them.
var summaryBuckets = []struct{ upTo, bucket time.Duration }{
	{time.Hour, time.Minute},
	{6 * time.Hour, 5 * time.Minute},
	{48 * time.Hour, 30 * time.Minute},
	{maxSummaryWindow, 3 * time.Hour},
}

// bucketFor is the bucket for a window of length; false past 7 days.
func bucketFor(length time.Duration) (time.Duration, bool) {
	for _, b := range summaryBuckets {
		if length <= b.upTo {
			return b.bucket, true
		}
	}
	return 0, false
}

// eventWindow is the time an Events request covers: from From (nil: the
// beginning) to To (exclusive; nil: now). Name is the relative window, or
// "" for a custom range; Bucket is a relative window's chart bucket (a
// custom range's comes from its length).
type eventWindow struct {
	Name     string
	From, To *time.Time
	Bucket   time.Duration
}

// dayBucket is the bucket of a window of a day or so: 24h, today, yesterday.
const dayBucket = 30 * time.Minute

// resolveWindow resolves a relative window at now. A window starts on its
// first bucket's boundary, so "the last 24 hours" at 14:47 starts at 14:30
// yesterday and the chart's first column is whole; it runs to now, except
// yesterday, which ends at midnight.
func resolveWindow(now time.Time, name string) (eventWindow, bool) {
	now = now.UTC()
	midnight := store.FloorTime(now, 24*time.Hour)
	last := func(length time.Duration) (eventWindow, bool) {
		bucket, _ := bucketFor(length)
		from := store.FloorTime(now.Add(-length), bucket)
		return eventWindow{Name: name, From: &from, To: &now, Bucket: bucket}, true
	}
	switch name {
	case windowHour:
		return last(time.Hour)
	case windowDay:
		return last(24 * time.Hour)
	case windowWeek:
		return last(maxSummaryWindow)
	case windowToday:
		return eventWindow{Name: name, From: &midnight, To: &now, Bucket: dayBucket}, true
	case windowYesterday:
		from := midnight.Add(-24 * time.Hour)
		return eventWindow{Name: name, From: &from, To: &midnight, Bucket: dayBucket}, true
	case windowAll:
		return eventWindow{Name: name}, true
	}
	return eventWindow{}, false
}

// listTo is the list's upper bound: none for a window running to now (the
// newest rows, whatever the database's clock says), else To.
func (w eventWindow) listTo() *time.Time {
	switch w.Name {
	case windowHour, windowDay, windowWeek, windowToday:
		return nil
	}
	return w.To
}

// windowEcho is the window as the response gives it: null for all time,
// From null for a range with no start, To now for one with no end.
type windowEcho struct {
	From *time.Time `json:"from"`
	To   time.Time  `json:"to"`
}

func (w eventWindow) echo(now time.Time) *windowEcho {
	if w.Name == windowAll {
		return nil
	}
	to := now
	if w.To != nil {
		to = *w.To
	}
	return &windowEcho{From: w.From, To: to}
}

// summaryWindow is what a summary of w covers; false when it has no start
// or is a range longer than 7 days. A relative window keeps its own bucket
// (the last 7 days start up to a bucket earlier, on its boundary).
func (w eventWindow) summaryWindow(now time.Time) (store.SummaryWindow, bool) {
	if w.From == nil {
		return store.SummaryWindow{}, false
	}
	to := now
	if w.To != nil {
		to = *w.To
	}
	bucket, ok := w.Bucket, w.Bucket != 0
	if !ok {
		bucket, ok = bucketFor(to.Sub(*w.From))
	}
	return store.SummaryWindow{From: *w.From, To: to, Bucket: bucket}, ok
}
