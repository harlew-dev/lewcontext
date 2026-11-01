package store

import (
	"bytes"
	"encoding/json"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/observe"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer is a log destination tests can read while slog writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Every audit event (events.Audit) is also written as one JSON log line,
// msg "audit", with its evt_ ID, name, the actor's, user's and
// workspace's public IDs, the IP, when, and the request's ID; never its
// payload, so no email or name. Other events write none.
func TestAuditEventsAreLogged(t *testing.T) {
	s, ctx := queueStore(t, nil)
	out := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(observe.NewLogger(out, observe.FormatJSON, slog.LevelInfo))
	t.Cleanup(func() { slog.SetDefault(previous) })
	f := factory.TestFaker(t)
	users, err := factory.Users.Create(ctx, s.DB, f, 2)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := factory.WorkspaceFor(ctx, s.DB, f, users[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	request := events.WithUserAgent(events.WithRequest(events.WithActor(ctx, users[0].ID), "198.51.100.7", "req_00000000000007"), "test")
	audited := []events.Event{
		events.LoginAttempted{Email: "ada.secret@example.test", Result: "wrong_password"},
		events.Registered{User: "usr_00000000000001", Name: "Ada Secretname", Email: "ada.secret@example.test", VerifiedBy: "code"},
		events.AdminUserListed{Admin: "usr_00000000000001", Q: "searched.secret@example.test"},
		events.ServerStarted{Server: "srv_00000000000001", Version: "dev"},
		events.PruneCompleted{Target: "events", Cutoff: time.Now().UTC().Format(time.RFC3339)},
	}
	for _, e := range audited {
		if err := s.Record(request, e, EventScope{UserID: users[1].ID, WorkspaceID: workspace.ID}); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range []events.Event{
		events.WorkspaceInvitationCreated{Invitation: "inv_00000000000001", Workspace: workspace.PublicID, Email: "invitee.secret@example.test", Role: "member"},
		events.RateLimitReached{Limit: "auth:per-ip:block", Window: 900, ResetsAt: time.Now()},
		events.EmailBlocked{Email: "blocked.secret@example.test"},
	} {
		if err := s.Record(request, e, EventScope{}); err != nil {
			t.Fatal(err)
		}
	}
	out.mu.Lock()
	text := out.buf.String()
	out.mu.Unlock()
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not JSON: %q", line)
		}
		if m["msg"] == "audit" {
			lines = append(lines, m)
		}
	}
	if len(lines) != len(audited) {
		t.Fatalf("%d audit lines for %d audit events: %s", len(lines), len(audited), text)
	}
	for i, line := range lines {
		event, _ := line["event"].(string)
		if line["name"] != audited[i].EventName() || !strings.HasPrefix(event, "evt_") || line["actor"] != users[0].PublicID ||
			line["user"] != users[1].PublicID || line["workspace"] != workspace.PublicID || line["ip"] != "198.51.100.7" ||
			line["requestId"] != "req_00000000000007" || line["createdAt"] == "" {
			t.Errorf("line %d: %v", i, line)
		}
		// The line names a row that exists.
		if n, err := s.DB.NewSelect().Model((*eventRecord)(nil)).Where("e.public_id = ? AND e.name = ?", event, line["name"]).Count(ctx); err != nil || n != 1 {
			t.Errorf("line %d's event: %d %v", i, n, err)
		}
	}
	if strings.Contains(text, "secret") {
		t.Fatalf("an email or a name reached the log: %s", text)
	}
}
