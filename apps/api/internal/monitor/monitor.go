// Package monitor defines what the admin dashboard's Monitoring pages
// watch (docs/api/monitoring.md): each signal once, with the levels at
// which it's a problem, and each page's sections. Pages show signals;
// alerts (plans/features/admin/alerts.md) will evaluate the same
// definitions. Thresholds are in code, changed by a reviewed commit and
// listed in the doc. The queries are the store's (internal/store,
// monitor_<page>.go); nothing here builds SQL.
package monitor

import (
	"context"
	"fmt"
	"lewcontext/api/internal/httpapi/ratelimit"
	"lewcontext/api/internal/store"
	"net/url"
	"time"
)

// Level is how a signal stands: fine, worth a look, or a problem now.
type Level string

const (
	LevelOK       Level = "ok"
	LevelWarn     Level = "warn"
	LevelCritical Level = "critical"
)

// Units a signal's value is in, for the page to write it.
const (
	UnitCount   = "count"
	UnitPercent = "percent"
	UnitMs      = "ms"
	UnitMinutes = "minutes"
	UnitPerMin  = "per_minute"
)

// Env is what signals and pages read: the store, the rate limits as
// declared (httpapi.RateLimits, passed in so this package doesn't import
// the HTTP layer), and the time they're read at.
type Env struct {
	Store  *store.Store
	Limits []ratelimit.Limit
	Now    time.Time
	// Budget is how long a page's signal or section may take before it's
	// shown as not loaded (sections.go); zero: no budget of its own.
	Budget time.Duration
	// AdminKeys are the digests of the admin keys this server takes
	// (httpapi.Server.AdminKeys), for the Configuration page's count of
	// sessions under the key being retired.
	AdminKeys store.AdminKeys
	// checks keeps the integrity checks run while one page is read
	// (integrity.go); Show sets it.
	checks *checkMemo
}

// Span is what a signal is counted over: [From, To). From is its window
// before To, floored to the window's bucket as the Events page floors the
// same window, so its drill-down finds the rows it counted.
type Span struct {
	From, To time.Time
}

// Reading is a signal's measure: its value and, where a level needs
// enough to go on, how many it was counted from.
type Reading struct {
	Value  float64
	Sample int64
}

// A Signal is one number worth watching, with the levels at which it's a
// problem. Its value is at or over Warn (or Critical) to be at that level;
// 0 means no such level. Below MinSample (when set) it's ok whatever the
// value: a share of a handful means nothing.
type Signal struct {
	// Name is stable: alerts and the docs refer to it.
	Name      string
	Page      string
	Label     string
	Unit      string
	Window    time.Duration
	Warn      float64
	Critical  float64
	MinSample int64
	Measure   func(ctx context.Context, env Env, span Span) (Reading, error)
	// Drill is the Events page's filter behind the number (its window is
	// added), or nil when it has none. DrillExact: it lists exactly the
	// rows counted (or, for a share, the attempts it's a share of);
	// otherwise it lists the events the number was counted from, more
	// than it counted (the Events page has no filter for, say, an address
	// trying 10 emails).
	Drill      url.Values
	DrillExact bool
}

// Result is a signal as read, for a page.
type Result struct {
	Name          string            `json:"name"`
	Label         string            `json:"label"`
	Unit          string            `json:"unit"`
	Value         float64           `json:"value"`
	Sample        int64             `json:"sample"`
	Level         Level             `json:"level"`
	Warn          float64           `json:"warn,omitempty"`
	Critical      float64           `json:"critical,omitempty"`
	MinSample     int64             `json:"minSample,omitempty"`
	WindowSeconds int               `json:"windowSeconds"`
	Drill         map[string]string `json:"drill,omitempty"`
	DrillExact    bool              `json:"drillExact,omitempty"`
	// Unavailable: it couldn't be read within its budget; its value and
	// level mean nothing.
	Unavailable bool `json:"unavailable,omitempty"`
}

// LevelOf is the level of value read from sample.
func (sig Signal) LevelOf(r Reading) Level {
	if sig.MinSample > 0 && r.Sample < sig.MinSample {
		return LevelOK
	}
	switch {
	case sig.Critical > 0 && r.Value >= sig.Critical:
		return LevelCritical
	case sig.Warn > 0 && r.Value >= sig.Warn:
		return LevelWarn
	}
	return LevelOK
}

// SpanAt is the span sig counts at now.
func (sig Signal) SpanAt(now time.Time) Span {
	return Span{From: store.FloorTime(now.Add(-sig.Window), bucketFor(sig.Window)), To: now}
}

// Read measures sig at env.Now.
func Read(ctx context.Context, env Env, sig Signal) (Result, error) {
	span := sig.SpanAt(env.Now)
	reading, err := sig.Measure(ctx, env, span)
	if err != nil {
		return Result{}, fmt.Errorf("signal %s: %w", sig.Name, err)
	}
	out := Result{
		Name: sig.Name, Label: sig.Label, Unit: sig.Unit,
		Value: reading.Value, Sample: reading.Sample, Level: sig.LevelOf(reading),
		Warn: sig.Warn, Critical: sig.Critical, MinSample: sig.MinSample,
		WindowSeconds: int(sig.Window.Seconds()), DrillExact: sig.DrillExact,
	}
	if sig.Drill != nil {
		out.Drill = map[string]string{"window": windowName(sig.Window)}
		for k, v := range sig.Drill {
			out.Drill[k] = v[0]
		}
	}
	return out, nil
}

// ReadAll reads signals in order.
func ReadAll(ctx context.Context, env Env, signals []Signal) ([]Result, error) {
	out := make([]Result, 0, len(signals))
	for _, sig := range signals {
		r, err := Read(ctx, env, sig)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// count is a Reading of a count.
func count(n int64, err error) (Reading, error) {
	return Reading{Value: float64(n), Sample: n}, err
}

// share is a Reading of part of whole as a percentage, from whole.
func share(part, whole int64) Reading {
	if whole == 0 {
		return Reading{}
	}
	return Reading{Value: float64(part) * 100 / float64(whole), Sample: whole}
}

// drill builds an Events filter: name tokens, and any other filter.
func drill(names string, more ...string) url.Values {
	v := url.Values{"name": {names}}
	for i := 0; i+1 < len(more); i += 2 {
		v.Set(more[i], more[i+1])
	}
	return v
}
