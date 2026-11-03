package store

import (
	"testing"
	"time"
)

func TestAlertRecoveryDistinctEvidenceAndUnknown(t *testing.T) {
	s, ctx := queueStore(t, nil)
	a, err := s.OpenAlert(ctx, NewAlert{Signal: "auth.stuffing_addresses", Level: "warn", Value: 1, Title: "Credential stuffing", Unit: "count"})
	if err != nil {
		t.Fatal(err)
	}
	start := a.OpenedAt.Add(time.Minute).Truncate(time.Minute)
	read := func(at time.Time, unavailable bool) Alert {
		t.Helper()
		_, err := s.UpdateAlertHealth(ctx, AlertObservation{ID: a.ID, Revision: a.Revision, At: at, Level: "ok", RawLevel: "ok", Unavailable: unavailable})
		if err != nil {
			t.Fatal(err)
		}
		var row alertRecord
		if err := s.DB.NewSelect().Model(&row).Where("id = ?", a.ID).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		a = alertOf(row)
		return a
	}
	for i := 0; i < 10; i++ {
		at := start.Add(time.Duration(i) * time.Minute)
		read(at, false)
		read(at.Add(10*time.Second), false)
	}
	if a.State != "confirming" || a.HealthyChecks != 10 || a.ResolvedAt != nil {
		t.Fatalf("premature recovery: %+v", a)
	}
	read(start.Add(10*time.Minute), false)
	if a.State != "recovered" || a.HealthyChecks != 11 || a.ResolvedAt != nil {
		t.Fatalf("not recovered but unresolved: %+v", a)
	}
	sequence := a.TransitionSequence
	read(start.Add(11*time.Minute), false)
	if a.TransitionSequence != sequence {
		t.Fatal("duplicate recovery event")
	}
	read(start.Add(12*time.Minute), true)
	if a.State != "unknown" || a.HealthyChecks != 0 || a.RecoveredAt != nil {
		t.Fatalf("unavailable retained evidence: %+v", a)
	}
	read(start.Add(13*time.Minute), false)
	if a.State != "confirming" || a.HealthyChecks != 1 {
		t.Fatalf("outage bridged: %+v", a)
	}
	read(start.Add(16*time.Minute), false)
	if a.HealthyChecks != 1 {
		t.Fatal("gap bridged")
	}
}

func TestAlertRecoveryHourlyAndStaleRevision(t *testing.T) {
	s, ctx := queueStore(t, nil)
	a, err := s.OpenAlert(ctx, NewAlert{Signal: "integrity.admins_without_mfa", Level: "critical", Value: 1, Hourly: true})
	if err != nil {
		t.Fatal(err)
	}
	at := a.OpenedAt.Add(time.Hour)
	if _, err := s.UpdateAlertHealth(ctx, AlertObservation{ID: a.ID, Revision: a.Revision, At: at, Level: "ok", RawLevel: "ok"}); err != nil {
		t.Fatal(err)
	}
	var row alertRecord
	if err := s.DB.NewSelect().Model(&row).Where("id = ?", a.ID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if row.State != "recovered" || row.HealthyChecks != 1 {
		t.Fatalf("hourly: %+v", row)
	}
	if _, err := s.UpdateAlertHealth(ctx, AlertObservation{ID: a.ID, Revision: a.Revision, At: at.Add(time.Minute), Level: "critical", RawLevel: "critical"}); err != nil {
		t.Fatal(err)
	}
	var after alertRecord
	if err := s.DB.NewSelect().Model(&after).Where("id = ?", a.ID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if after.Revision != row.Revision || after.State != "recovered" {
		t.Fatal("stale query overwrote evidence")
	}
	if after.AlertHealth.At(at.Add(66*time.Minute)).State != "unknown" {
		t.Fatal("stale recovery remained green")
	}
}
