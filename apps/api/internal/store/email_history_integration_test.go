package store

import (
	"lewcontext/api/internal/factory"
	"strings"
	"testing"
	"time"
)

func TestEmailHistoryRetainsMinimisedReferencesUntilPruned(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	const email = "former@example.test"
	released := time.Now().Add(-40 * 24 * time.Hour)
	history, err := factory.UserEmailHistories.State(func(h *factory.UserEmailHistory) { h.UserID, h.Email, h.ReleasedAt = u.ID, email, released }).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	e, err := factory.Events.State(func(e *factory.Event) {
		e.Name, e.CreatedAt = "auth.email_changed", released.Add(-time.Hour)
		e.Payload = map[string]any{"email": "current@example.test", "previousEmail": email}
	}).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PseudonymiseEvents(ctx, time.Now().Add(-30*24*time.Hour), 100, factory.Keys.EmailPseudonym); err != nil {
		t.Fatal(err)
	}
	var payload string
	if err := s.DB.NewSelect().Model((*eventRecord)(nil)).ColumnExpr("payload::text").Where("id = ?", e.ID).Scan(ctx, &payload); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(payload, email) || !strings.Contains(payload, factory.Keys.EmailPseudonym(email)) {
		t.Fatalf("previousEmail not minimised: %s", payload)
	}
	if err := s.cleanupEmailHistory(ctx, factory.Keys.EmailPseudonym); err != nil {
		t.Fatal(err)
	}
	if found, err := s.DB.NewSelect().Model((*userEmailHistory)(nil)).Where("id = ?", history.ID).Exists(ctx); err != nil || !found {
		t.Fatalf("reference lost its erasure registry: %v", err)
	}
	if _, err := s.DB.NewDelete().Model((*eventRecord)(nil)).Where("id = ?", e.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupEmailHistory(ctx, factory.Keys.EmailPseudonym); err != nil {
		t.Fatal(err)
	}
	if found, err := s.DB.NewSelect().Model((*userEmailHistory)(nil)).Where("id = ?", history.ID).Exists(ctx); err != nil || found {
		t.Fatalf("unused former address retained: %v", err)
	}
}

func TestEmailHistoryCleanupDoesNotStarveLaterRows(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	released := time.Now()
	if _, err := factory.UserEmailHistories.State(func(h *factory.UserEmailHistory) {
		h.UserID, h.Email, h.ReleasedAt = u.ID, "retained@example.test", released
	}).Create(ctx, s.DB, f, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Events.State(func(e *factory.Event) {
		e.CreatedAt = released.Add(-time.Minute)
		e.Payload = map[string]any{"email": "retained@example.test"}
	}).CreateOne(ctx, s.DB, f); err != nil {
		t.Fatal(err)
	}
	unused, err := factory.UserEmailHistories.State(func(h *factory.UserEmailHistory) { h.UserID = u.ID }).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupEmailHistory(ctx, factory.Keys.EmailPseudonym); err != nil {
		t.Fatal(err)
	}
	if found, err := s.DB.NewSelect().Model((*userEmailHistory)(nil)).Where("id = ?", unused.ID).Exists(ctx); err != nil || found {
		t.Fatalf("later unreferenced row never checked: %v", err)
	}
}
