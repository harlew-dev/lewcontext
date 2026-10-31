package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"
)

// eventIDs are the public IDs of a page's events, in order.
func eventIDs(page store.AdminEventPage) []string {
	ids := make([]string, len(page.Data))
	for i, e := range page.Data {
		ids[i] = e.PublicID
	}
	return ids
}

// listEvents loads the Events list with query and fails on anything but 200.
func listEvents(t *testing.T, h http.Handler, cookie *http.Cookie, query string) store.AdminEventPage {
	t.Helper()
	return decodeAdmin[store.AdminEventPage](t, adminCall(t, h, "GET", "/api/admin/events"+query, "", "", 200, cookie).Body.Bytes())
}

func TestAdminEvents(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	ada := apitest.Must(factory.Users.State(func(u *factory.User) { u.Name, u.Email = "Ada", "ada@example.test" }).CreateOne(ctx, db, f))(t)
	bob := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	acme := apitest.Must(factory.WorkspaceFor(ctx, db, f, ada.ID, factory.Named("Acme")))(t)
	now := time.Now().Truncate(time.Microsecond)
	event := func(name string, payload map[string]any, at time.Duration, scope func(*factory.Event)) factory.Event {
		e := factory.Events.State(factory.EventOf(name, payload)).State(func(e *factory.Event) {
			e.CreatedAt, e.IP = now.Add(-at), ""
			scope(e)
		})
		return apitest.Must(e.CreateOne(ctx, db, f))(t)
	}
	none := func(*factory.Event) {}
	attempt := event("auth.login_attempted", map[string]any{"email": "ada@example.test", "user": ada.PublicID, "result": "wrong_password"}, 5*time.Hour,
		func(e *factory.Event) { e.UserID, e.IP = ada.ID, "203.0.113.5" })
	created := event("workspace.created", map[string]any{"workspace": acme.PublicID, "name": "Acme"}, 4*time.Hour,
		func(e *factory.Event) { e.ActorID, e.WorkspaceID, e.IP = ada.ID, acme.ID, "198.51.100.7" })
	project := event("project.created", map[string]any{"project": "prj_2yEHYLwiwPVhLW", "workspace": acme.PublicID, "name": "Secret plan", "description": "<script>launch</script>", "status": "Planned", "color": "violet"}, 3*time.Hour,
		func(e *factory.Event) { e.ActorID, e.WorkspaceID = ada.ID, acme.ID })
	removed := event("workspace.member.removed", map[string]any{"workspace": acme.PublicID, "user": bob.PublicID, "role": "member"}, 2*time.Hour,
		func(e *factory.Event) { e.ActorID, e.UserID, e.WorkspaceID = ada.ID, bob.ID, acme.ID })
	limited := event("rate_limit.reached", map[string]any{"limit": "auth:per-ip:block", "window": 900}, time.Hour,
		func(e *factory.Event) { e.IP = "2001:db8:1:2::5" })
	// Three at the same moment, so the cursor's tie-break is tested.
	var tied []factory.Event
	for range 3 {
		tied = append(tied, event("rate_limit.reached", map[string]any{"limit": "auth:per-account:block", "window": 900}, 30*time.Minute, none))
	}
	// An account since deleted leaves only its usr_ in the payload.
	left := event("workspace.member.left", map[string]any{"workspace": acme.PublicID, "user": "usr_2yEHYLwiwPVhLW", "role": "viewer"}, 20*time.Minute,
		func(e *factory.Event) { e.WorkspaceID = acme.ID })
	viewed := event("admin.user.viewed", map[string]any{"admin": admin.User.PublicID, "user": ada.PublicID}, 10*time.Minute,
		func(e *factory.Event) { e.ActorID = admin.User.ID })

	newestFirst := []string{left.PublicID, tied[2].PublicID, tied[1].PublicID, tied[0].PublicID, limited.PublicID, removed.PublicID, project.PublicID, created.PublicID, attempt.PublicID}
	all := listEvents(t, h, cookie, "")
	if got := eventIDs(all); !slices.Equal(got, newestFirst) || all.Next != nil {
		t.Fatalf("every event but admin reads, newest first: %v, want %v (next %v)", got, newestFirst, all.Next)
	}

	// Pages follow the cursor: no row twice or missing, even sharing a time.
	var walked []string
	query := "?perPage=2"
	for pages := 0; ; pages++ {
		page := listEvents(t, h, cookie, query)
		walked = append(walked, eventIDs(page)...)
		if page.Next == nil {
			break
		}
		if pages > 10 {
			t.Fatal("the cursor never ends")
		}
		query = "?perPage=2&before=" + *page.Next
	}
	if !slices.Equal(walked, newestFirst) {
		t.Fatalf("walking pages: %v, want %v", walked, newestFirst)
	}

	// Each filter alone, and combined.
	for query, want := range map[string][]string{
		"?name=auth.login_attempted":               {attempt.PublicID},
		"?name=workspace.":                         {left.PublicID, removed.PublicID, created.PublicID},
		"?name=workspace.member.":                  {left.PublicID, removed.PublicID},
		"?account=" + ada.PublicID:                 {removed.PublicID, project.PublicID, created.PublicID, attempt.PublicID},
		"?account=" + bob.PublicID:                 {removed.PublicID},
		"?account=usr_2yEHYLwiwPVhLW":              {},
		"?workspace=" + acme.PublicID:              {left.PublicID, removed.PublicID, project.PublicID, created.PublicID},
		"?email=ADA%40example.test":                {attempt.PublicID},
		"?ip=203.0.113.5":                          {attempt.PublicID},
		"?ip=198.51.100.0%2F24":                    {created.PublicID},
		"?ip=2001%3Adb8%3A1%3A%3A%2F48":            {limited.PublicID},
		"?ip=10.0.0.0%2F16":                        {},
		"?name=project.&account=" + bob.PublicID:   {},
		"?name=workspace.&account=" + bob.PublicID: {removed.PublicID},
		"?from=" + url.QueryEscape(now.Add(-150*time.Minute).Format(time.RFC3339)) + "&to=" + url.QueryEscape(now.Add(-45*time.Minute).Format(time.RFC3339)): {limited.PublicID, removed.PublicID},
		"?name=admin.user.viewed": {viewed.PublicID},
	} {
		if got := eventIDs(listEvents(t, h, cookie, query)); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", query, got, want)
		}
	}
	reads := listEvents(t, h, cookie, "?adminReads=include&name=admin.")
	if len(reads.Data) < 2 || !slices.Contains(eventIDs(reads), viewed.PublicID) {
		t.Fatalf("admin reads asked for: %v", eventIDs(reads))
	}
	for _, query := range []string{
		"?name=auth.nope", "?name=auth", "?name=nope.", "?name=%25",
		"?account=wsp_2yEHYLwiwPVhLW", "?account=usr_x", "?workspace=usr_2yEHYLwiwPVhLW",
		"?email=nope", "?ip=nope", "?ip=10.0.0.0%2F8", "?ip=0.0.0.0%2F0", "?ip=2001%3Adb8%3A%3A%2F16",
		"?from=yesterday", "?to=2026-10-31", "?adminReads=yes", "?perPage=0", "?perPage=101",
		"?before=evt_x", "?before=evt_2yEHYLwiwPVhLW", "?page=2", "?name=auth.&name=workspace.",
	} {
		adminCall(t, h, "GET", "/api/admin/events"+query, "", "", 400, cookie)
	}

	// Rows name their references; payloads are shown as stored, project
	// names and descriptions included.
	rows := map[string]store.AdminEvent{}
	for _, e := range all.Data {
		rows[e.PublicID] = e
	}
	if e := rows[attempt.PublicID]; e.IP == nil || *e.IP != "203.0.113.5" || e.User == nil || e.User.Email != "ada@example.test" || e.Actor != nil {
		t.Fatalf("login attempt row: %+v", e)
	}
	p := rows[project.PublicID]
	var payload map[string]any
	apitest.Must(0, json.Unmarshal(p.Payload, &payload))(t)
	if p.Actor == nil || p.Actor.Name != "Ada" || p.Workspace == nil || p.Workspace.Name != "Acme" ||
		payload["name"] != "Secret plan" || payload["description"] != "<script>launch</script>" {
		t.Fatalf("project row: %+v %s", p, p.Payload)
	}
	if e := rows[left.PublicID]; e.User == nil || e.User.PublicID != "usr_2yEHYLwiwPVhLW" || e.User.Name != "" {
		t.Fatalf("a deleted account's row: %+v", e.User)
	}

	// A deleted workspace's events are still found, by the ID in their payload.
	apitest.Must(0, s.Repo.DeleteWorkspace(ctx, store.Workspace{ID: acme.ID, PublicID: acme.PublicID}, "Acme"))(t)
	gone := listEvents(t, h, cookie, "?workspace="+acme.PublicID)
	if len(gone.Data) != 5 || gone.Data[1].Workspace == nil || gone.Data[1].Workspace.PublicID != acme.PublicID || gone.Data[1].Workspace.Name != "" {
		t.Fatalf("a deleted workspace's events: %+v", gone.Data)
	}

	// Each list read is recorded with its filters, by the admin, from their IP.
	listed := apitest.EventsNamed(t, db, "admin.event.listed")
	var emailSearch *apitest.Event
	for i, e := range listed {
		if filters, _ := e.Payload["filters"].(map[string]any); filters["email"] == "ada@example.test" {
			emailSearch = &listed[i]
		}
	}
	if emailSearch == nil || !apitest.Is(emailSearch.ActorID, admin.User.ID) || emailSearch.UserID.Valid || emailSearch.WorkspaceID.Valid ||
		emailSearch.Payload["admin"] != admin.User.PublicID || emailSearch.IP != "127.0.0.1" {
		t.Fatalf("the email search's record: %+v", emailSearch)
	}
	apitest.CheckPayloadIDs(t, db)
}

// An event's detail: deliveries, the user agent and related events. Events
// recorded in a request keep its IP in events.ip; a listener's have none.
func TestAdminEventDetail(t *testing.T) {
	s, db, _, deliver := mailServer(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	ada := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)

	apitest.Call(t, h, "POST", "/api/auth/password-reset", fmt.Sprintf(`{"email":%q}`, ada.Email), "", nil, 202)
	deliver()
	requested := apitest.OnlyEvent(t, db, "auth.password_reset.requested")
	sent := apitest.OnlyEvent(t, db, "auth.password_reset.sent")
	if requested.IP != "127.0.0.1" || sent.IP != "" {
		t.Fatalf("events.ip: requested %q, sent by the listener %q", requested.IP, sent.IP)
	}
	page := listEvents(t, h, cookie, "?name=auth.password_reset.")
	if len(page.Data) != 2 || page.Data[1].Name != "auth.password_reset.requested" || page.Data[0].IP != nil {
		t.Fatalf("reset events: %+v", page.Data)
	}
	requestID, sentID := page.Data[1].PublicID, page.Data[0].PublicID

	detail := decodeAdmin[store.AdminEventDetail](t, adminCall(t, h, "GET", "/api/admin/events/"+requestID, "", "", 200, cookie).Body.Bytes())
	if detail.UserAgent != apitest.UserAgent || len(detail.Deliveries) != 1 || detail.Deliveries[0].Listener != "mail.password_reset" ||
		detail.Deliveries[0].Status != store.DeliveryDelivered || detail.Deliveries[0].DeliveredAt == nil || detail.Deliveries[0].Attempts != 1 {
		t.Fatalf("the request's detail: %+v", detail)
	}
	if len(detail.Related) != 1 || detail.Related[0].PublicID != sentID {
		t.Fatalf("the request's related events: %+v", detail.Related)
	}
	back := decodeAdmin[store.AdminEventDetail](t, adminCall(t, h, "GET", "/api/admin/events/"+sentID, "", "", 200, cookie).Body.Bytes())
	if len(back.Related) != 1 || back.Related[0].PublicID != requestID || len(back.Deliveries) != 0 || back.User == nil || back.User.PublicID != ada.PublicID {
		t.Fatalf("the sent event's detail: %+v", back)
	}
	for _, id := range []string{"evt_2yEHYLwiwPVhLW", "evt_x", ada.PublicID} {
		adminCall(t, h, "GET", "/api/admin/events/"+id, "", "", 404, cookie)
	}
	viewed := apitest.EventsNamed(t, db, "admin.event.viewed")
	if len(viewed) != 2 || viewed[0].Payload["event"] != requestID || viewed[0].Payload["admin"] != admin.User.PublicID || viewed[0].UserID.Valid {
		t.Fatalf("admin.event.viewed: %+v", viewed)
	}

	// Recorded outside a request (the CLI), an event has no IP.
	apitest.Must(0, s.Repo.Record(ctx, events.AdminRevoked{User: ada.PublicID}, store.EventScope{UserID: ada.ID}))(t)
	if e := apitest.OnlyEvent(t, db, "admin.revoked"); e.IP != "" {
		t.Fatalf("a CLI event's IP: %q", e.IP)
	}
}

// The catalogue lists the filters' names and isn't recorded.
func TestAdminCatalogue(t *testing.T) {
	s, db, _, _ := mailServer(t)
	h := router.New(s)
	_, session := adminSignedIn(t, db, factory.TestFaker(t))
	before := apitest.Must(db.NewSelect().TableExpr("events").Count(context.Background()))(t)
	type catalogue struct {
		Events    []string `json:"events"`
		Limits    []string `json:"limits"`
		Listeners []string `json:"listeners"`
	}
	c := decodeAdmin[catalogue](t, adminCall(t, h, "GET", "/api/admin/catalogue", "", "", 200, session.Cookie()).Body.Bytes())
	if !slices.Contains(c.Events, "auth.login_attempted") || !slices.Contains(c.Events, "admin.event.listed") ||
		!slices.Contains(c.Limits, "auth:per-ip") || !slices.Contains(c.Limits, "account-exists:per-account") ||
		!slices.Equal(c.Listeners, []string{"mail.password_reset", "mail.workspace_invitation"}) {
		t.Fatalf("catalogue: %+v", c)
	}
	if after := apitest.Must(db.NewSelect().TableExpr("events").Count(context.Background()))(t); after != before {
		t.Fatalf("reading the catalogue recorded %d events", after-before)
	}
}

// A workspace's projects, with names and descriptions, filtered as the app's
// list is.
func TestAdminWorkspaceProjects(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	cookie := session.Cookie()
	owner := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	acme := apitest.OwnedWorkspace(t, db, f, owner.ID)
	other := apitest.OwnedWorkspace(t, db, f, owner.ID)
	apitest.Must(factory.ProjectsIn(acme.ID).Sequence(func(i int, p *factory.Project) {
		p.Name, p.Description, p.Status = fmt.Sprintf("Launch %d", i), "50% done", []string{"Planned", "In progress", "Completed"}[i%3]
	}).Create(ctx, db, f, 5))(t)
	apitest.Must(factory.ProjectsIn(other.ID).Create(ctx, db, f, 2))(t)

	path := "/api/admin/workspaces/" + acme.PublicID + "/projects"
	page := decodeAdmin[store.Page[store.Project]](t, adminCall(t, h, "GET", path+"?sort=name&perPage=2&page=2", "", "", 200, cookie).Body.Bytes())
	if page.Pages.Items != 5 || len(page.Data) != 2 || page.Data[0].Name != "Launch 2" || page.Data[0].Description != "50% done" {
		t.Fatalf("projects, page 2 by name: %+v", page)
	}
	if found := decodeAdmin[store.Page[store.Project]](t, adminCall(t, h, "GET", path+"?status=Planned&q=launch", "", "", 200, cookie).Body.Bytes()); found.Pages.Items != 2 {
		t.Fatalf("Planned projects matching launch: %+v", found)
	}
	if none := decodeAdmin[store.Page[store.Project]](t, adminCall(t, h, "GET", path+"?q=5%25", "", "", 200, cookie).Body.Bytes()); none.Pages.Items != 0 {
		t.Fatalf("%% matched as a wildcard: %+v", none)
	}
	for _, query := range []string{"?sort=password", "?status=Done", "?workspace=x"} {
		adminCall(t, h, "GET", path+query, "", "", 400, cookie)
	}
	for _, id := range []string{"wsp_2yEHYLwiwPVhLW", "wsp_x", owner.PublicID} {
		adminCall(t, h, "GET", "/api/admin/workspaces/"+id+"/projects", "", "", 404, cookie)
	}
	listed := apitest.EventsNamed(t, db, "admin.project.listed")
	if len(listed) != 3 || listed[0].Payload["workspace"] != acme.PublicID || listed[0].Payload["sort"] != "name" ||
		listed[1].Payload["status"] != "Planned" || listed[0].Payload["admin"] != admin.User.PublicID || listed[0].WorkspaceID.Valid {
		t.Fatalf("admin.project.listed: %+v", listed)
	}
}

// The new reads need an admin session, like every admin read.
func TestAdminEventsAndMailNeedASession(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	app := apitest.SignedIn(t, db, factory.TestFaker(t))
	for _, path := range []string{"/api/admin/events", "/api/admin/events/evt_2yEHYLwiwPVhLW", "/api/admin/catalogue", "/api/admin/mail", "/api/admin/deliveries", "/api/admin/workspaces/wsp_2yEHYLwiwPVhLW/projects"} {
		adminCall(t, h, "GET", path, "", "", 401)
		adminCall(t, h, "GET", path, "", "", 401, app.Cookie())
	}
}
