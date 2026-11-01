package events

import (
	"slices"
	"strings"
	"testing"
)

// Every catalogue name is in exactly one group; IDs are unique; admin-reads
// is exactly the admin reads, and admin.* holds none of them; a sub-group
// holds two or more names.
func TestEventGroups(t *testing.T) {
	seen := map[string]string{}
	ids := map[string]bool{}
	var check func(parent string, groups []Group)
	check = func(parent string, groups []Group) {
		for _, g := range groups {
			if ids[g.ID] {
				t.Errorf("group %s listed twice", g.ID)
			}
			ids[g.ID] = true
			if g.Label == "" {
				t.Errorf("group %s has no label", g.ID)
			}
			if slices.Contains(Names(), g.ID) {
				t.Errorf("group %s clashes with a name", g.ID)
			}
			if parent != "" {
				if !strings.HasPrefix(g.ID, strings.TrimSuffix(parent, "*")) || len(g.All()) < 2 || len(g.Groups) != 0 {
					t.Errorf("sub-group %s of %s: %v", g.ID, parent, g.All())
				}
			}
			for _, name := range g.Names {
				if other, ok := seen[name]; ok {
					t.Errorf("%s is in %s and %s", name, other, g.ID)
				}
				seen[name] = g.ID
				if g.ID != AdminReadsGroup && !strings.HasPrefix(name, strings.TrimSuffix(g.ID, "*")) {
					t.Errorf("%s isn't under %s", name, g.ID)
				}
			}
			check(g.ID, g.Groups)
		}
	}
	groups := Groups()
	check("", groups)
	for _, name := range Names() {
		if _, ok := seen[name]; !ok {
			t.Errorf("%s is in no group", name)
		}
	}
	all := GroupNames()
	if reads := all[AdminReadsGroup]; !slices.Equal(reads, AdminReadNames()) || len(reads) != 14 {
		t.Errorf("admin-reads: %v", reads)
	}
	for _, name := range all["admin.*"] {
		if AdminRead(name) {
			t.Errorf("admin.* holds the read %s", name)
		}
	}
	if !slices.Contains(all["admin.*"], "admin.email.unblocked") || !slices.Equal(all["admin.session.*"], []string{"admin.session.moved", "admin.session.ended"}) {
		t.Errorf("admin.*: %v", all["admin.*"])
	}
	if !slices.Equal(all["admin.user.*"], []string{"admin.user.signed_out", "admin.user.mfa_reset", "admin.user.suspended", "admin.user.unsuspended", "admin.user.erased", "admin.user.exported"}) || !slices.Equal(all["admin.ip.*"], []string{"admin.ip.blocked", "admin.ip.unblocked"}) {
		t.Errorf("the actions' groups: %v %v", all["admin.user.*"], all["admin.ip.*"])
	}
	if !slices.Equal(all["auth.mfa.*"], []string{"auth.mfa.enabled", "auth.mfa.disabled", "auth.mfa.checked"}) {
		t.Errorf("auth.mfa.*: %v", all["auth.mfa.*"])
	}
	// One name under a prefix isn't a sub-group.
	if _, ok := all["auth.recovery_codes.*"]; ok || seen["auth.recovery_codes.created"] != "auth.*" {
		t.Errorf("auth.recovery_codes.created is in %s", seen["auth.recovery_codes.created"])
	}
	if groups[0].ID != "auth.*" || groups[len(groups)-1].ID != AdminReadsGroup {
		t.Errorf("groups out of catalogue order: %s … %s", groups[0].ID, groups[len(groups)-1].ID)
	}
	for _, name := range []string{"auth.login_attempted", "admin.user.viewed", "admin.logged_in", "rate_limit.reached"} {
		if want := map[string]string{"auth.login_attempted": "auth.*", "admin.user.viewed": AdminReadsGroup, "admin.logged_in": "admin.*", "rate_limit.reached": "rate_limit.*"}[name]; AreaOf(name) != want {
			t.Errorf("AreaOf(%s) = %s, want %s", name, AreaOf(name), want)
		}
	}
}
