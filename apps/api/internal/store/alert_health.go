package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
)

// AlertHealth is the durable current-cycle health projection. Revision changes
// with every distinct observation, including failed observations.
type AlertHealth struct {
	State                  string     `json:"state" bun:",nullzero,default:'active'"`
	CurrentLevel           string     `json:"currentLevel" bun:",nullzero,default:'unknown'"`
	RawLevel               string     `json:"rawLevel" bun:",nullzero,default:'unknown'"`
	HealthySince           *time.Time `json:"healthySince"`
	HealthyChecks          int        `json:"healthyChecks"`
	RecoveredAt            *time.Time `json:"recoveredAt"`
	LastCheckedAt          *time.Time `json:"lastCheckedAt"`
	LastSuccessfulAt       *time.Time `json:"lastSuccessfulAt"`
	LastBucket             *time.Time `json:"-"`
	Hourly                 bool       `json:"hourly"`
	RecoveryInterrupted    bool       `json:"healthInterrupted"`
	RecoveryPendingRelapse bool       `json:"-"`
	Revision               int64      `json:"revision" bun:",nullzero,default:1"`
	TransitionSequence     int64      `json:"sequence"`
}

// Fresh reports whether successful evidence is still within the cadence's bound.
func (h AlertHealth) Fresh(now time.Time) bool {
	gap := 2 * time.Minute
	if h.Hourly {
		gap = 65 * time.Minute
	}
	return h.LastSuccessfulAt != nil && !now.Before(*h.LastSuccessfulAt) && now.Sub(*h.LastSuccessfulAt) <= gap
}

// At derives unknown at read time; observed durations never bridge an outage.
func (h AlertHealth) At(now time.Time) AlertHealth {
	if h.State != "resolved" && !h.Fresh(now) {
		h.State, h.CurrentLevel = "unknown", "unknown"
	}
	return h
}

// AlertObservation is one completed scheduled query, bound to the snapshot read
// before doing I/O. A superseded cycle/revision cannot consume its result.
type AlertObservation struct {
	ID              int64
	Revision        int64
	At              time.Time
	Level, RawLevel string
	Value           float64
	Unavailable     bool
}

// UpdateAlertHealth applies a distinct completed observation under the global
// write lock, then parent and cycle locks, never during signal query I/O.
func (s *Store) UpdateAlertHealth(ctx context.Context, observation AlertObservation) (string, error) {
	transition := ""
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := database.LockAlertWrites(ctx, tx); err != nil {
			return err
		}
		var row alertRecord
		if err := tx.NewSelect().Model(&row).Where("id = ?", observation.ID).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		if row.ResolvedAt != nil || row.Revision != observation.Revision {
			return nil
		}
		at := observation.At
		if at.IsZero() {
			if err := tx.NewSelect().ColumnExpr("clock_timestamp()").Scan(ctx, &at); err != nil {
				return err
			}
		}
		bucket := at.Truncate(time.Minute)
		if row.Hourly {
			bucket = at.Truncate(time.Hour)
		}
		if row.LastBucket != nil && !bucket.After(*row.LastBucket) {
			return nil
		}
		if row.LastCheckedAt != nil && !at.After(*row.LastCheckedAt) {
			return nil
		}
		wasRecovered := row.RecoveredAt != nil || row.RecoveryPendingRelapse
		fresh := row.AlertHealth.Fresh(at)
		row.LastCheckedAt, row.LastBucket = &at, &bucket
		row.Revision++
		if observation.Unavailable {
			row.RecoveryPendingRelapse = wasRecovered
			interruptAlertRecovery(&row)
			row.State, row.CurrentLevel, row.RawLevel = "unknown", "unknown", "unknown"
		} else {
			if !fresh {
				interruptAlertRecovery(&row)
			}
			row.LastSuccessfulAt = &at
			row.LastValue = observation.Value
			row.CurrentLevel, row.RawLevel = observation.Level, observation.RawLevel
			if observation.Level == "ok" {
				if row.HealthySince == nil {
					row.HealthySince = &at
				}
				row.HealthyChecks++
				row.State = "confirming"
				confirmed := row.Hourly || (row.HealthyChecks >= 11 && at.Sub(*row.HealthySince) >= 10*time.Minute)
				if confirmed {
					row.State = "recovered"
					if row.RecoveredAt == nil {
						row.RecoveredAt = &at
						transition = "alert.recovered"
					}
					row.RecoveryPendingRelapse = false
				}
			} else {
				interruptAlertRecovery(&row)
				row.State = "active"
				if observation.Value > row.PeakValue {
					row.PeakValue = observation.Value
				}
				if wasRecovered {
					transition = "alert.reopened"
					row.AcknowledgedAt, row.AcknowledgedBy = nil, nil
					row.ReminderAnchor = at
					row.RecoveryPendingRelapse = false
				}
				if observation.Level == "critical" && row.Level != "critical" {
					row.Level = "critical"
					if transition == "" {
						transition = "alert.escalated"
					}
				}
			}
		}
		if transition != "" {
			row.TransitionSequence++
		}
		if err := saveAlertHealth(ctx, tx, &row); err != nil {
			return err
		}
		if transition == "" {
			return nil
		}
		mail, webhook, err := reserveAlertTransition(ctx, tx, row, at)
		if err != nil {
			return err
		}
		snapshot := alertSnapshot(row, at)
		opened := events.AlertOpened{Alert: row.PublicID, Signal: row.Signal, Level: row.Level, Value: row.LastValue, Mail: mail, Webhook: webhook, Snapshot: snapshot}
		switch transition {
		case "alert.recovered":
			return s.record(ctx, tx, events.AlertRecovered(opened), EventScope{})
		case "alert.reopened":
			return s.record(ctx, tx, events.NewAlertReopened(opened, "relapse"), EventScope{})
		default:
			return s.record(ctx, tx, events.AlertEscalated{Alert: row.PublicID, Signal: row.Signal, From: "warn", To: row.Level, Value: row.LastValue, Mail: mail, Webhook: webhook, Snapshot: snapshot}, EventScope{})
		}
	})
	return transition, err
}

func interruptAlertRecovery(row *alertRecord) {
	if row.HealthySince != nil || row.RecoveredAt != nil {
		row.RecoveryInterrupted = true
	}
	row.HealthySince, row.RecoveredAt = nil, nil
	row.HealthyChecks = 0
}

func saveAlertHealth(ctx context.Context, tx bun.Tx, row *alertRecord) error {
	cycle, cycleErr := FindCurrentIncidentCycle(ctx, tx, IncidentParent{AlertID: row.ID}, true)
	if cycleErr != nil {
		return cycleErr
	}
	row.cyclePublicID = cycle.PublicID
	cycle.AlertHealth, cycle.Level, cycle.PeakValue = row.AlertHealth, row.Level, row.PeakValue
	cycle.AcknowledgedAt, cycle.AcknowledgedBy, cycle.Notified, cycle.ReminderAnchor = row.AcknowledgedAt, row.AcknowledgedBy, row.Notified, row.ReminderAnchor
	if _, err := tx.NewUpdate().Model(&cycle).Column("state", "current_level", "raw_level", "healthy_since", "healthy_checks", "recovered_at", "last_checked_at", "last_successful_at", "last_bucket", "hourly", "recovery_interrupted", "recovery_pending_relapse", "revision", "transition_sequence", "reminder_anchor", "peak_value", "level", "acknowledged_at", "acknowledged_by", "notified").WherePK().Exec(ctx); err != nil {
		return err
	}
	_, err := tx.NewUpdate().Model(row).Column("state", "current_level", "raw_level", "healthy_since", "healthy_checks", "recovered_at", "last_checked_at", "last_successful_at", "last_bucket", "hourly", "recovery_interrupted", "recovery_pending_relapse", "revision", "transition_sequence", "reminder_anchor", "last_value", "peak_value", "level", "acknowledged_at", "acknowledged_by").WherePK().Exec(ctx)
	return err
}

func reserveAlertTransition(ctx context.Context, tx bun.Tx, row alertRecord, at time.Time) (bool, bool, error) {
	if row.Notified == NotifiedQuiet || alertOf(row).Silenced(at) {
		return false, false, nil
	}
	mail, err := mailAllowed(ctx, tx, row.Signal, at)
	if err != nil {
		return false, false, err
	}
	if mail {
		if err := recordMail(ctx, tx, row.ID, at); err != nil {
			return false, false, err
		}
	}
	webhook, err := reserveAlertWebhook(ctx, tx, row, at, true)
	return mail, webhook, err
}

// reserveAlertWebhook says whether the transition is eligible to be posted:
// not quiet and not silenced. Which webhooks get it, and each one's cap of 12
// posts an hour per signal, are decided when the event is recorded
// (queueAlertWebhooks), so a capped notice is an event no receiver was queued.
func reserveAlertWebhook(_ context.Context, _ bun.Tx, _ alertRecord, _ time.Time, eligible bool) (bool, error) {
	return eligible, nil
}

func alertSnapshot(row alertRecord, at time.Time) *events.AlertSnapshot {
	return &events.AlertSnapshot{Cycle: row.cyclePublicID, Sequence: row.TransitionSequence, OccurredAt: at, State: row.State, CurrentLevel: row.CurrentLevel, RawLevel: row.RawLevel, Title: row.ExternalTitle, Unit: row.ExternalUnit, Meaning: row.ExternalMeaning, OpenedAt: row.OpenedAt, RecoveredAt: row.RecoveredAt, ResolvedAt: row.ResolvedAt, HealthySince: row.HealthySince, HealthyChecks: row.HealthyChecks}
}

func (s *Store) createAlertCycle(ctx context.Context, tx bun.Tx, row *alertRecord) error {
	cycle, err := CreateIncidentCycle(ctx, tx, IncidentParent{AlertID: row.ID}, row.OpenedAt)
	row.cyclePublicID = cycle.PublicID
	return err
}

// FindAlertEvaluationTime uses the database clock for scheduling and evidence.
func (s *Store) FindAlertEvaluationTime(ctx context.Context) (time.Time, error) {
	var at time.Time
	err := s.DB.NewSelect().ColumnExpr("clock_timestamp()").Scan(ctx, &at)
	return at, err
}

// CheckAlertHourlyDue persists the hourly schedule through parent observations:
// a rule is due when it hasn't been checked in the current hour, so an early
// duplicate leader never manufactures another healthy hourly check.
func (s *Store) CheckAlertHourlyDue(ctx context.Context, signal string, at time.Time) (bool, error) {
	var last *time.Time
	err := s.DB.NewSelect().Model((*alertRecord)(nil)).Column("last_checked_at").Where("signal = ?", signal).Scan(ctx, &last)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return last == nil || at.Truncate(time.Hour).After(last.Truncate(time.Hour)), nil
}
