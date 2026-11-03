package apitest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/httpapi/input"
	"regexp"
	"strings"
	"testing"
)

// Event is a row of the events table as tests read it.
type Event struct {
	Name                         string
	ActorID, UserID, WorkspaceID sql.NullInt64
	Raw                          string         // the payload as PostgreSQL prints it
	Payload                      map[string]any `bun:"-"`
	IP                           string         // host() of events.ip, "" for null
	RequestID                    string
	UserAgent                    string
}

// NumericID finds an "id" that is a number, i.e. a leaked internal ID.
var NumericID = regexp.MustCompile(`"id":\s*\d`)

// storedEvents selects events with their payload as text.
func storedEvents(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().TableExpr("events").
		Column("name", "actor_id", "user_id", "workspace_id", "request_id", "user_agent").
		ColumnExpr("payload::text AS raw").
		ColumnExpr("coalesce(host(ip), '') AS ip").
		OrderExpr("id")
}

// EventsNamed returns the events with name, oldest first.
func EventsNamed(t *testing.T, db bun.IDB, name string) []Event {
	t.Helper()
	var found []Event
	if err := storedEvents(db).Where("name = ?", name).Scan(context.Background(), &found); err != nil {
		t.Fatal(err)
	}
	for i := range found {
		if err := json.Unmarshal([]byte(found[i].Raw), &found[i].Payload); err != nil {
			t.Fatal(err)
		}
	}
	return found
}

// OnlyEvent is the single event with name; it fails unless there is exactly one.
func OnlyEvent(t *testing.T, db bun.IDB, name string) Event {
	t.Helper()
	found := EventsNamed(t, db, name)
	if len(found) != 1 {
		t.Fatalf("want one %s event, got %d: %+v", name, len(found), found)
	}
	return found[0]
}

// Is reports whether a nullable ID column holds id (0 meaning NULL).
func Is(column sql.NullInt64, id int64) bool {
	if id == 0 {
		return !column.Valid
	}
	return column.Valid && column.Int64 == id
}

// CheckPayloadIDs checks that payloads identify records by public ID only and
// never hold numeric IDs.
func CheckPayloadIDs(t *testing.T, db bun.IDB) {
	t.Helper()
	var all []Event
	if err := storedEvents(db).Scan(context.Background(), &all); err != nil {
		t.Fatal(err)
	}
	prefixes := map[string]string{"user": "usr", "workspace": "wsp", "project": "prj"}
	for _, e := range all {
		name, raw := e.Name, e.Raw
		var payload map[string]any
		_ = json.Unmarshal([]byte(raw), &payload)
		for key, prefix := range prefixes {
			if value, ok := payload[key]; ok && !input.PublicID(prefix, fmt.Sprint(value)) {
				t.Errorf("%s: %s is not a public ID: %s", name, key, raw)
			}
		}
		if NumericID.MatchString(raw) || strings.Contains(raw, `"id"`) {
			t.Errorf("%s: internal ID in payload: %s", name, raw)
		}
	}
}
