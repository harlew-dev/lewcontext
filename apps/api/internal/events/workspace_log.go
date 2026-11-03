package events

import (
	"encoding/json"
	"slices"
	"strings"
)

// The workspace log: what owners and admins read in a workspace's Logs tab
// (docs/api/events.md#the-workspace-log). The admin dashboard shows an
// event's payload as stored; the workspace log shows a projection of it
// instead. A name is in WorkspaceLog, with the payload fields to copy, or
// it is invisible, so a new event stays hidden until someone classifies it
// (TestEveryEventIsClassifiedForTheWorkspaceLog).

// workspaceLog lists the events a workspace's log shows and, for each, the
// payload fields it copies. Every field is a public ID, a value of
// workspace content (a project's name) or a bounded fact; never an address
// apart from an invitation's email, and never an IP, user agent or request
// ID (they're columns, not payload).
var workspaceLog = map[string][]string{
	"workspace.created": {"name"},
	"workspace.renamed": {"changes"},
	"workspace.deleted": {"name", "projects", "members"},

	"workspace.member.updated": {"user", "changes"},
	"workspace.member.removed": {"user", "role"},
	"workspace.member.left":    {"role"},

	"workspace.invitation.created":  {"invitation", "email", "role"},
	"workspace.invitation.revoked":  {"invitation", "email"},
	"workspace.invitation.accepted": {"invitation", "role"},

	"project.created": {"project", "name", "description", "status", "color"},
	"project.updated": {"project", "changes"},
	"project.deleted": {"project", "name"},

	"workspace.api_key.created": {"key", "scopes"},
	"workspace.api_key.updated": {"key", "scopes", "fields"},
	"workspace.api_key.revoked": {"key", "reason"},
	// A member's personal key for this workspace (recorded with its ID).
	"auth.api_key.created": {"key", "scopes"},
	"auth.api_key.updated": {"key", "scopes", "fields"},
	"auth.api_key.revoked": {"key", "reason"},
}

// workspaceLogLabels say each event in words, as the Logs tab does ("Created
// API key"), so a search for "api key" or "role" finds it by what it is, not
// only by the text in its payload. They are matched against, never shown.
var workspaceLogLabels = map[string]string{
	"workspace.created": "Created workspace",
	"workspace.renamed": "Renamed workspace",
	"workspace.deleted": "Deleted workspace",

	"workspace.member.updated": "Changed member role",
	"workspace.member.removed": "Removed member from workspace",
	"workspace.member.left":    "Member left workspace",

	"workspace.invitation.created":  "Invited member invitation",
	"workspace.invitation.revoked":  "Revoked invitation",
	"workspace.invitation.accepted": "Accepted invitation joined member",

	"project.created": "Created project",
	"project.updated": "Updated project",
	"project.deleted": "Deleted project",

	"workspace.api_key.created": "Created API key",
	"workspace.api_key.updated": "Changed API key",
	"workspace.api_key.revoked": "Revoked API key",
	"auth.api_key.created":      "Created personal API key",
	"auth.api_key.updated":      "Changed personal API key",
	"auth.api_key.revoked":      "Revoked personal API key",
}

// WorkspaceLogNamesMatching are the events in the log whose label or name
// contains word, ignoring case, in catalogue order: "key" finds every API
// key event, "role" the role changes. The name's underscores and dots count
// as spaces, so "api key" and "api_key" both match.
func WorkspaceLogNamesMatching(word string) []string {
	word = strings.ToLower(strings.TrimSpace(word))
	var names []string
	for _, name := range WorkspaceLogNames() {
		text := strings.ToLower(workspaceLogLabels[name] + " " + strings.NewReplacer("_", " ", ".", " ").Replace(name))
		if strings.Contains(text, word) || strings.Contains(strings.ToLower(name), word) {
			names = append(names, name)
		}
	}
	return names
}

// WorkspaceLogGroupLabels are the groups the log's Type filter offers, by
// ID. A group ID can't clash with an event name: names are dotted.
var WorkspaceLogGroupLabels = []struct{ ID, Label string }{
	{"workspace", "Workspace"},
	{"members", "Members"},
	{"invitations", "Invitations"},
	{"projects", "Projects"},
	{"api-keys", "API keys"},
}

// WorkspaceLogNames are the events the workspace log shows, in catalogue
// order.
func WorkspaceLogNames() []string {
	var names []string
	for _, name := range Names() {
		if _, ok := workspaceLog[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

// WorkspaceLogGroups maps each group ID to its names.
func WorkspaceLogGroups() map[string][]string {
	groups := map[string][]string{}
	for _, name := range WorkspaceLogNames() {
		var id string
		switch {
		case strings.HasPrefix(name, "workspace.member."):
			id = "members"
		case strings.HasPrefix(name, "workspace.invitation."):
			id = "invitations"
		case strings.HasPrefix(name, "project."):
			id = "projects"
		case strings.Contains(name, ".api_key."):
			id = "api-keys"
		default:
			id = "workspace"
		}
		groups[id] = append(groups[id], name)
	}
	return groups
}

// InWorkspaceLog reports whether the workspace log shows the event named name.
func InWorkspaceLog(name string) bool {
	_, ok := workspaceLog[name]
	return ok
}

// minimisedEmail reports an email the retention rules replaced: a keyed
// pseudonym (30 days) or an erased address.
func minimisedEmail(email string) bool {
	return strings.HasPrefix(email, "hmac:") || email == "[deleted]"
}

// WorkspaceLogDetails projects payload for the log: only the fields
// workspaceLog lists for name, nothing for a name that isn't in it. An
// invitation's address the retention rules replaced is left out, and
// emailRemoved says so.
func WorkspaceLogDetails(name string, payload json.RawMessage) (map[string]any, error) {
	fields, ok := workspaceLog[name]
	if !ok {
		return map[string]any{}, nil
	}
	var all map[string]any
	if err := json.Unmarshal(payload, &all); err != nil {
		return nil, err
	}
	details := map[string]any{}
	for _, field := range fields {
		value, ok := all[field]
		if !ok {
			continue
		}
		if email, isString := value.(string); field == "email" && isString && minimisedEmail(email) {
			details["emailRemoved"] = true
			continue
		}
		details[field] = value
	}
	return details, nil
}

// WorkspaceLogSearchFields are the payload fields a log search matches,
// besides the changes' values: only fields WorkspaceLogDetails can show.
func WorkspaceLogSearchFields() []string {
	fields := []string{}
	for _, copied := range workspaceLog {
		for _, f := range copied {
			if f != "changes" && f != "email" && f != "scopes" && f != "fields" && !slices.Contains(fields, f) {
				fields = append(fields, f)
			}
		}
	}
	slices.Sort(fields)
	return fields
}
