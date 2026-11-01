package alerts

import (
	"fmt"
	"lewcontext/api/internal/monitor"
	"math"
	"strconv"
	"strings"
	"time"
)

// What an alert says, in words, wherever it leaves the system (email,
// webhook): its signal's title and meaning, its level's word, its value in
// the signal's unit, times in UTC. Fixed text and numbers only.

// About is an alert's signal as it's told: its title, unit and meaning.
// An alert whose signal is no longer defined (renamed since) is told by
// its name.
type About struct {
	Title, Unit, Meaning string
}

// AboutSignal describes the signal named name.
func AboutSignal(name string) About {
	about := About{Title: name, Unit: monitor.UnitCount}
	if sig, ok := monitor.FindSignal(name); ok {
		about.Title, about.Unit = sig.Label, sig.Unit
	}
	if rule, ok := monitor.FindAlertRule(name); ok {
		about.Meaning = rule.Meaning
		if rule.Title != "" {
			about.Title = rule.Title
		}
	}
	return about
}

// LevelWord is a level's word: "Critical" or "Warning".
func LevelWord(level string) string {
	if level == string(monitor.LevelCritical) {
		return "Critical"
	}
	return "Warning"
}

// FormatValue writes value in unit: "1,204", "5.2%", "1,450 ms",
// "12 minutes", "34 a minute".
func FormatValue(value float64, unit string) string {
	switch unit {
	case monitor.UnitPercent:
		return strconv.FormatFloat(round(value, 1), 'f', -1, 64) + "%"
	case monitor.UnitMs:
		return group(int64(math.Round(value))) + " ms"
	case monitor.UnitMinutes:
		n := int64(math.Round(value))
		if n == 1 {
			return "1 minute"
		}
		return group(n) + " minutes"
	case monitor.UnitPerMin:
		return group(int64(math.Round(value))) + " a minute"
	}
	return group(int64(math.Round(value)))
}

// round rounds v to places decimal places.
func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

// group writes n with thousands separators.
func group(n int64) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return sign + b.String()
}

// FormatTime writes t in UTC: "1 Nov 2026, 14:30 UTC".
func FormatTime(t time.Time) string {
	return t.UTC().Format("2 Jan 2006, 15:04") + " UTC"
}

// FormatDuration writes how long something lasted: "2 hours 5 minutes",
// "under a minute".
func FormatDuration(d time.Duration) string {
	minutes := int(d / time.Minute)
	if minutes < 1 {
		return "under a minute"
	}
	hours, minutes := minutes/60, minutes%60
	var parts []string
	if hours > 0 {
		parts = append(parts, plural(hours, "hour"))
	}
	if minutes > 0 {
		parts = append(parts, plural(minutes, "minute"))
	}
	return strings.Join(parts, " ")
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
