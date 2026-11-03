package workspace_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
)

func machineCall(h http.Handler, method, path, secret, body, retry string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:12345"
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if retry != "" {
		r.Header.Set("Idempotency-Key", retry)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestApiKeyManagementAndSelectedProjects(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	owner := apitest.SignedIn(t, db, f)
	w := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	projects := apitest.Must(factory.ProjectsIn(w.ID).Create(ctx, db, f, 3))(t)
	path := apitest.WorkspacePath(w.PublicID, "/api-keys")
	body := fmt.Sprintf(`{"name":"Integration","projectAccess":"selected","projects":[%q],"scopes":["project.index","project.show","project.update"],"expiresAt":null,"password":%q}`, projects[0].PublicID, factory.DefaultPassword)
	created := apitest.Call(t, h, "POST", path, body, owner.CSRFToken, owner.Cookie(), 201)
	var issued struct {
		Key    store.ApiKey `json:"key"`
		Secret string       `json:"secret"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	if issued.Secret == "" || issued.Key.ExpiresAt != nil {
		t.Fatal("missing creation secret or Never expiry")
	}
	list := apitest.Call(t, h, "GET", path, "", "", owner.Cookie(), 200)
	if strings.Contains(list.Body.String(), issued.Secret) || strings.Contains(list.Body.String(), "tokenHash") {
		t.Fatal("secret leaked in metadata")
	}
	base := "/api/v1/workspaces/" + w.PublicID + "/projects"
	listed := machineCall(h, "GET", base, issued.Secret, "", "")
	if listed.Code != 200 {
		t.Fatalf("list: %d %s", listed.Code, listed.Body.String())
	}
	var page store.Page[store.ApiProject]
	if err := json.Unmarshal(listed.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Pages.Items != 1 || page.Data[0].PublicID != projects[0].PublicID {
		t.Fatal("selection leaked totals or rows")
	}
	for _, id := range []string{projects[1].PublicID, "prj_00000000000000"} {
		if got := machineCall(h, "GET", base+"/"+id, issued.Secret, "", ""); got.Code != 404 {
			t.Fatalf("out of scope: %d", got.Code)
		}
	}
	projectBody := `{"name":"Changed","description":"","status":"Planned","color":"blue"}`
	if got := machineCall(h, "PATCH", base+"/"+projects[0].PublicID, issued.Secret, projectBody, ""); got.Code != 200 {
		t.Fatalf("machine write without Origin/CSRF: %d %s", got.Code, got.Body.String())
	}
	if got := machineCall(h, "POST", base, issued.Secret, projectBody, ""); got.Code != 403 {
		t.Fatalf("selected create: %d", got.Code)
	}
	if got := machineCall(h, "GET", base, "", "", ""); got.Code != 401 || got.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatal("missing bearer accepted")
	}
	for _, route := range []string{"/api/auth/me", path, "/api/admin/users"} {
		if got := machineCall(h, "GET", route, issued.Secret, "", ""); got.Code == 200 {
			t.Fatalf("bearer crossed session boundary: %s", route)
		}
	}
	apitest.Call(t, h, "DELETE", path+"/"+issued.Key.PublicID, "", owner.CSRFToken, owner.Cookie(), 204)
	apitest.Call(t, h, "DELETE", path+"/"+issued.Key.PublicID, "", owner.CSRFToken, owner.Cookie(), 204)
	if got := machineCall(h, "GET", base, issued.Secret, "", ""); got.Code != 401 {
		t.Fatal("revoked key still works")
	}
	var row factory.ApiKey
	if err := db.NewSelect().Model(&row).Where("ak.public_id = ?", issued.Key.PublicID).Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if row.TokenHash != "" || row.RevokedAt == nil {
		t.Fatal("revocation did not clear digest")
	}
}

func TestApiProjectOptionalIdempotency(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	w := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	body := fmt.Sprintf(`{"name":"Create integration","projectAccess":"all","projects":[],"scopes":["project.store","project.show","project.destroy"],"expiresAt":null,"password":%q}`, factory.DefaultPassword)
	created := apitest.Call(t, h, "POST", apitest.WorkspacePath(w.PublicID, "/api-keys"), body, owner.CSRFToken, owner.Cookie(), 201)
	var issued struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspaces/" + w.PublicID + "/projects"
	project := `{"name":"Duplicate-safe","description":"Original","status":"Planned","color":"blue"}`
	results := make(chan *httptest.ResponseRecorder, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- machineCall(h, "POST", base, issued.Secret, project, "retry-project-0001")
		}()
	}
	wg.Wait()
	close(results)
	response := ""
	for got := range results {
		if got.Code != 201 {
			t.Fatalf("create: %d %s", got.Code, got.Body.String())
		}
		if response != "" && response != got.Body.String() {
			t.Fatal("concurrent retry changed response")
		}
		response = got.Body.String()
	}
	if got := machineCall(h, "POST", base, issued.Secret, strings.Replace(project, "Original", "Different", 1), "retry-project-0001"); got.Code != 409 {
		t.Fatal("changed retry input accepted")
	}
	if got := machineCall(h, "POST", base, issued.Secret, project, ""); got.Code != 201 {
		t.Fatalf("optional header: %d %s", got.Code, got.Body.String())
	}
	var original store.ApiProject
	if err := json.Unmarshal([]byte(response), &original); err != nil {
		t.Fatal(err)
	}
	if got := machineCall(h, "DELETE", base+"/"+original.PublicID, issued.Secret, "", ""); got.Code != 204 {
		t.Fatal("delete failed")
	}
	if got := machineCall(h, "POST", base, issued.Secret, project, "retry-project-0001"); got.Code != 409 {
		t.Fatal("deleted response replayed")
	}
	if n, err := db.NewSelect().Model((*factory.Project)(nil)).Where("p.workspace_id = ?", w.ID).Count(context.Background()); err != nil || n != 1 {
		t.Fatalf("duplicate project count: %d %v", n, err)
	}
}

func TestPersonalApiKeyLiveAuthority(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	ctx := context.Background()
	manager := apitest.SignedIn(t, db, f)
	w := apitest.OwnedWorkspace(t, db, f, manager.UserID)
	member := apitest.SignedIn(t, db, f)
	apitest.Must(factory.MemberOf(ctx, db, f, member.UserID, w.ID, auth.RoleMember))(t)
	project := apitest.Must(factory.ProjectsIn(w.ID).CreateOne(ctx, db, f))(t)
	body := fmt.Sprintf(`{"workspace":%q,"name":"My script","projectAccess":"all","projects":[],"scopes":["project.index","project.show","project.update"],"expiresAt":null,"password":%q}`, w.PublicID, factory.DefaultPassword)
	created := apitest.Call(t, h, "POST", "/api/auth/api-keys", body, member.CSRFToken, member.Cookie(), 201)
	var issued struct {
		Key    store.ApiKey `json:"key"`
		Secret string       `json:"secret"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &issued); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspaces/" + w.PublicID + "/projects"
	edit := `{"name":"From script","description":"","status":"Completed","color":"blue"}`
	if got := machineCall(h, "PATCH", base+"/"+project.PublicID, issued.Secret, edit, ""); got.Code != 200 {
		t.Fatalf("personal write: %d %s", got.Code, got.Body.String())
	}
	userID := apitest.PublicIDOf(t, db, member.UserID)
	apitest.Call(t, h, "PATCH", apitest.WorkspacePath(w.PublicID, "/members/"+userID), `{"role":"viewer"}`, manager.CSRFToken, manager.Cookie(), 200)
	if got := machineCall(h, "PATCH", base+"/"+project.PublicID, issued.Secret, edit, ""); got.Code != 403 {
		t.Fatal("demotion did not narrow access")
	}
	if got := machineCall(h, "GET", base, issued.Secret, "", ""); got.Code != 200 {
		t.Fatal("demoted read refused")
	}
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(w.PublicID, "/members/"+userID), "", manager.CSRFToken, manager.Cookie(), 204)
	apitest.Must(factory.MemberOf(ctx, db, f, member.UserID, w.ID, auth.RoleMember))(t)
	if got := machineCall(h, "GET", base, issued.Secret, "", ""); got.Code != 401 {
		t.Fatal("rejoining revived key")
	}
}
