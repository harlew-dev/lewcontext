package workspace_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func listProjects(t *testing.T, h http.Handler, cookie *http.Cookie, path, query string) store.Page[store.Project] {
	t.Helper()
	var page store.Page[store.Project]
	if err := json.Unmarshal(apitest.Call(t, h, "GET", path+"?"+query, "", "", cookie, 200).Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
func projectIDs(projects []store.Project) []string {
	ids := make([]string, len(projects))
	for i, p := range projects {
		ids[i] = p.PublicID
	}
	return ids
}

// seedProjects creates n fake projects in a workspace with predictable names,
// statuses and timestamps. Pairs share an updated_at, so ordering has to fall
// back to the ID tiebreak.
func seedProjects(t *testing.T, db bun.IDB, f *gofakeit.Faker, workspaceID int64, n int) {
	t.Helper()
	statuses := []string{"Planned", "In progress", "Completed"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := factory.ProjectsIn(workspaceID).Sequence(func(i int, p *factory.Project) {
		p.Name = fmt.Sprintf("Project %02d", i)
		p.Description = ""
		p.Status = statuses[i%3]
		p.CreatedAt = base.Add(time.Duration(i) * time.Hour)
		p.UpdatedAt = base.Add(time.Duration(i/2) * time.Minute)
	}).Create(context.Background(), db, f, n)
	if err != nil {
		t.Fatal(err)
	}
}
func TestProjectPagination(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	cookie := session.Cookie()
	workspace := apitest.OwnedWorkspace(t, db, f, session.UserID)
	path := apitest.WorkspacePath(workspace.PublicID, "/projects")
	other := apitest.SignedIn(t, db, f)
	otherPath := apitest.WorkspacePath(apitest.OwnedWorkspace(t, db, f, other.UserID).PublicID, "/projects")
	seedProjects(t, db, f, workspace.ID, 45)

	t.Run("pages cover every project once for each sort", func(t *testing.T) {
		for _, sort := range []string{"-updated", "updated", "-created", "created", "name", "-name", "status", "-status"} {
			var seen []string
			for page := 1; page <= 3; page++ {
				got := listProjects(t, h, cookie, path, fmt.Sprintf("sort=%s&page=%d", url.QueryEscape(sort), page))
				want := store.Pages{Current: page, Total: 3, PerPage: 20, Items: 45}
				if got.Pages != want {
					t.Fatalf("%s page %d: want %+v got %+v", sort, page, want, got.Pages)
				}
				seen = append(seen, projectIDs(got.Data)...)
			}
			unique := slices.Clone(seen)
			slices.Sort(unique)
			if len(seen) != 45 || len(slices.Compact(unique)) != 45 {
				t.Fatalf("%s: pages repeated or skipped projects: %v", sort, seen)
			}
		}
	})
	t.Run("default order is most recently updated", func(t *testing.T) {
		got := listProjects(t, h, cookie, path, "perPage=3")
		if names := []string{got.Data[0].Name, got.Data[1].Name, got.Data[2].Name}; !slices.Equal(names, []string{"Project 44", "Project 43", "Project 42"}) {
			t.Fatalf("unexpected order: %v", names)
		}
	})
	t.Run("status sorts in workflow order", func(t *testing.T) {
		first := listProjects(t, h, cookie, path, "sort=status&perPage=1").Data[0]
		last := listProjects(t, h, cookie, path, "sort=-status&perPage=1").Data[0]
		if first.Status != "Planned" || last.Status != "Completed" {
			t.Fatalf("status order: first %q last %q", first.Status, last.Status)
		}
	})
	t.Run("page past the end is empty with real totals", func(t *testing.T) {
		got := listProjects(t, h, cookie, path, "page=9")
		if len(got.Data) != 0 || got.Pages != (store.Pages{Current: 9, Total: 3, PerPage: 20, Items: 45}) {
			t.Fatalf("past the end: %+v", got)
		}
	})
	t.Run("filters by status and search", func(t *testing.T) {
		if got := listProjects(t, h, cookie, path, "status=Planned&perPage=100"); got.Pages.Items != 15 {
			t.Fatalf("status filter: want 15 got %d", got.Pages.Items)
		}
		got := listProjects(t, h, cookie, path, "q=project+0")
		if got.Pages.Items != 10 {
			t.Fatalf("search: want 10 got %d", got.Pages.Items)
		}
		if got := listProjects(t, h, cookie, path, "q=PROJECT+44&status=Planned"); got.Pages.Items != 0 {
			t.Fatal("search and status not combined")
		}
	})
	t.Run("search treats wildcards and quotes literally", func(t *testing.T) {
		names := map[string]string{"%": `50% done`, "_": `under_score`, `\`: `back\slash`, "'": `it's quoted`}
		for _, name := range names {
			_, err := factory.ProjectsIn(workspace.ID).State(func(p *factory.Project) {
				p.Name, p.Description = name, ""
			}).CreateOne(context.Background(), db, f)
			if err != nil {
				t.Fatal(err)
			}
		}
		for term, name := range names {
			got := listProjects(t, h, cookie, path, "q="+url.QueryEscape(term))
			if got.Pages.Items != 1 || got.Data[0].Name != name {
				t.Fatalf("search %q: want only %q, got %d items", term, name, got.Pages.Items)
			}
		}
	})
	t.Run("other workspaces see nothing", func(t *testing.T) {
		got := listProjects(t, h, other.Cookie(), otherPath, "q=Project")
		if got.Pages.Items != 0 || len(got.Data) != 0 {
			t.Fatalf("cross-user leak: %+v", got.Pages)
		}
	})
	t.Run("rejects invalid parameters", func(t *testing.T) {
		for _, query := range []string{"page=0", "page=10001", "perPage=101", "page=1&page=2", "sort=user_id", "status=Archived", "q=%00", "owner=1", "q=" + strings.Repeat("a", 101)} {
			apitest.Call(t, h, "GET", path+"?"+query, "", "", cookie, 400)
		}
		apitest.Call(t, h, "GET", path+"?page=0", "", "", nil, 401)
	})
}

// A NUL can't be stored by PostgreSQL, so it must be refused on its field
// before it reaches the database, rather than failing there as a 500.
func TestProjectRefusesControlCharacters(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, session.UserID)
	path := apitest.WorkspacePath(workspace.PublicID, "/projects")
	project := apitest.Must(factory.ProjectsIn(workspace.ID).CreateOne(context.Background(), db, f))(t)
	body := func(name, description string) string {
		b, err := json.Marshal(map[string]string{"name": name, "description": description, "status": "Planned", "color": "violet"})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, c := range []struct{ field, body string }{
		{"name", body("nul\u0000here", "")},
		{"description", body("Fine", "nul\u0000here")},
	} {
		for _, req := range []struct{ method, path string }{
			{"POST", path},
			{"PATCH", path + "/" + project.PublicID},
		} {
			w := apitest.Call(t, h, req.method, req.path, c.body, session.CSRFToken, session.Cookie(), 422)
			if got := apitest.ErrorField(t, w); got != c.field {
				t.Fatalf("%s %s with NUL in %s: field %q", req.method, req.path, c.field, got)
			}
		}
	}
	if got := listProjects(t, h, session.Cookie(), path, ""); got.Pages.Items != 1 || got.Data[0].Name != project.Name {
		t.Fatalf("want only the factory's project, unchanged, got %+v", got.Data)
	}
	// Line breaks and tabs are still fine in a description.
	apitest.Call(t, h, "POST", path, body("Fine", "Line one\n\tline two"), session.CSRFToken, session.Cookie(), 201)
}
