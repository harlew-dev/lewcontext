package events

import (
	"slices"
	"strings"
)

// Groups of event names, as the admin dashboard's Events page picks them
// (docs/api/events.md#groups): one per area of the catalogue (auth.*,
// workspace.*, …), each holding sub-groups (auth.mfa.*) and single names,
// plus admin-reads, the admin.<resource>.listed and .viewed names, which
// admin.* leaves out. Built from the catalogue, so a new event joins its
// group without anyone remembering.

// Group is a set of names picked together. Its ID is "<prefix>.*" (or
// AdminReadsGroup); Names are the names directly in it, Groups its
// sub-groups. Every catalogue name is in exactly one group, at one level.
type Group struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	Names  []string `json:"names"`
	Groups []Group  `json:"groups"`
}

// AdminReadsGroup is the group of admin reads. It can't clash with a name:
// the events.name CHECK allows no "-".
const AdminReadsGroup = "admin-reads"

// groupLabels name the groups for people; a group without one is shown by
// its ID.
var groupLabels = map[string]string{
	"auth.*":                    "Accounts and sign-in",
	"auth.password_reset.*":     "Password resets",
	"auth.registration.*":       "Sign-ups",
	"auth.email_verification.*": "Email verification",
	"auth.mfa.*":                "Two-factor authentication",
	"workspace.*":               "Workspaces",
	"workspace.member.*":        "Members",
	"workspace.invitation.*":    "Invitations",
	"project.*":                 "Projects",
	"rate_limit.*":              "Rate limits",
	"server.*":                  "Servers",
	"email.*":                   "Email addresses",
	"prune.*":                   "Retention",
	"admin.*":                   "Admin",
	"admin.session.*":           "Admin sessions",
	"admin.user.*":              "Actions on accounts",
	"admin.ip.*":                "IP blocks",
	AdminReadsGroup:             "Admin reads",
}

// Names are every catalogue name, in catalogue order.
func Names() []string {
	names := make([]string, len(Catalogue))
	for i, e := range Catalogue {
		names[i] = e.EventName()
	}
	return names
}

// AdminRead reports whether name records what an admin read
// (admin.<resource>.listed or .viewed).
func AdminRead(name string) bool {
	parts := strings.Split(name, ".")
	return len(parts) == 3 && parts[0] == "admin" && (parts[2] == "listed" || parts[2] == "viewed")
}

// AdminReadNames are the admin reads, in catalogue order.
func AdminReadNames() []string {
	var names []string
	for _, name := range Names() {
		if AdminRead(name) {
			names = append(names, name)
		}
	}
	return names
}

// Groups builds the groups from the catalogue, in its order: one per area,
// then admin-reads.
func Groups() []Group {
	var areas []string
	byArea := map[string][]string{}
	for _, name := range Names() {
		if AdminRead(name) {
			continue
		}
		area, _, _ := strings.Cut(name, ".")
		if _, ok := byArea[area]; !ok {
			areas = append(areas, area)
		}
		byArea[area] = append(byArea[area], name)
	}
	groups := make([]Group, 0, len(areas)+1)
	for _, area := range areas {
		groups = append(groups, areaGroup(area, byArea[area]))
	}
	return append(groups, group(AdminReadsGroup, AdminReadNames(), nil))
}

// areaGroup groups an area's names: a three-segment prefix holding two or
// more of them is a sub-group (auth.mfa.*), listed first; the rest are
// single names.
func areaGroup(area string, names []string) Group {
	var prefixes []string
	byPrefix := map[string][]string{}
	for _, name := range names {
		if parts := strings.Split(name, "."); len(parts) == 3 {
			prefix := parts[0] + "." + parts[1]
			if _, ok := byPrefix[prefix]; !ok {
				prefixes = append(prefixes, prefix)
			}
			byPrefix[prefix] = append(byPrefix[prefix], name)
		}
	}
	var subs []Group
	var grouped []string
	for _, prefix := range prefixes {
		if len(byPrefix[prefix]) >= 2 {
			subs = append(subs, group(prefix+".*", byPrefix[prefix], nil))
			grouped = append(grouped, byPrefix[prefix]...)
		}
	}
	var singles []string
	for _, name := range names {
		if !slices.Contains(grouped, name) {
			singles = append(singles, name)
		}
	}
	return group(area+".*", singles, subs)
}

func group(id string, names []string, subs []Group) Group {
	label := groupLabels[id]
	if label == "" {
		label = id
	}
	if names == nil {
		names = []string{}
	}
	if subs == nil {
		subs = []Group{}
	}
	return Group{ID: id, Label: label, Names: names, Groups: subs}
}

// All is every name in the group and its sub-groups, sub-groups first.
func (g Group) All() []string {
	var names []string
	for _, sub := range g.Groups {
		names = append(names, sub.All()...)
	}
	return append(names, g.Names...)
}

// GroupNames maps every group's ID, sub-groups included, to its names.
func GroupNames() map[string][]string {
	out := map[string][]string{}
	var walk func([]Group)
	walk = func(groups []Group) {
		for _, g := range groups {
			out[g.ID] = g.All()
			walk(g.Groups)
		}
	}
	walk(Groups())
	return out
}

// AreaOf is the top-level group a name is in: "auth.*", or AdminReadsGroup.
func AreaOf(name string) string {
	if AdminRead(name) {
		return AdminReadsGroup
	}
	area, _, _ := strings.Cut(name, ".")
	return area + ".*"
}
