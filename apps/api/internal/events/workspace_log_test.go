package events

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// hiddenAreas are the areas the workspace log never shows: other accounts'
// sign-in and security events, the admin dashboard, alerts, servers,
// retention, blocked addresses and rate limits. auth.api_key is the one
// auth exception, in workspaceLog.
var hiddenAreas = []string{"auth", "admin", "alert", "server", "prune", "email", "rate_limit"}

// hiddenFromWorkspaceLog are events of a shown area that the log still
// leaves out, with why.
var hiddenFromWorkspaceLog = map[string]string{
	"workspace.invitation.sent": "delivery plumbing: the invitation's creation is the fact",
}

// A new event is hidden from the workspace log until someone puts it in
// workspaceLog or lists it here with a reason.
func TestEveryEventIsClassifiedForTheWorkspaceLog(t *testing.T) {
	for _, name := range Names() {
		area, _, _ := strings.Cut(name, ".")
		switch {
		case InWorkspaceLog(name):
			if _, hidden := hiddenFromWorkspaceLog[name]; hidden {
				t.Errorf("%s is both shown and hidden", name)
			}
		case slices.Contains(hiddenAreas, area):
		case hiddenFromWorkspaceLog[name] != "":
		default:
			t.Errorf("%s: classify it for the workspace log (workspaceLog, or hiddenFromWorkspaceLog with a reason)", name)
		}
	}
	// The shown personal key events are the only auth ones.
	for _, name := range WorkspaceLogNames() {
		if strings.HasPrefix(name, "auth.") && !strings.HasPrefix(name, "auth.api_key.") {
			t.Errorf("%s shown in the workspace log", name)
		}
	}
}

// Every field the log copies exists in its event's payload, and none can
// hold an IP, user agent or request ID.
func TestWorkspaceLogFieldsAreRealAndSafe(t *testing.T) {
	for _, e := range Catalogue {
		fields, ok := workspaceLog[e.EventName()]
		if !ok {
			continue
		}
		tags := map[string]bool{}
		typ := reflect.TypeOf(e)
		for i := range typ.NumField() {
			tags[strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]] = true
		}
		for _, f := range fields {
			if !tags[f] {
				t.Errorf("%s: payload has no field %q", e.EventName(), f)
			}
			if slices.Contains([]string{"ip", "userAgent", "request", "workspace", "token"}, f) {
				t.Errorf("%s: %q must not be shown", e.EventName(), f)
			}
		}
	}
	if len(workspaceLog) != len(WorkspaceLogNames()) {
		t.Error("a workspaceLog name isn't in the catalogue")
	}
}

func TestWorkspaceLogDetails(t *testing.T) {
	payload := json.RawMessage(`{"workspace":"wsp_x","project":"prj_x","changes":{"name":{"from":"A","to":"B"}},"request":"evt_y","email":"a@example.test"}`)
	got, err := WorkspaceLogDetails("project.updated", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["project"] != "prj_x" || got["changes"] == nil {
		t.Fatalf("details: %v", got)
	}
	// A pseudonymised or erased address is removed, and says so.
	for _, email := range []string{"hmac:0123", "[deleted]"} {
		got, _ = WorkspaceLogDetails("workspace.invitation.created", json.RawMessage(`{"invitation":"inv_x","role":"viewer","email":"`+email+`"}`))
		if _, has := got["email"]; has || got["emailRemoved"] != true {
			t.Fatalf("%s: %v", email, got)
		}
	}
	if got, _ := WorkspaceLogDetails("auth.logged_in", payload); len(got) != 0 {
		t.Fatalf("a hidden event projects to %v", got)
	}
}

func TestWorkspaceLogGroups(t *testing.T) {
	seen := map[string]string{}
	for id, names := range WorkspaceLogGroups() {
		for _, n := range names {
			if other, dup := seen[n]; dup {
				t.Errorf("%s is in %s and %s", n, id, other)
			}
			seen[n] = id
		}
	}
	if len(seen) != len(workspaceLog) {
		t.Errorf("groups hold %d names, the log %d", len(seen), len(workspaceLog))
	}
	for _, g := range WorkspaceLogGroupLabels {
		if len(WorkspaceLogGroups()[g.ID]) == 0 {
			t.Errorf("group %s is empty or unknown", g.ID)
		}
	}
}
