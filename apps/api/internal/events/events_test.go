package events

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// eventName is <resource>.<fact> in a known area: two or three snake_case
// segments, like the events.name CHECK (migration 007).
var eventName = regexp.MustCompile(`^(auth|workspace|project|rate_limit)(\.[a-z][a-z_]*){1,2}$`)

func TestEventNamePattern(t *testing.T) {
	for _, name := range []string{"workspace.member.removed", "auth.logged_in", "auth.password_reset.requested", "project.updated", "rate_limit.reached"} {
		if !eventName.MatchString(name) {
			t.Errorf("%q refused", name)
		}
	}
	for _, name := range []string{"workspace.member_removed.x.y", "Workspace.created", "workspace..created", "workspace.created.", "workspace.1created", "workspace", "billing.paid"} {
		if eventName.MatchString(name) {
			t.Errorf("%q accepted", name)
		}
	}
}

func TestCatalogueNames(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Catalogue {
		name := e.EventName()
		if !eventName.MatchString(name) || seen[name] {
			t.Errorf("%T: name %q is malformed or used twice", e, name)
		}
		seen[name] = true
	}
}

// Payloads are stored and may later leave the system (webhooks), so they must
// not carry secrets or internal IDs, and every field needs an explicit name.
func TestPayloadsHoldNothingSensitive(t *testing.T) {
	forbidden := []string{"password", "token", "hash", "csrf", "secret"}
	for _, e := range Catalogue {
		typ := reflect.TypeOf(e)
		for field := range typ.Fields() {
			name := strings.ToLower(field.Name)
			for _, word := range forbidden {
				if strings.Contains(name, word) {
					t.Errorf("%s.%s looks sensitive", typ.Name(), field.Name)
				}
			}
			if field.Type.Kind() == reflect.Int64 {
				t.Errorf("%s.%s is an int64; payloads use public IDs", typ.Name(), field.Name)
			}
			if tag := field.Tag.Get("json"); tag == "" || tag == "-" {
				t.Errorf("%s.%s has no JSON name", typ.Name(), field.Name)
			}
		}
	}
}

func TestChangesKeepOnlyChangedFields(t *testing.T) {
	c := Changes{}
	c.Add("name", "Acme", "Acme Inc")
	c.Add("status", "Planned", "Planned")
	if len(c) != 1 || c["name"] != (Change{From: "Acme", To: "Acme Inc"}) {
		t.Fatalf("changes: %+v", c)
	}
}

func TestRequestMeta(t *testing.T) {
	ctx := context.Background()
	if Actor(ctx) != 0 || UserAgent(ctx) != "" {
		t.Fatal("empty context has meta")
	}
	ctx = WithUserAgent(ctx, strings.Repeat("é", 300)+"\xff")
	ctx = WithActor(ctx, 7)
	if Actor(ctx) != 7 {
		t.Fatal("actor lost")
	}
	agent := UserAgent(ctx)
	if !utf8.ValidString(agent) || utf8.RuneCountInString(agent) != maxUserAgent {
		t.Fatalf("user agent not cut to %d valid characters: %d", maxUserAgent, utf8.RuneCountInString(agent))
	}
}
