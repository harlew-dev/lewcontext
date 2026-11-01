package events

import (
	"fmt"
	"lewcontext/api/internal/auth"
	"slices"
	"strings"
	"testing"
)

// A three-segment event's resource (workspace.member in
// workspace.member.removed) is spelled as in its permission
// (docs/naming.md#events-internalevents), or is a stored resource with no
// permission, listed here with its reason. Admin reads name the resource
// read (admin.user.viewed): docs/naming.md's rule for the admin area.

// storedResources are the event resources no permission guards.
var storedResources = map[string]string{
	"auth.password_reset":     "a reset link (password_resets)",
	"auth.registration":       "a sign-up waiting for its code (pending_registrations)",
	"auth.email_verification": "an emailed verification code (email_verifications)",
	"auth.mfa":                "the account's authenticator (mfa_totp)",
	"auth.recovery_codes":     "the account's recovery codes (mfa_recovery_codes)",
	"admin.session":           "one admin session (admin_sessions)",
	"admin.email":             "an admin's action names what it acted on: an email address on the block list",
	"admin.user":              "an admin's action names what it acted on: an account (signed out, its MFA reset, suspended)",
	"admin.workspace":         "an admin's action names what it acted on: a workspace (its invitations revoked)",
	"admin.delivery":          "an admin's action names what it acted on: a delivery (event_deliveries) queued again",
	"admin.ip":                "an admin's action names what it acted on: a client address or range (ip_blocks)",
	"admin.admin":             "an admin's action names what it acted on: another admin (signed out)",
	"admin.event":             "an admin's action names what it acted on: the audit trail (events), exported as CSV",
}

// permissionResources are the resources the permissions name: a permission
// without its action.
func permissionResources() []string {
	var resources []string
	for _, p := range auth.RoleOwner.Permissions() {
		resource := string(p)[:strings.LastIndex(string(p), ".")]
		if !slices.Contains(resources, resource) {
			resources = append(resources, resource)
		}
	}
	return resources
}

// badEventResource says what's wrong with an event name's resource, or "".
func badEventResource(name string, resources []string) string {
	parts := strings.Split(name, ".")
	if len(parts) != 3 {
		return ""
	}
	resource := parts[0] + "." + parts[1]
	if slices.Contains(resources, resource) || storedResources[resource] != "" {
		return ""
	}
	// What an admin read: admin.<resource>.listed or .viewed.
	if parts[0] == "admin" && (parts[2] == "listed" || parts[2] == "viewed") {
		return ""
	}
	return fmt.Sprintf("event %q: its resource %q is neither a permission's resource nor a listed stored resource; spell it as in its permission, or list it with its reason; see docs/naming.md#events-internalevents", name, resource)
}

func TestEventResources(t *testing.T) {
	resources := permissionResources()
	if !slices.Contains(resources, "workspace.member") || !slices.Contains(resources, "project") {
		t.Fatalf("permission resources: %v", resources)
	}
	used := map[string]bool{}
	for _, name := range Names() {
		if problem := badEventResource(name, resources); problem != "" {
			t.Error(problem)
		}
		parts := strings.Split(name, ".")
		if len(parts) == 3 {
			used[parts[0]+"."+parts[1]] = true
		}
	}
	for resource := range storedResources {
		if !used[resource] {
			t.Errorf("%s is listed as a stored resource, but no event names it", resource)
		}
	}
}

// The check itself.
func TestBadEventResource(t *testing.T) {
	resources := []string{"workspace", "workspace.member", "project"}
	for _, name := range []string{"workspace.member.removed", "project.created", "auth.mfa.checked", "admin.user.viewed", "auth.logged_in"} {
		if problem := badEventResource(name, resources); problem != "" {
			t.Errorf("refused: %s", problem)
		}
	}
	for _, name := range []string{"workspace.membership.removed", "workspace.members.removed", "project.task.created", "admin.widget.deleted"} {
		if problem := badEventResource(name, resources); !strings.Contains(problem, "docs/naming.md#events-internalevents") {
			t.Errorf("%s accepted", name)
		}
	}
}
