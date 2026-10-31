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
	good := store.ProjectInput{Name: " A project ", Status: "Planned", Color: "violet"}
	if !validProject(&good) || good.Name != "A project" {
		t.Fatal("valid project rejected")
	}
	for _, in := range []store.ProjectInput{{Name: "", Status: "Planned", Color: "violet"}, {Name: "Test", Status: "Unknown", Color: "violet"}, {Name: "Test", Status: "Planned", Color: "red"}, {Name: strings.Repeat("x", 101), Status: "Planned", Color: "violet"}} {
		if validProject(&in) {
			t.Fatal("invalid project accepted")
		}
	}
}

func TestWorkspaceValidation(t *testing.T) {
	good := store.WorkspaceInput{Name: "  Acme  "}
	if !validWorkspace(&good) || good.Name != "Acme" {
		t.Fatal("valid workspace rejected")
	}
	for _, name := range []string{"", "   ", strings.Repeat("é", 81), "tab\there"} {
		in := store.WorkspaceInput{Name: name}
		if validWorkspace(&in) {
			t.Fatalf("invalid workspace name %q accepted", name)
		}
	}
	if !validWorkspace(&store.WorkspaceInput{Name: strings.Repeat("é", 80)}) {
		t.Fatal("80-character name rejected")
	}
}

// Fake data has to be data the API would accept, or tests exercise states
// real users can't reach.
func TestFactoriesMatchValidation(t *testing.T) {
	f := factory.TestFaker(t)
	for _, p := range factory.Projects.Make(f, 500) {
		in := store.ProjectInput{Name: p.Name, Description: p.Description, Status: p.Status, Color: p.Color}
		if !validProject(&in) || in.Name != p.Name || in.Description != p.Description {
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
