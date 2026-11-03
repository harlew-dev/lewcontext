package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"testing"
	"time"
)

func TestProjectWritesRecheckAuthorityAfterWaiting(t *testing.T) {
	for _, change := range []string{"revoke", "scopes", "selection", "personal demotion", "browser demotion"} {
		t.Run(change, func(t *testing.T) {
			s, parent := queueStore(t, nil)
			ctx, cancel := context.WithTimeout(parent, 10*time.Second)
			defer cancel()
			f := factory.TestFaker(t)
			u, err := factory.Users.CreateOne(ctx, s.DB, f)
			if err != nil {
				t.Fatal(err)
			}
			w, err := factory.WorkspaceFor(ctx, s.DB, f, u.ID)
			if err != nil {
				t.Fatal(err)
			}
			p, err := factory.ProjectsIn(w.ID).CreateOne(ctx, s.DB, f)
			if err != nil {
				t.Fatal(err)
			}
			key, err := factory.ApiKeys.State(func(k *factory.ApiKey) {
				k.WorkspaceID = w.ID
				k.RevokedAt = nil
				k.TokenHash = auth.Digest(f.UUID())
				k.Scopes = []string{"project.show", "project.update"}
				if change == "personal demotion" {
					k.Kind, k.OwnerUserID = "personal", u.ID
				}
			}).CreateOne(ctx, s.DB, f)
			if err != nil {
				t.Fatal(err)
			}
			secret, digest, err := auth.NewApiKey(auth.ApiKeyKind(key.Kind), key.PublicID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.NewUpdate().Model((*apiKeyRecord)(nil)).Set("token_hash = ?", digest).Where("id = ?", key.ID).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			admitted, err := s.FindApiKeyAccess(ctx, secret, false)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := database.LockWorkspaceGraph(ctx, tx, false); err != nil {
				t.Fatal(err)
			}
			if err := lockWorkspace(ctx, tx, w.ID); err != nil {
				t.Fatal(err)
			}
			var pid int
			if err := tx.NewSelect().ColumnExpr("pg_backend_pid()").Scan(ctx, &pid); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				input := ProjectInput{Name: "Unauthorized change", Status: "Completed", Color: "blue"}
				if change == "browser demotion" {
					_, err := s.UpdateProject(events.WithActor(ctx, u.ID), Workspace{ID: w.ID, PublicID: w.PublicID}, p.PublicID, input)
					done <- err
					return
				}
				_, err := s.UpdateApiProject(ctx, admitted, p.PublicID, input)
				done <- err
			}()
			waitForBlockedTransaction(t, s.DB, pid)
			// Commit the authority change while the already-admitted write is
			// waiting for its workspace lock, rather than relying on timing.
			want := ErrApiKeyForbidden
			switch change {
			case "revoke":
				_, err = tx.NewUpdate().Model((*apiKeyRecord)(nil)).Set("token_hash = NULL").Set("revoked_at = clock_timestamp()").Where("id = ?", key.ID).Exec(ctx)
				want = ErrApiKeyInvalid
			case "scopes":
				_, err = tx.NewUpdate().Model((*apiKeyRecord)(nil)).Set("scopes = ARRAY[?]::text[]", "project.show").Where("id = ?", key.ID).Exec(ctx)
			case "selection":
				_, err = tx.NewUpdate().Model((*apiKeyRecord)(nil)).Set("project_access = ?", "selected").Where("id = ?", key.ID).Exec(ctx)
				want = sql.ErrNoRows
			default:
				_, err = tx.NewUpdate().Model((*membership)(nil)).Set("role = ?", auth.RoleViewer).Where("workspace_id = ? AND user_id = ?", w.ID, u.ID).Exec(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, want) {
				t.Fatalf("waiting write: got %v, want %v", err, want)
			}
			after, err := s.FindProject(ctx, w.ID, p.PublicID)
			if err != nil || after.Name != p.Name {
				t.Fatalf("refused write changed project: %+v, %v", after, err)
			}
			count, err := s.DB.NewSelect().Model((*factory.Event)(nil)).Where("e.name = ?", "project.updated").Count(ctx)
			if err != nil || count != 0 {
				t.Fatalf("refused write left audit side effects: %d, %v", count, err)
			}
		})
	}
}

func waitForBlockedTransaction(t *testing.T, db *bun.DB, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		waiting, err := db.NewSelect().TableExpr("pg_stat_activity").Where("? = ANY(pg_blocking_pids(pid))", pid).Exists(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("transaction did not reach its authorization lock")
}

func TestApiKeyManagementRechecksARevokedSession(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	session, err := factory.SessionFor(ctx, s.DB, f, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := factory.WorkspaceFor(ctx, s.DB, f, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := lockUser(ctx, tx, u.ID); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := tx.NewSelect().ColumnExpr("pg_backend_pid()").Scan(ctx, &pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := s.CreateApiKey(ctx, ApiKeyManager{UserID: u.ID, SessionHash: session.TokenHash}, w.PublicID, ApiKeyInput{Name: "stale session", ProjectAccess: "all", Scopes: []string{"project.index"}}, ApiKeyProof{PasswordConfirmation: PasswordConfirmation{Password: func(string) error { return nil }}})
		done <- err
	}()
	waitForBlockedTransaction(t, s.DB, pid)
	if _, err := tx.NewDelete().Model((*sessionRecord)(nil)).Where("token_hash = ?", session.TokenHash).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrApiKeyInvalid) {
		t.Fatalf("ended session issued a key: %v", err)
	}
	if n, err := s.DB.NewSelect().Model((*apiKeyRecord)(nil)).Count(ctx); err != nil || n != 0 {
		t.Fatal("refused issuance left a key")
	}
}

func TestWorkspaceGraphBarrierDrainsBeforeAccountForeignKeys(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	other, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	w, err := factory.WorkspaceFor(ctx, s.DB, f, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.MemberOf(ctx, s.DB, f, other.ID, w.ID, auth.RoleOwner); err != nil {
		t.Fatal(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := database.LockWorkspaceGraph(ctx, tx, false); err != nil {
		t.Fatal(err)
	}
	if err := lockWorkspace(ctx, tx, w.ID); err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := tx.NewSelect().ColumnExpr("pg_backend_pid()").Scan(ctx, &pid); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.DeleteAccount(ctx, User{ID: u.ID, PublicID: u.PublicID}, func(string) string { return "pseudonym" })
	}()
	waitForBlockedTransaction(t, s.DB, pid)
	// The inherited deadlock used to block here on the deleting user's FK,
	// while deletion waited for this transaction's workspace lock.
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := s.record(bounded, tx, events.WorkspaceMemberUpdated{Workspace: w.PublicID, User: u.PublicID}, EventScope{ActorID: other.ID, UserID: u.ID, WorkspaceID: w.ID}); err != nil {
		t.Fatalf("audit FK reversed deletion locks: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestApiKeyPasswordRecoveryAndVerificationPolicy(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	w, err := factory.WorkspaceFor(ctx, s.DB, f, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := factory.ApiKeys.State(func(k *factory.ApiKey) {
		k.WorkspaceID = w.ID
		k.Kind = "personal"
		k.OwnerUserID = u.ID
		k.RevokedAt = nil
		k.TokenHash = auth.Digest(f.UUID())
	}).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	secret, digest, err := auth.NewApiKey(auth.ApiKeyPersonal, key.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.NewUpdate().Model((*apiKeyRecord)(nil)).Set("token_hash = ?", digest).Where("id = ?", key.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangePassword(ctx, User{ID: u.ID, PublicID: u.PublicID}, "", PasswordConfirmation{Password: func(string) error { return nil }}, func(string) (string, error) { return u.PasswordHash, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindApiKeyAccess(ctx, secret, true); err != nil {
		t.Fatalf("password change revoked personal key: %v", err)
	}
	if _, err := s.DB.NewUpdate().Model((*User)(nil)).Set("email_verified_at = NULL").Where("id = ?", u.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindApiKeyAccess(ctx, secret, true); !errors.Is(err, ErrApiKeyInvalid) {
		t.Fatal("required verification did not gate personal credential")
	}
	if _, err := s.FindApiKeyAccess(ctx, secret, false); err != nil {
		t.Fatal("optional verification refused credential")
	}
	if _, err := s.SuspendUser(ctx, u.PublicID, Suspension{Reason: "Test suspension of personal automation"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UnsuspendUser(ctx, u.PublicID, "Test unsuspension of personal automation", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindApiKeyAccess(ctx, secret, false); !errors.Is(err, ErrApiKeyInvalid) {
		t.Fatal("unsuspension revived credential")
	}
}

func TestApiKeyCleanupHandlesNeverAndEndedMetadata(t *testing.T) {
	s, ctx := queueStore(t, nil)
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	w, err := factory.WorkspaceFor(ctx, s.DB, f, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := factory.ApiKeys.State(func(k *factory.ApiKey) { k.WorkspaceID = w.ID; k.CreatedAt = time.Now().Add(-100 * 24 * time.Hour) }).Sequence(func(i int, k *factory.ApiKey) {
		if i == 0 {
			k.RevokedAt = nil
			k.TokenHash = auth.Digest(f.UUID())
		} else if i == 1 {
			end := time.Now().Add(-91 * 24 * time.Hour)
			k.RevokedAt = &end
		} else {
			end := time.Now().Add(-99 * 24 * time.Hour)
			k.ExpiresAt = &end
		}
	}).Create(ctx, s.DB, f, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupApiKeys(ctx); err != nil {
		t.Fatal(err)
	}
	var kept []apiKeyRecord
	if err := s.DB.NewSelect().Model(&kept).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 || kept[0].PublicID != keys[0].PublicID || kept[0].ExpiresAt != nil {
		t.Fatal("cleanup confused active Never with ended metadata")
	}
}

func TestApiProjectReplayAtCapacityWithOneConnection(t *testing.T) {
	s, parent := queueStore(t, nil)
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	f := factory.TestFaker(t)
	u, err := factory.Users.CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	w, err := factory.WorkspaceFor(ctx, s.DB, f, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	k, err := factory.ApiKeys.State(func(k *factory.ApiKey) {
		k.WorkspaceID, k.RevokedAt = w.ID, nil
		k.TokenHash = auth.Digest(f.UUID())
		k.Scopes = []string{"project.store"}
	}).CreateOne(ctx, s.DB, f)
	if err != nil {
		t.Fatal(err)
	}
	secret, digest, err := auth.NewApiKey(auth.ApiKeyWorkspace, k.PublicID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.NewUpdate().Model((*apiKeyRecord)(nil)).Set("token_hash = ?", digest).Where("id = ?", k.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	access, err := s.FindApiKeyAccess(ctx, secret, false)
	if err != nil {
		t.Fatal(err)
	}
	s.DB.SetMaxOpenConns(1)
	in := ProjectInput{Name: "Replay original", Status: "Planned", Color: "blue"}
	retry := auth.Digest("original-retry-value")
	limit := ApiProjectCreateLimit{Key: "test-create-limit", Maximum: 1, Window: time.Hour}
	original, err := s.CreateApiProject(ctx, access, in, retry, limit)
	if err != nil {
		t.Fatal(err)
	}
	// The ordinary feature allowance is exhausted, but replay must not
	// reserve it again or need another connection inside the transaction.
	_, err = s.CreateApiProject(ctx, access, in, auth.Digest("another-retry-value"), limit)
	var limited ApiProjectLimited
	if !errors.As(err, &limited) {
		t.Fatalf("new create ignored allowance: %v", err)
	}
	if _, err := factory.ApiIdempotencyRequests.State(func(r *factory.ApiIdempotencyRequest) {
		r.WorkspaceID, r.ApiKeyID = w.ID, k.ID
	}).Create(ctx, s.DB, f, 4999); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateApiProject(ctx, access, in, auth.Digest("at-capacity-retry"), ApiProjectCreateLimit{}); !errors.Is(err, ErrApiReplayCapacity) {
		t.Fatalf("new replay reservation ignored storage capacity: %v", err)
	}
	if _, err := factory.ProjectsIn(w.ID).Create(ctx, s.DB, f, MaxWorkspaceProjects-1); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.CreateApiProject(ctx, access, in, retry, limit)
	originalJSON, marshalErr := json.Marshal(original)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	replayedJSON, marshalErr := json.Marshal(replayed)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if err != nil || string(replayedJSON) != string(originalJSON) {
		t.Fatalf("full quotas prevented exact replay: %+v, %v", replayed, err)
	}
	if count, err := s.DB.NewSelect().Model((*factory.Event)(nil)).Where("e.name = ?", "project.created").Count(ctx); err != nil || count != 1 {
		t.Fatalf("replay duplicated creation event: %d, %v", count, err)
	}
}
