package events

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// alertWire is explicitly sanitised: routing, people, messages and dashboard
// sample/drill details are deliberately absent, even if AlertSnapshot grows.
type alertWire struct {
	Version       int        `json:"version"`
	Alert         string     `json:"alert"`
	Cycle         string     `json:"cycle"`
	Sequence      int64      `json:"sequence"`
	Event         string     `json:"event"`
	Source        string     `json:"source,omitempty"`
	Cause         string     `json:"cause,omitempty"`
	Signal        string     `json:"signal"`
	Level         string     `json:"level"`
	Value         float64    `json:"value"`
	OccurredAt    time.Time  `json:"occurredAt"`
	State         string     `json:"state"`
	CurrentLevel  string     `json:"currentLevel"`
	RawLevel      string     `json:"rawLevel"`
	Title         string     `json:"title"`
	Unit          string     `json:"unit"`
	Meaning       string     `json:"meaning"`
	OpenedAt      time.Time  `json:"openedAt"`
	RecoveredAt   *time.Time `json:"recoveredAt"`
	ResolvedAt    *time.Time `json:"resolvedAt"`
	HealthySince  *time.Time `json:"healthySince"`
	HealthyChecks int        `json:"healthyChecks"`
}

// FreezeAlertSnapshot runs once in the transition transaction. Workers send
// these bytes unchanged on every retry, independent of future code or rule edits.
func FreezeAlertSnapshot(event Event) error {
	wire := alertWire{Version: 2, Event: event.EventName()}
	var snapshot *AlertSnapshot
	switch e := event.(type) {
	case AlertOpened:
		wire.Alert, wire.Signal, wire.Level, wire.Value, snapshot = e.Alert, e.Signal, e.Level, e.Value, e.Snapshot
	case AlertReopened:
		wire.Alert, wire.Signal, wire.Level, wire.Value, snapshot, wire.Cause = e.Alert, e.Signal, e.Level, e.Value, e.Snapshot, e.Cause
	case AlertRecovered:
		wire.Alert, wire.Signal, wire.Level, wire.Value, snapshot = e.Alert, e.Signal, e.Level, e.Value, e.Snapshot
	case AlertEscalated:
		wire.Alert, wire.Signal, wire.Level, wire.Value, snapshot = e.Alert, e.Signal, e.To, e.Value, e.Snapshot
	case AlertReminded:
		wire.Alert, wire.Signal, wire.Level, wire.Value, snapshot = e.Alert, e.Signal, e.Level, e.Value, e.Snapshot
	case AlertResolved:
		wire.Alert, wire.Signal, wire.Level, snapshot, wire.Source = e.Alert, e.Signal, e.Level, e.Snapshot, e.Source
	default:
		return nil
	}
	if snapshot == nil || snapshot.JSONBody != "" {
		return nil
	}
	wire.Cycle, wire.Sequence, wire.OccurredAt = snapshot.Cycle, snapshot.Sequence, snapshot.OccurredAt
	wire.State, wire.CurrentLevel, wire.RawLevel = snapshot.State, snapshot.CurrentLevel, snapshot.RawLevel
	wire.Title, wire.Unit, wire.Meaning = snapshot.Title, snapshot.Unit, snapshot.Meaning
	wire.OpenedAt, wire.RecoveredAt, wire.ResolvedAt = snapshot.OpenedAt, snapshot.RecoveredAt, snapshot.ResolvedAt
	wire.HealthySince, wire.HealthyChecks = snapshot.HealthySince, snapshot.HealthyChecks
	body, err := json.Marshal(wire)
	if err != nil {
		return err
	}
	snapshot.JSONBody = string(body)
	what := map[string]string{"alert.opened": "Alert opened", "alert.reopened": "Alert reopened", "alert.recovered": "Recovery confirmed; awaiting review", "alert.resolved": "Resolved after recovery", "alert.escalated": "Now critical", "alert.reminded": "Still active"}[event.EventName()]
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace
	text := fmt.Sprintf("%s at %s: %s. Value %g %s. State %s; sequence %d. %s", what, snapshot.OccurredAt.UTC().Format("2 Jan 2006, 15:04 UTC"), snapshot.Title, wire.Value, snapshot.Unit, snapshot.State, snapshot.Sequence, snapshot.Meaning)
	slack, err := json.Marshal(map[string]string{"text": escape(text)})
	snapshot.SlackBody = string(slack)
	return err
}
