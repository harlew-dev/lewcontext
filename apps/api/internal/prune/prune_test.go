package prune

import (
	"testing"
	"time"
)

func TestParseAge(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"90d": 90 * 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
		"36h": 36 * time.Hour,
		"1h":  time.Hour,
	} {
		if got, err := ParseAge(value); err != nil || got != want {
			t.Errorf("ParseAge(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	// Too young, negative, unitless, other units, words, overflow-sized.
	for _, value := range []string{"0d", "0h", "-1d", "7", "7days", "30m", "1w", "", "d", "123456d", " 7d"} {
		if got, err := ParseAge(value); err == nil {
			t.Errorf("ParseAge(%q) = %v; want an error", value, got)
		}
	}
}

func TestFormatAgeRoundTrips(t *testing.T) {
	for _, value := range []string{"90d", "1d", "36h", "1h"} {
		age, _ := ParseAge(value)
		if FormatAge(age) != value {
			t.Errorf("FormatAge(ParseAge(%q)) = %q", value, FormatAge(age))
		}
	}
}

func TestFind(t *testing.T) {
	if p, ok := Find("events"); !ok || p.DefaultAge != 90*24*time.Hour || p.count == nil || p.delete == nil {
		t.Fatalf("events target: %+v %v", p, ok)
	}
	if _, ok := Find("sessions"); ok {
		t.Fatal("found a target that doesn't exist")
	}
}
