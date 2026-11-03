package monitor

import (
	"lewcontext/api/internal/store"
	"time"
)

// A page's window: what its charts and review lists cover, chosen in its
// header. Signals keep their own windows (Signal.Window): they say how
// things stand now, whichever window the charts show.

// Page windows, by their query value, and the default.
const (
	WindowHour    = "1h"
	WindowDay     = "24h"
	WindowWeek    = "7d"
	DefaultWindow = WindowDay
)

// windows are each window's length and chart bucket, the Events page's
// (endpoints/admin/event_window.go; TestMonitorWindowsMatchEvents checks
// they agree), so a drill-down shows the same columns.
var windows = map[string]struct{ length, bucket time.Duration }{
	WindowHour: {time.Hour, time.Minute},
	WindowDay:  {24 * time.Hour, 30 * time.Minute},
	WindowWeek: {7 * 24 * time.Hour, 3 * time.Hour},
}

// Window is a page's window as resolved at a time.
type Window struct {
	Name string
	store.SummaryWindow
}

// ResolveWindow resolves a page window at now: it starts on its first
// bucket's boundary and runs to now. False for a name not in windows.
func ResolveWindow(name string, now time.Time) (Window, bool) {
	w, ok := windows[name]
	if !ok {
		return Window{}, false
	}
	now = now.UTC()
	from := store.FloorTime(now.Add(-w.length), w.bucket)
	return Window{Name: name, SummaryWindow: store.SummaryWindow{From: from, To: now, Bucket: w.bucket}}, true
}

// WindowEcho is a page's window as its response gives it.
type WindowEcho struct {
	Name          string    `json:"name"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	BucketSeconds int       `json:"bucketSeconds"`
}

// Echo is w as a response gives it.
func (w Window) Echo() WindowEcho {
	return WindowEcho{Name: w.Name, From: w.From, To: w.To, BucketSeconds: int(w.Bucket.Seconds())}
}

// hourly is w's review lists' span: grouped by the hour ("in an hour").
func (w Window) hourly() store.MonitorSpan {
	return store.MonitorSpan{From: w.From, To: w.To, Per: time.Hour}
}

// bucketFor is the bucket a signal's window is floored to: its page
// window's, or a minute for any other length.
func bucketFor(length time.Duration) time.Duration {
	for _, w := range windows {
		if w.length == length {
			return w.bucket
		}
	}
	return time.Minute
}

// windowName is the page window of a signal's length, for its drill-down;
// signals only use those lengths (TestSignalsAreWellFormed).
func windowName(length time.Duration) string {
	for name, w := range windows {
		if w.length == length {
			return name
		}
	}
	return ""
}

// whole is a signal's span as one review-list period.
func (sp Span) whole() store.MonitorSpan {
	return store.MonitorSpan{From: sp.From, To: sp.To, Per: sp.To.Sub(sp.From) + time.Second}
}
