package prune

import (
	"slices"
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
	if p, ok := Find("events"); !ok || p.DefaultAge != 90*day || p.Table.Model == nil || p.Table.Keep == nil {
		t.Fatalf("events target: %+v %v", p, ok)
	}
	if _, ok := Find("sessions"); ok {
		t.Fatal("found a target that doesn't exist")
	}
}

func TestRegister(t *testing.T) {
	events, _ := Find("events")
	for name, p := range map[string]Pruner{
		"a duplicate":     events,
		"no name":         {DefaultAge: day, Table: events.Table},
		"an age under 1h": {Name: "young", DefaultAge: time.Minute, Table: events.Table},
		"no table":        {Name: "tableless", DefaultAge: day},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("registering %s did not panic", name)
				}
			}()
			register(p)
		}()
	}
	// The list is sorted by name.
	register(Pruner{Name: "aaa", DefaultAge: day, Table: events.Table})
	t.Cleanup(func() { delete(pruners, "aaa") })
	names := []string{}
	for _, p := range Pruners() {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"aaa", "alerts", "app_errors", "audit_events", "events", "metric_hours", "pseudonymise", "request_hours"}) {
		t.Fatalf("Pruners(): %v", names)
	}
}
