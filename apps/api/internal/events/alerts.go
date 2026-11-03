package events

import "time"

// AlertSnapshot freezes the sanitised display and wire fields of one transition.
// Which webhooks get it is decided when it is recorded (one delivery per
// matching webhook), never carried in the event or its external body.
type AlertSnapshot struct {
	Cycle         string     `json:"cycle"`
	Sequence      int64      `json:"sequence"`
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
	JSONBody      string     `json:"jsonBody"`
	SlackBody     string     `json:"slackBody"`
}

// Alerts (docs/api/monitoring.md#alerts): the evaluator, on the one server
// holding the alerts' lock, records each change to an alert, with no actor.
// The listeners (mail.alert, alert.webhook) tell people: Mail and Webhook
// say whether each should, decided when the change was made (a silenced
// alert, or one whose signal opened another within the hour, is quiet; an
// opening past the burst limit waits for a digest), so a delivery that
// runs again, or late, decides the same. Payloads hold signal names,
// levels and numbers only: never an address, a name or a sample ID.

// AlertOpened: a signal stayed at Level for its rule's time, so an alert
// opened (Alert, its alt_ ID) with the signal's Value.
type AlertOpened struct {
	Snapshot *AlertSnapshot `json:"snapshot,omitempty"`
	Alert    string         `json:"alert"`
	Signal   string         `json:"signal"`
	Level    string         `json:"level"`
	Value    float64        `json:"value"`
	Mail     bool           `json:"mail"`
	Webhook  bool           `json:"webhook"`
}

func (AlertOpened) EventName() string { return "alert.opened" }

// AlertEscalated: an open alert's signal rose from warn to critical; the
// alert's level was raised in place.
type AlertEscalated struct {
	Snapshot *AlertSnapshot `json:"snapshot,omitempty"`
	Alert    string         `json:"alert"`
	Signal   string         `json:"signal"`
	From     string         `json:"from"`
	To       string         `json:"to"`
	Value    float64        `json:"value"`
	Mail     bool           `json:"mail"`
	Webhook  bool           `json:"webhook"`
}

func (AlertEscalated) EventName() string { return "alert.escalated" }

// AlertResolved: the signal was back at ok for 10 minutes (or at an hourly
// check), so the alert closed after OpenFor seconds. Level is the level it
// closed at.
type AlertResolved struct {
	Snapshot *AlertSnapshot `json:"snapshot,omitempty"`
	Source   string         `json:"source,omitempty"`
	Alert    string         `json:"alert"`
	Signal   string         `json:"signal"`
	Level    string         `json:"level"`
	OpenFor  int            `json:"openFor"`
	Mail     bool           `json:"mail"`
	Webhook  bool           `json:"webhook"`
}

func (AlertResolved) EventName() string { return "alert.resolved" }

// AlertReminded: a critical alert still open, not acknowledged nor
// silenced, 4 hours after it was last emailed: it's emailed again.
type AlertReminded struct {
	Mail     bool           `json:"mail"`
	Webhook  bool           `json:"webhook"`
	Snapshot *AlertSnapshot `json:"snapshot,omitempty"`
	Alert    string         `json:"alert"`
	Signal   string         `json:"signal"`
	Level    string         `json:"level"`
	Value    float64        `json:"value"`
}

func (AlertReminded) EventName() string { return "alert.reminded" }

// AlertDigested: openings past the burst limit (more than 5 in 10
// minutes) were gathered into one email, at most one every 10 minutes.
type AlertDigested struct {
	Alerts []DigestedAlert `json:"alerts"`
}

func (AlertDigested) EventName() string { return "alert.digested" }

// DigestedAlert is one alert in a digest.
type DigestedAlert struct {
	Snapshot *AlertSnapshot `json:"snapshot,omitempty"`
	Alert    string         `json:"alert"`
	Signal   string         `json:"signal"`
	Level    string         `json:"level"`
	Value    float64        `json:"value"`
}

// Alert levels, as stored.
const (
	AlertWarn     = "warn"
	AlertCritical = "critical"
)

// AlertRecovered confirms below-threshold evidence without resolving the incident.
type AlertRecovered AlertOpened

func (AlertRecovered) EventName() string { return "alert.recovered" }

// AlertReopened reactivates a cycle or starts its successor on recurrence.
type AlertReopened struct {
	Snapshot *AlertSnapshot `json:"snapshot,omitempty"`
	Alert    string         `json:"alert"`
	Signal   string         `json:"signal"`
	Level    string         `json:"level"`
	Value    float64        `json:"value"`
	Mail     bool           `json:"mail"`
	Webhook  bool           `json:"webhook"`
	Cause    string         `json:"cause"`
}

// NewAlertReopened is an opening's facts as a reopening, with its cause.
func NewAlertReopened(e AlertOpened, cause string) AlertReopened {
	return AlertReopened{Snapshot: e.Snapshot, Alert: e.Alert, Signal: e.Signal, Level: e.Level, Value: e.Value, Mail: e.Mail, Webhook: e.Webhook, Cause: cause}
}

func (AlertReopened) EventName() string { return "alert.reopened" }

func (e AlertOpened) AlertLifecycleSnapshot() *AlertSnapshot    { return e.Snapshot }
func (e AlertEscalated) AlertLifecycleSnapshot() *AlertSnapshot { return e.Snapshot }
func (e AlertRecovered) AlertLifecycleSnapshot() *AlertSnapshot { return e.Snapshot }
func (e AlertReopened) AlertLifecycleSnapshot() *AlertSnapshot  { return e.Snapshot }
func (e AlertResolved) AlertLifecycleSnapshot() *AlertSnapshot  { return e.Snapshot }
func (e AlertReminded) AlertLifecycleSnapshot() *AlertSnapshot  { return e.Snapshot }
