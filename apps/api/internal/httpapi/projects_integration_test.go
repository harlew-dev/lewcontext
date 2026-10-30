package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"forma/api/internal/store"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

// signUp registers a user through the API and returns their ID and session cookie.
func signUp(t *testing.T, h http.Handler, email string) (int64, *http.Cookie) {
	t.Helper()
	w := call(t, h, "POST", "/api/auth/register", fmt.Sprintf(`{"name":"Test User","email":%q,"password":"correct horse battery staple"}`, email), "", nil, 201)
	var res response
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	return res.User.ID, w.Result().Cookies()[0]
}
func listProjects(t *testing.T, h http.Handler, cookie *http.Cookie, query string) store.Page[store.Project] {
	t.Helper()
	var page store.Page[store.Project]
	if err := json.Unmarshal(call(t, h, "GET", "/api/projects?"+query, "", "", cookie, 200).Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	return page
}
func projectIDs(projects []store.Project) []int64 {
	ids := make([]int64, len(projects))
	for i, p := range projects {
		ids[i] = p.ID
	}
	return ids
}

// seedProjects inserts n projects for userID. Pairs share an updated_at so
// ordering has to fall back to the ID tiebreak.
func seedProjects(t *testing.T, s *Server, userID int64, n int) {
	t.Helper()
	statuses := []string{"Planned", "In progress", "Completed"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	projects := make([]store.Project, n)
	for i := range projects {
		projects[i] = store.Project{
			UserID:    userID,
			Name:      fmt.Sprintf("Project %02d", i),
			Status:    statuses[i%3],
			Color:     "blue",
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
			UpdatedAt: base.Add(time.Duration(i/2) * time.Minute),
		}
	}
	if _, err := s.Store.DB.NewInsert().Model(&projects).Exec(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestProjectPagination(t *testing.T) {
	s, _ := integrationServer(t)
	h := s.Routes()
	owner, cookie := signUp(t, h, "pages@example.com")
	_, otherCookie := signUp(t, h, "other@example.com")
	seedProjects(t, s, owner, 45)

	t.Run("pages cover every project once for each sort", func(t *testing.T) {
		for _, sort := range []string{"-updated", "updated", "-created", "created", "name", "-name", "status", "-status"} {
			var seen []int64
			for page := 1; page <= 3; page++ {
				got := listProjects(t, h, cookie, fmt.Sprintf("sort=%s&page=%d", url.QueryEscape(sort), page))
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
		got := listProjects(t, h, cookie, "perPage=3")
		if names := []string{got.Data[0].Name, got.Data[1].Name, got.Data[2].Name}; !slices.Equal(names, []string{"Project 44", "Project 43", "Project 42"}) {
			t.Fatalf("unexpected order: %v", names)
		}
	})
	t.Run("status sorts in workflow order", func(t *testing.T) {
		first := listProjects(t, h, cookie, "sort=status&perPage=1").Data[0]
		last := listProjects(t, h, cookie, "sort=-status&perPage=1").Data[0]
		if first.Status != "Planned" || last.Status != "Completed" {
			t.Fatalf("status order: first %q last %q", first.Status, last.Status)
		}
	})
	t.Run("page past the end is empty with real totals", func(t *testing.T) {
		got := listProjects(t, h, cookie, "page=9")
		if len(got.Data) != 0 || got.Pages != (store.Pages{Current: 9, Total: 3, PerPage: 20, Items: 45}) {
			t.Fatalf("past the end: %+v", got)
		}
	})
	t.Run("filters by status and search", func(t *testing.T) {
		if got := listProjects(t, h, cookie, "status=Planned&perPage=100"); got.Pages.Items != 15 {
			t.Fatalf("status filter: want 15 got %d", got.Pages.Items)
		}
		got := listProjects(t, h, cookie, "q=project+0")
		if got.Pages.Items != 10 {
			t.Fatalf("search: want 10 got %d", got.Pages.Items)
		}
		if got := listProjects(t, h, cookie, "q=PROJECT+44&status=Planned"); got.Pages.Items != 0 {
			t.Fatal("search and status not combined")
		}
	})
	t.Run("search treats wildcards and quotes literally", func(t *testing.T) {
		names := map[string]string{"%": `50% done`, "_": `under_score`, `\`: `back\slash`, "'": `it's quoted`}
		for _, name := range names {
			in := store.ProjectInput{Name: name, Status: "Planned", Color: "rose"}
			if _, err := s.Store.CreateProject(context.Background(), owner, in); err != nil {
				t.Fatal(err)
			}
		}
		for term, name := range names {
			got := listProjects(t, h, cookie, "q="+url.QueryEscape(term))
			if got.Pages.Items != 1 || got.Data[0].Name != name {
				t.Fatalf("search %q: want only %q, got %d items", term, name, got.Pages.Items)
			}
		}
	})
	t.Run("other users see nothing", func(t *testing.T) {
		got := listProjects(t, h, otherCookie, "q=Project")
		if got.Pages.Items != 0 || len(got.Data) != 0 {
			t.Fatalf("cross-user leak: %+v", got.Pages)
		}
	})
	t.Run("rejects invalid parameters", func(t *testing.T) {
		for _, query := range []string{"page=0", "page=10001", "perPage=101", "page=1&page=2", "sort=user_id", "status=Archived", "q=%00", "owner=1", "q=" + strings.Repeat("a", 101)} {
			call(t, h, "GET", "/api/projects?"+query, "", "", cookie, 400)
		}
		call(t, h, "GET", "/api/projects?page=0", "", "", nil, 401)
	})
}
