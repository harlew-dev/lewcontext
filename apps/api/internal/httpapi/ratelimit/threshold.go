package ratelimit

// Threshold is the count a level of a limit triggers above, or Never. There
// is no bare number where 0 might mean "off": a level that never triggers
// is written out as Never.
type Threshold struct {
	at  int
	set bool
}

// Above is a level that triggers once a window holds more than n hits.
func Above(n int) Threshold { return Threshold{at: n, set: true} }

// Never is a level that never triggers.
var Never Threshold

// Exceeds reports whether count is past this level; Never never is.
func (t Threshold) Exceeds(count int) bool { return t.set && count > t.at }

// Crossed reports whether count is the hit that first goes past this level
// in its window, so the level is recorded once however many follow.
func (t Threshold) Crossed(count int) bool { return t.set && count == t.at+1 }

// Set reports whether the level can trigger at all.
func (t Threshold) Set() bool { return t.set }

// At is the count the level triggers above, or 0 for Never.
func (t Threshold) At() int { return t.at }
