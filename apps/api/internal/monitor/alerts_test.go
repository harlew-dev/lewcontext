package monitor

import (
	"testing"
	"time"
)

// Every rule names a signal that exists and has a level to alert at, once,
// with a meaning; hourly rules have no For (their checks are an hour
// apart); a rule's From and Cap are levels.
func TestAlertRulesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range AlertRules {
		sig, ok := FindSignal(r.Signal)
		switch {
		case !ok:
			t.Errorf("%s: no such signal", r.Signal)
			continue
		case seen[r.Signal]:
			t.Errorf("%s: two rules", r.Signal)
		case sig.Warn == 0 && sig.Critical == 0:
			t.Errorf("%s: the signal has no level to alert at", r.Signal)
		case r.Meaning == "" || len(r.Meaning) > 200:
			t.Errorf("%s: a rule needs a sentence of meaning", r.Signal)
		case r.Hourly && r.For != 0:
			t.Errorf("%s: an hourly rule has no For", r.Signal)
		case r.For < 0 || r.For > time.Hour:
			t.Errorf("%s: For %v", r.Signal, r.For)
		}
		for _, l := range []Level{r.From, r.Cap} {
			if l != "" && l != LevelWarn && l != LevelCritical {
				t.Errorf("%s: level %q", r.Signal, l)
			}
		}
		if r.From == LevelCritical && sig.Critical == 0 {
			t.Errorf("%s: alerts from critical, which the signal never reaches", r.Signal)
		}
		seen[r.Signal] = true
	}
	if len(AlertRules) < 15 {
		t.Fatalf("only %d rules", len(AlertRules))
	}
}

// A rule's level: the signal's, from From, at most Cap.
func TestAlertRuleLevel(t *testing.T) {
	plain := AlertRule{}
	cluster := AlertRule{From: LevelCritical, Cap: LevelWarn}
	capped := AlertRule{Cap: LevelWarn}
	for _, c := range []struct {
		rule        AlertRule
		level, want Level
	}{
		{plain, LevelOK, LevelOK}, {plain, LevelWarn, LevelWarn}, {plain, LevelCritical, LevelCritical},
		{cluster, LevelWarn, LevelOK}, {cluster, LevelCritical, LevelWarn},
		{capped, LevelWarn, LevelWarn}, {capped, LevelCritical, LevelWarn}, {capped, LevelOK, LevelOK},
	} {
		if got := c.rule.Level(c.level); got != c.want {
			t.Errorf("%+v at %s: %s, want %s", c.rule, c.level, got, c.want)
		}
	}
}
