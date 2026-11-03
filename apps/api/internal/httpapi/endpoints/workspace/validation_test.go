package workspace

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestProjectValidation(t *testing.T) {
	good := store.ProjectInput{Name: " A project ", Description: "Line one\r\n\tline two", Status: "Planned", Color: "violet"}
	if _, _, ok := validProject(&good); !ok || good.Name != "A project" {
		t.Fatal("valid project rejected")
	}
	// Emoji joined with ZWJ and a variation selector are ordinary text.
	emoji := store.ProjectInput{Name: "👩\u200d💻 Launch", Description: "🏳\ufe0f\u200d🌈 week", Status: "Planned", Color: "violet"}
	if _, _, ok := validProject(&emoji); !ok {
		t.Fatal("emoji with a zero-width joiner rejected")
	}
	for _, c := range []struct {
		in    store.ProjectInput
		field string
	}{
		{store.ProjectInput{Name: "", Status: "Planned", Color: "violet"}, "name"},
		{store.ProjectInput{Name: strings.Repeat("x", 101), Status: "Planned", Color: "violet"}, "name"},
		{store.ProjectInput{Name: "nul\x00here", Status: "Planned", Color: "violet"}, "name"},
		{store.ProjectInput{Name: "tab\there", Status: "Planned", Color: "violet"}, "name"},
		{store.ProjectInput{Name: "Test", Description: strings.Repeat("x", 501), Status: "Planned", Color: "violet"}, "description"},
		{store.ProjectInput{Name: "Test", Description: "nul\x00here", Status: "Planned", Color: "violet"}, "description"},
		{store.ProjectInput{Name: "Test", Description: "escape\x1b[2J", Status: "Planned", Color: "violet"}, "description"},
		{store.ProjectInput{Name: "Test", Description: "bidi\u202eevil", Status: "Planned", Color: "violet"}, "description"},
		{store.ProjectInput{Name: "Test", Description: "zero\u200bwidth", Status: "Planned", Color: "violet"}, "description"},
		{store.ProjectInput{Name: "Test", Description: "line\u2028separator", Status: "Planned", Color: "violet"}, "description"},
		{store.ProjectInput{Name: "Test", Status: "Unknown", Color: "violet"}, ""},
		{store.ProjectInput{Name: "Test", Status: "Planned", Color: "red"}, ""},
	} {
		field, message, ok := validProject(&c.in)
		if ok || field != c.field || message == "" {
			t.Fatalf("%+v: want refused on %q, got ok=%v field=%q", c.in, c.field, ok, field)
		}
	}
}

func TestWorkspaceValidation(t *testing.T) {
	good := store.WorkspaceInput{Name: "  Acme  "}
	if !validWorkspace(&good) || good.Name != "Acme" {
		t.Fatal("valid workspace rejected")
	}
	for _, name := range []string{"", "   ", strings.Repeat("é", 81), "tab\there", "nul\x00", "esc\x1b", "rlo\u202eevil", "zero\u200bwidth", "line\u2028sep"} {
		in := store.WorkspaceInput{Name: name}
		if validWorkspace(&in) {
			t.Fatalf("invalid workspace name %q accepted", name)
		}
	}
	if !validWorkspace(&store.WorkspaceInput{Name: strings.Repeat("é", 80)}) {
		t.Fatal("80-character name rejected")
	}
	if !validWorkspace(&store.WorkspaceInput{Name: "👩\u200d💻 Studio"}) {
		t.Fatal("emoji with a zero-width joiner rejected")
	}
}

// Fake data has to be data the API would accept, or tests exercise states
// real users can't reach.
func TestFactoriesMatchValidation(t *testing.T) {
	f := factory.TestFaker(t)
	for _, p := range factory.Projects.Make(f, 500) {
		in := store.ProjectInput{Name: p.Name, Description: p.Description, Status: p.Status, Color: p.Color}
		if _, _, ok := validProject(&in); !ok || in.Name != p.Name || in.Description != p.Description {
			t.Fatalf("fake project rejected or changed by validation: %+v", p)
		}
	}
	for _, w := range factory.Workspaces.Make(f, 500) {
		in := store.WorkspaceInput{Name: w.Name}
		if !validWorkspace(&in) || in.Name != w.Name {
			t.Fatalf("fake workspace rejected or changed by validation: %+v", w)
		}
	}
	for _, m := range factory.WorkspaceUsers.Make(f, 50) {
		if _, ok := auth.ParseRole(string(m.Role)); !ok {
			t.Fatalf("fake membership has an unknown role: %+v", m)
		}
	}
	for _, i := range factory.WorkspaceInvitations.Make(f, 500) {
		email := i.Email
		if !httpapi.ValidEmail(&email) || email != i.Email || i.Role == auth.RoleOwner {
			t.Fatalf("fake invitation rejected or changed by validation: %+v", i)
		}
	}
	for _, u := range factory.Users.Make(f, 500) {
		in := httpapi.Credentials{Name: u.Name, Email: u.Email, Password: factory.DefaultPassword}
		if !httpapi.ValidCredentials(&in) || in.Email != u.Email || utf8.RuneCountInString(u.Name) > 80 {
			t.Fatalf("fake user rejected or changed by validation: %+v", u)
		}
	}
}
