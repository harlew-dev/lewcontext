package workspace_test

import (
	"context"
	"encoding/json"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/store"
	"slices"
	"strings"
	"testing"
	"time"
)

// logFixture is a workspace with its roles and a log that holds the
// events owners may see, and the ones they may not.
type logFixture struct {
	memberFixture
	admin, member, viewer factory.Session
	ada                   factory.User // a member who did the changes
	project               string
	otherProject          string
}

const (
	logIP        = "203.0.113.77"
	logAgent     = "Mozilla/5.0 LogTest"
	logRequestID = "req_abcdefghijklmn"
)

func newLogFixture(t *testing.T) logFixture {
	t.Helper()
	m := newMemberFixture(t)
	ctx := context.Background()
	l := logFixture{memberFixture: m, admin: m.join(t, auth.RoleAdmin), member: m.join(t, auth.RoleMember), viewer: m.join(t, auth.RoleViewer)}
	l.ada = apitest.Must(factory.Users.State(func(u *factory.User) { u.Name = "Ada Lovelace" }).CreateOne(ctx, m.db, m.f))(t)
	apitest.Must(factory.MemberOf(ctx, m.db, m.f, l.ada.ID, m.workspace.ID, auth.RoleMember))(t)
	l.project, l.otherProject = "prj_aaaaaaaaaaaaaa", "prj_bbbbbbbbbbbbbb"
	return l
}

// add records an event in the workspace (workspace -1: none) with the IP,
// browser and request ID real ones carry.
func (l logFixture) add(t *testing.T, name string, ago time.Duration, scope func(*factory.Event), payload map[string]any) factory.Event {
	t.Helper()
	return apitest.Must(factory.Events.State(factory.EventOf(name, payload)).State(func(e *factory.Event) {
		e.WorkspaceID, e.IP, e.UserAgent, e.RequestID = l.workspace.ID, logIP, logAgent, logRequestID
		e.CreatedAt = time.Now().Add(-ago)
		if scope != nil {
			scope(e)
		}
	}).CreateOne(context.Background(), l.db, l.f))(t)
}

type logPage struct {
	Data []struct {
		ID      string                 `json:"id"`
		Name    string                 `json:"name"`
		Actor   *store.WorkspaceLogRef `json:"actor"`
		Subject *store.WorkspaceLogRef `json:"subject"`
		Details map[string]any         `json:"details"`
	} `json:"data"`
	Next *string                  `json:"next"`
	Refs store.WorkspaceEventRefs `json:"refs"`
}

func (l logFixture) get(t *testing.T, session factory.Session, query string, want int) (logPage, string) {
	t.Helper()
	w := apitest.Call(t, l.h, "GET", l.path("/events?"+query), "", "", session.Cookie(), want)
	var page logPage
	if want == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
	}
	return page, w.Body.String()
}

func logNamesOf(page logPage) []string {
	var names []string
	for _, e := range page.Data {
		names = append(names, e.Name)
	}
	return names
}

func TestWorkspaceLogIsForOwnersAndAdmins(t *testing.T) {
	l := newLogFixture(t)
	l.get(t, l.owner, "", 200)
	l.get(t, l.admin, "", 200)
	l.get(t, l.member, "", 403)
	l.get(t, l.viewer, "", 403)
	l.get(t, apitest.SignedIn(t, l.db, l.f), "", 404)
	apitest.Call(t, l.h, "GET", l.path("/events"), "", "", nil, 401)
}

func TestWorkspaceLogShowsOnlyWhatItAllows(t *testing.T) {
	l := newLogFixture(t)
	other := apitest.OwnedWorkspace(t, l.db, l.f, apitest.SignedIn(t, l.db, l.f).UserID)
	actor := func(e *factory.Event) { e.ActorID = l.ada.ID }
	l.add(t, "project.updated", time.Hour, actor, map[string]any{
		"workspace": l.workspace.PublicID, "project": l.project, "request": "evt_zzzzzzzzzzzzzz",
		"changes": map[string]any{"name": map[string]any{"from": "Website", "to": "Marketing site"}},
	})
	// Shown events, and ones that carry the workspace but are not for owners.
	for _, name := range []string{"auth.logged_in", "auth.password_changed", "rate_limit.reached", "admin.workspace.viewed", "workspace.invitation.sent", "alert.opened"} {
		l.add(t, name, time.Hour, actor, map[string]any{"workspace": l.workspace.PublicID, "admin": "usr_cccccccccccccc"})
	}
	// Another workspace's change, even naming this workspace in its payload.
	apitest.Must(factory.Events.State(factory.EventOf("project.updated", map[string]any{"workspace": l.workspace.PublicID, "project": "prj_dddddddddddddd"})).State(func(e *factory.Event) {
		e.WorkspaceID = other.ID
	}).CreateOne(context.Background(), l.db, l.f))(t)

	page, body := l.get(t, l.owner, "window=all", 200)
	if got := logNamesOf(page); !slices.Equal(got, []string{"project.updated"}) {
		t.Fatalf("log: %v", got)
	}
	e := page.Data[0]
	if e.Actor == nil || e.Actor.Name != "Ada Lovelace" || !strings.HasPrefix(e.Actor.PublicID, "usr_") {
		t.Fatalf("actor: %+v", e.Actor)
	}
	// A projection: the project and what changed, not the payload.
	if len(e.Details) != 2 || e.Details["project"] != l.project || e.Details["changes"] == nil {
		t.Fatalf("details: %v", e.Details)
	}
	// The request ID is the one reference an entry carries.
	if !strings.Contains(body, `"requestId":"`+logRequestID+`"`) {
		t.Errorf("no request ID in %s", body)
	}
	for _, secret := range []string{logIP, logAgent, "evt_zzzzzzzzzzzzzz", l.workspace.PublicID, `"ip"`, "userAgent", "prj_dddddddddddddd"} {
		if strings.Contains(body, secret) {
			t.Errorf("response holds %q: %s", secret, body)
		}
	}
	if apitest.NumericID.Match([]byte(body)) {
		t.Errorf("internal ID in %s", body)
	}
}

func TestWorkspaceLogFilters(t *testing.T) {
	l := newLogFixture(t)
	bob := apitest.Must(factory.Users.State(func(u *factory.User) { u.Name = "Bob Member" }).CreateOne(context.Background(), l.db, l.f))(t)
	by := func(u factory.User) func(*factory.Event) { return func(e *factory.Event) { e.ActorID = u.ID } }
	l.add(t, "project.created", 5*time.Hour, by(l.ada), map[string]any{"project": l.project, "name": "Rate 50% off", "description": "", "status": "Planned", "color": "blue"})
	l.add(t, "project.updated", 4*time.Hour, by(bob), map[string]any{"project": l.otherProject, "changes": map[string]any{"status": map[string]any{"from": "Planned", "to": "Completed"}}})
	l.add(t, "workspace.member.updated", 3*time.Hour, func(e *factory.Event) { e.ActorID, e.UserID = l.ada.ID, bob.ID },
		map[string]any{"user": "usr_eeeeeeeeeeeeee", "changes": map[string]any{"role": map[string]any{"from": "viewer", "to": "member"}}})
	l.add(t, "workspace.invitation.created", 2*time.Hour, by(l.ada), map[string]any{"invitation": "inv_aaaaaaaaaaaaaa", "email": "new@example.test", "role": "viewer"})
	l.add(t, "workspace.invitation.created", 100*time.Hour, by(l.ada), map[string]any{"invitation": "inv_bbbbbbbbbbbbbb", "email": "hmac:00ff00ff", "role": "viewer"})
	l.add(t, "workspace.api_key.created", time.Hour, nil, map[string]any{"key": "apk_aaaaaaaaaaaaaa", "scopes": []string{"project.index"}})
	l.add(t, "auth.api_key.created", 30*time.Minute, func(e *factory.Event) { e.UserID = l.ada.ID }, map[string]any{"key": "apk_bbbbbbbbbbbbbb", "scopes": []string{"project.show"}})

	cases := map[string][]string{
		"window=all":                          {"auth.api_key.created", "workspace.api_key.created", "workspace.invitation.created", "workspace.member.updated", "project.updated", "project.created", "workspace.invitation.created"},
		"":                                    {"auth.api_key.created", "workspace.api_key.created", "workspace.invitation.created", "workspace.member.updated", "project.updated", "project.created", "workspace.invitation.created"}, // the default: the last 7 days
		"window=24h":                          {"auth.api_key.created", "workspace.api_key.created", "workspace.invitation.created", "workspace.member.updated", "project.updated", "project.created"},
		"window=all&name=projects":            {"project.updated", "project.created"},
		"window=all&name=members,invitations": {"workspace.invitation.created", "workspace.member.updated", "workspace.invitation.created"},
		"window=all&name=api-keys":            {"auth.api_key.created", "workspace.api_key.created"},
		"window=all&name=project.created,workspace":  {"project.created"},
		"actor=" + l.adaPublic(t) + "&name=projects": {"project.created"},
		"subject=" + publicOf(t, l, bob):             {"workspace.member.updated"},
		"project=" + l.otherProject:                  {"project.updated"},
		"q=marketing":                                nil,
		"q=50%25":                                    {"project.created"},
		"q=%25":                                      {"project.created"},
		"q=completed":                                {"project.updated"},
		"q=NEW@EXAMPLE":                              {"workspace.invitation.created"},
		"window=all&q=00ff":                          nil,
		"from=" + time.Now().Add(-3*time.Hour-30*time.Minute).UTC().Format(time.RFC3339) + "&to=" + time.Now().Add(-90*time.Minute).UTC().Format(time.RFC3339): {"workspace.invitation.created", "workspace.member.updated"},
	}
	for query, want := range cases {
		page, _ := l.get(t, l.owner, query, 200)
		if got := logNamesOf(page); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}

	// An erased or pseudonymised address is not shown.
	page, _ := l.get(t, l.owner, "window=all&name=invitations", 200)
	last := page.Data[len(page.Data)-1]
	if _, has := last.Details["email"]; has || last.Details["emailRemoved"] != true {
		t.Errorf("minimised invitation: %v", last.Details)
	}
	// A personal key's event is about its owner.
	page, _ = l.get(t, l.owner, "name=auth.api_key.created", 200)
	if s := page.Data[0].Subject; s == nil || s.Name != "Ada Lovelace" || page.Data[0].Details["key"] != "apk_bbbbbbbbbbbbbb" {
		t.Errorf("personal key: %+v", page.Data[0])
	}
}

func (l logFixture) adaPublic(t *testing.T) string { return publicOf(t, l, l.ada) }

func publicOf(t *testing.T, l logFixture, u factory.User) string {
	t.Helper()
	return apitest.PublicIDOf(t, l.db, u.ID)
}

func TestWorkspaceLogPagesByCursor(t *testing.T) {
	l := newLogFixture(t)
	same := time.Now().Add(-time.Hour)
	var ids []string
	for i := range 5 {
		e := l.add(t, "project.deleted", time.Since(same), nil, map[string]any{"project": l.project, "name": string(rune('A' + i))})
		ids = append(ids, e.PublicID)
	}
	var seen []string
	query := "perPage=2"
	for range 4 {
		page, _ := l.get(t, l.owner, query, 200)
		for _, e := range page.Data {
			seen = append(seen, e.ID)
		}
		if page.Next == nil {
			break
		}
		query = "perPage=2&before=" + *page.Next
	}
	slices.Sort(seen)
	slices.Sort(ids)
	if !slices.Equal(seen, ids) {
		t.Fatalf("paged %v, want each of %v once", seen, ids)
	}

	// A cursor naming another workspace's event, or a hidden one, is as
	// good as gone, so it can't be used to find events.
	other := apitest.OwnedWorkspace(t, l.db, l.f, apitest.SignedIn(t, l.db, l.f).UserID)
	foreign := apitest.Must(factory.Events.State(factory.EventOf("project.deleted", map[string]any{"project": l.project, "name": "x"})).State(func(e *factory.Event) { e.WorkspaceID = other.ID }).CreateOne(context.Background(), l.db, l.f))(t)
	hidden := l.add(t, "rate_limit.reached", time.Minute, nil, map[string]any{"limit": "x"})
	for _, cursor := range []string{foreign.PublicID, hidden.PublicID, "evt_zzzzzzzzzzzzzz"} {
		l.get(t, l.owner, "before="+cursor, 400)
	}
}

func TestWorkspaceLogRefusesBadParameters(t *testing.T) {
	l := newLogFixture(t)
	for _, query := range []string{
		"name=nope", "name=projects,projects", "name=,", "name=auth.logged_in", "name=workspace.invitation.sent",
		"actor=ada", "subject=prj_aaaaaaaaaaaaaa", "project=usr_aaaaaaaaaaaaaa", "before=x",
		"window=yesterday", "window=24h&from=2026-01-01T00:00:00Z", "from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z",
		"from=1960-01-01T00:00:00Z", "perPage=0", "perPage=101", "q=" + strings.Repeat("a", 101), "q=a%00b",
		"unknown=1", "q=a&q=b", "ip=203.0.113.1",
	} {
		if _, body := l.get(t, l.owner, query, 400); strings.Contains(body, "203.0.113.1") && query != "ip=203.0.113.1" {
			t.Errorf("%q: %s", query, body)
		}
	}
}

// A filter can't be used to look up the name of an account that has nothing
// to do with the workspace, nor to see that it exists.
func TestWorkspaceLogRefsNameOnlyPeopleOfTheWorkspace(t *testing.T) {
	l := newLogFixture(t)
	stranger := apitest.Must(factory.Users.State(func(u *factory.User) { u.Name = "Sam Stranger" }).CreateOne(context.Background(), l.db, l.f))(t)
	page, body := l.get(t, l.owner, "actor="+publicOf(t, l, stranger), 200)
	if page.Refs.Actor == nil || page.Refs.Actor.Name != "" || strings.Contains(body, "Sam Stranger") {
		t.Fatalf("stranger named: %s", body)
	}
	page, _ = l.get(t, l.owner, "actor="+l.adaPublic(t), 200)
	if page.Refs.Actor == nil || page.Refs.Actor.Name != "Ada Lovelace" {
		t.Fatalf("member not named: %+v", page.Refs.Actor)
	}
}

func TestWorkspaceLogIsNotRecorded(t *testing.T) {
	l := newLogFixture(t)
	l.get(t, l.owner, "", 200)
	if got := apitest.EventsNamed(t, l.db, "workspace.event.listed"); len(got) != 0 {
		t.Fatalf("%d events recorded", len(got))
	}
	var count int
	if err := l.db.NewSelect().Model((*factory.Event)(nil)).ColumnExpr("count(*)").Scan(context.Background(), &count); err != nil || count != 0 {
		t.Fatalf("a read recorded %d events (err %v)", count, err)
	}
}

// A search finds an event by what it is ("api key"), by who did it, and by
// its text, with every word required.
func TestWorkspaceLogSearchesByWhatHappened(t *testing.T) {
	l := newLogFixture(t)
	by := func(u factory.User) func(*factory.Event) { return func(e *factory.Event) { e.ActorID = u.ID } }
	l.add(t, "workspace.api_key.created", 4*time.Hour, by(l.ada), map[string]any{"key": "apk_aaaaaaaaaaaaaa", "scopes": []string{"project.index"}})
	l.add(t, "auth.api_key.revoked", 3*time.Hour, by(l.ada), map[string]any{"key": "apk_bbbbbbbbbbbbbb", "reason": "member_left"})
	l.add(t, "project.updated", 2*time.Hour, by(l.ada), map[string]any{"project": l.project, "changes": map[string]any{"name": map[string]any{"from": "A", "to": "Keyboard"}}})
	l.add(t, "workspace.member.updated", time.Hour, by(l.ada), map[string]any{"user": "usr_eeeeeeeeeeeeee", "changes": map[string]any{"role": map[string]any{"from": "viewer", "to": "member"}}})
	cases := map[string][]string{
		"api key":          {"auth.api_key.revoked", "workspace.api_key.created"},
		"API KEY created":  {"workspace.api_key.created"},
		"api_key":          {"auth.api_key.revoked", "workspace.api_key.created"},
		"personal key":     {"auth.api_key.revoked"},
		"revoked":          {"auth.api_key.revoked"},
		"keyboard":         {"project.updated"},
		"role":             {"workspace.member.updated"},
		"lovelace":         {"workspace.member.updated", "project.updated", "auth.api_key.revoked", "workspace.api_key.created"},
		"lovelace project": {"project.updated"},
		"nothing like it":  nil,
	}
	for query, want := range cases {
		page, _ := l.get(t, l.owner, "q="+strings.ReplaceAll(query, " ", "%20"), 200)
		if got := logNamesOf(page); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
}

func TestWorkspaceLogShowsOneChangeWithItsRequest(t *testing.T) {
	l := newLogFixture(t)
	other := apitest.OwnedWorkspace(t, l.db, l.f, apitest.SignedIn(t, l.db, l.f).UserID)
	actor := func(e *factory.Event) { e.ActorID = l.ada.ID }
	removed := l.add(t, "workspace.member.removed", time.Hour, actor, map[string]any{"user": "usr_eeeeeeeeeeeeee", "role": "member"})
	revoked := l.add(t, "auth.api_key.revoked", time.Hour, actor, map[string]any{"key": "apk_bbbbbbbbbbbbbb", "reason": "member_removed"})
	l.add(t, "rate_limit.reached", time.Hour, actor, map[string]any{"limit": "x"}) // same request, hidden
	alone := l.add(t, "project.deleted", time.Hour, func(e *factory.Event) { e.ActorID, e.RequestID = l.ada.ID, "req_zzzzzzzzzzzzzz" }, map[string]any{"project": l.project, "name": "P"})
	foreign := apitest.Must(factory.Events.State(factory.EventOf("project.deleted", map[string]any{"project": l.project, "name": "x"})).State(func(e *factory.Event) {
		e.WorkspaceID, e.RequestID = other.ID, logRequestID
	}).CreateOne(context.Background(), l.db, l.f))(t)
	hidden := l.add(t, "rate_limit.reached", time.Minute, nil, map[string]any{"limit": "x"})

	var got struct {
		ID        string `json:"id"`
		RequestID string `json:"requestId"`
		Related   []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"related"`
	}
	w := apitest.Call(t, l.h, "GET", l.path("/events/"+removed.PublicID), "", "", l.admin.Cookie(), 200)
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != removed.PublicID || got.RequestID != logRequestID {
		t.Fatalf("event: %+v", got)
	}
	// Only this workspace's allowed events of the same request: not itself,
	// not the hidden one, not the other workspace's.
	if len(got.Related) != 1 || got.Related[0].ID != revoked.PublicID {
		t.Fatalf("related: %+v", got.Related)
	}
	for _, secret := range []string{logIP, logAgent, `"ip"`, "userAgent"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Errorf("response holds %q", secret)
		}
	}
	w = apitest.Call(t, l.h, "GET", l.path("/events/"+alone.PublicID), "", "", l.owner.Cookie(), 200)
	if !strings.Contains(w.Body.String(), `"related":[]`) {
		t.Fatalf("related: %s", w.Body.String())
	}
	for _, id := range []string{foreign.PublicID, hidden.PublicID, "evt_zzzzzzzzzzzzzz", "nope"} {
		apitest.Call(t, l.h, "GET", l.path("/events/"+id), "", "", l.owner.Cookie(), 404)
	}
	apitest.Call(t, l.h, "GET", l.path("/events/"+removed.PublicID), "", "", l.member.Cookie(), 403)
	if len(apitest.EventsNamed(t, l.db, "workspace.event.viewed")) != 0 {
		t.Fatal("reading an event recorded one")
	}
}

func TestWorkspaceLogFiltersByAccountAndKey(t *testing.T) {
	l := newLogFixture(t)
	bob := apitest.Must(factory.Users.CreateOne(context.Background(), l.db, l.f))(t)
	l.add(t, "project.created", 3*time.Hour, func(e *factory.Event) { e.ActorID = l.ada.ID }, map[string]any{"project": l.project, "name": "P", "description": "", "status": "Planned", "color": "blue"})
	l.add(t, "workspace.member.updated", 2*time.Hour, func(e *factory.Event) { e.ActorID, e.UserID = bob.ID, l.ada.ID }, map[string]any{"user": "usr_x"})
	l.add(t, "project.updated", time.Hour, func(e *factory.Event) { e.ApiKeyPublicID = "apk_aaaaaaaaaaaaaa" }, map[string]any{"project": l.project, "changes": map[string]any{"name": map[string]any{"from": "A", "to": "B"}}})
	l.add(t, "workspace.api_key.created", 4*time.Hour, nil, map[string]any{"key": "apk_aaaaaaaaaaaaaa", "scopes": []string{"project.index"}})
	l.add(t, "workspace.api_key.created", 5*time.Hour, nil, map[string]any{"key": "apk_bbbbbbbbbbbbbb", "scopes": []string{"project.index"}})
	cases := map[string][]string{
		"account=" + l.adaPublic(t):      {"workspace.member.updated", "project.created"},
		"account=" + publicOf(t, l, bob): {"workspace.member.updated"},
		"key=apk_aaaaaaaaaaaaaa":         {"project.updated", "workspace.api_key.created"},
		"key=apk_cccccccccccccc":         nil,
		"account=usr_zzzzzzzzzzzzzz":     nil,
	}
	for query, want := range cases {
		page, _ := l.get(t, l.owner, query, 200)
		if got := logNamesOf(page); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", query, got, want)
		}
	}
	l.get(t, l.owner, "key=prj_aaaaaaaaaaaaaa", 400)
	l.get(t, l.owner, "account=ada", 400)
}

func TestMemberPage(t *testing.T) {
	l := newLogFixture(t)
	ctx := context.Background()
	apitest.Must(factory.ApiKeys.State(factory.ApiKeyNever).State(func(k *factory.ApiKey) {
		k.WorkspaceID, k.Kind, k.OwnerUserID, k.CreatedBy, k.TokenHash, k.RevokedAt = l.workspace.ID, "personal", l.ada.ID, l.ada.ID, auth.Digest(l.f.UUID()), nil
	}).CreateOne(ctx, l.db, l.f))(t)
	l.add(t, "project.created", time.Hour, func(e *factory.Event) { e.ActorID = l.ada.ID }, map[string]any{"project": l.project, "name": "P", "description": "", "status": "Planned", "color": "blue"})
	former := apitest.Must(factory.Users.State(func(u *factory.User) { u.Name = "Fran Former" }).CreateOne(ctx, l.db, l.f))(t)
	l.add(t, "workspace.member.left", time.Hour, func(e *factory.Event) { e.ActorID, e.UserID = former.ID, former.ID }, map[string]any{"role": "member"})
	stranger := apitest.Must(factory.Users.CreateOne(ctx, l.db, l.f))(t)

	var d store.MemberDetail
	get := func(id string, session factory.Session, want int) string {
		w := apitest.Call(t, l.h, "GET", l.path("/members/"+id), "", "", session.Cookie(), want)
		d = store.MemberDetail{}
		if want == 200 {
			if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
				t.Fatal(err)
			}
		}
		return w.Body.String()
	}
	get(l.adaPublic(t), l.admin, 200)
	if !d.Current || d.Name != "Ada Lovelace" || d.Role != auth.RoleMember || d.Email == "" || d.AddedAt == nil || d.Keys != 1 || d.Changes != 1 {
		t.Fatalf("member: %+v", d)
	}
	// Someone who left is known by name only, so their history stays readable.
	body := get(publicOf(t, l, former), l.owner, 200)
	if d.Current || d.Name != "Fran Former" || d.Email != "" || d.Role != "" || d.AddedAt != nil || strings.Contains(body, former.Email) {
		t.Fatalf("former: %+v %s", d, body)
	}
	get(publicOf(t, l, stranger), l.owner, 404)
	get("usr_zzzzzzzzzzzzzz", l.owner, 404)
	get("nope", l.owner, 404)
	get(l.adaPublic(t), l.member, 403)
	get(l.adaPublic(t), l.viewer, 403)
}

func TestApiKeyPagesAndLogs(t *testing.T) {
	l := newLogFixture(t)
	ctx := context.Background()
	personal := apitest.Must(factory.ApiKeys.State(factory.ApiKeyNever).State(func(k *factory.ApiKey) {
		k.WorkspaceID, k.Kind, k.OwnerUserID, k.CreatedBy, k.Name, k.TokenHash, k.RevokedAt = l.workspace.ID, "personal", l.ada.ID, l.ada.ID, "Ada script", auth.Digest(l.f.UUID()), nil
	}).CreateOne(ctx, l.db, l.f))(t)
	shared := apitest.Must(factory.ApiKeys.State(factory.ApiKeyNever).State(func(k *factory.ApiKey) {
		k.WorkspaceID, k.Name, k.TokenHash, k.RevokedAt = l.workspace.ID, "Shared", auth.Digest(l.f.UUID()), nil
	}).CreateOne(ctx, l.db, l.f))(t)
	l.add(t, "auth.api_key.created", 3*time.Hour, func(e *factory.Event) { e.ActorID, e.UserID = l.ada.ID, l.ada.ID }, map[string]any{"key": personal.PublicID, "scopes": []string{"project.index"}})
	l.add(t, "project.updated", time.Hour, func(e *factory.Event) { e.ApiKeyPublicID, e.ActorID, e.IP = personal.PublicID, l.ada.ID, logIP }, map[string]any{"project": l.project, "changes": map[string]any{"name": map[string]any{"from": "A", "to": "B"}}})
	l.add(t, "project.updated", time.Hour, nil, map[string]any{"project": l.project, "changes": map[string]any{"name": map[string]any{"from": "A", "to": "C"}}})
	l.add(t, "workspace.api_key.created", 2*time.Hour, nil, map[string]any{"key": shared.PublicID, "scopes": []string{"project.index"}})

	// Workspace owners and admins read either kind, and filter the list by owner.
	for _, id := range []string{personal.PublicID, shared.PublicID} {
		w := apitest.Call(t, l.h, "GET", l.path("/api-keys/"+id), "", "", l.admin.Cookie(), 200)
		if !strings.Contains(w.Body.String(), `"id":"`+id+`"`) || strings.Contains(w.Body.String(), "token") {
			t.Fatalf("key: %s", w.Body.String())
		}
	}
	apitest.Call(t, l.h, "GET", l.path("/api-keys/"+personal.PublicID), "", "", l.member.Cookie(), 403)
	apitest.Call(t, l.h, "GET", l.path("/api-keys/apk_zzzzzzzzzzzzzz"), "", "", l.owner.Cookie(), 404)
	var keys struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	w := apitest.Call(t, l.h, "GET", l.path("/api-keys?owner="+l.adaPublic(t)), "", "", l.owner.Cookie(), 200)
	if err := json.Unmarshal(w.Body.Bytes(), &keys); err != nil || len(keys.Data) != 1 || keys.Data[0].ID != personal.PublicID {
		t.Fatalf("owner filter: %s", w.Body.String())
	}
	apitest.Call(t, l.h, "GET", l.path("/api-keys?owner=ada"), "", "", l.owner.Cookie(), 400)

	// The owner reads their own key and its log, which holds only that key's events.
	ada := factory.Session{}
	ada = apitest.Must(factory.SessionFor(ctx, l.db, l.f, l.ada.ID))(t)
	apitest.Call(t, l.h, "GET", "/api/auth/api-keys/"+personal.PublicID, "", "", ada.Cookie(), 200)
	apitest.Call(t, l.h, "GET", "/api/auth/api-keys/"+shared.PublicID, "", "", ada.Cookie(), 404)
	page := logPage{}
	w = apitest.Call(t, l.h, "GET", "/api/auth/api-keys/"+personal.PublicID+"/events", "", "", ada.Cookie(), 200)
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if got := logNamesOf(page); !slices.Equal(got, []string{"project.updated", "auth.api_key.created"}) {
		t.Fatalf("key log: %v", got)
	}
	if strings.Contains(w.Body.String(), logIP) {
		t.Fatal("an address in the key's log")
	}
	// No other filters, no one else's key, nothing once they've left.
	apitest.Call(t, l.h, "GET", "/api/auth/api-keys/"+personal.PublicID+"/events?actor="+l.adaPublic(t), "", "", ada.Cookie(), 400)
	apitest.Call(t, l.h, "GET", "/api/auth/api-keys/"+shared.PublicID+"/events", "", "", ada.Cookie(), 404)
	apitest.Call(t, l.h, "GET", "/api/auth/api-keys/"+personal.PublicID+"/events", "", "", l.owner.Cookie(), 404)
	if _, err := l.db.NewDelete().Model((*factory.WorkspaceUser)(nil)).Where("user_id = ? AND workspace_id = ?", l.ada.ID, l.workspace.ID).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	apitest.Call(t, l.h, "GET", "/api/auth/api-keys/"+personal.PublicID+"/events", "", "", ada.Cookie(), 404)
	// The workspace's own filter finds the same events.
	page = logPage{}
	w = apitest.Call(t, l.h, "GET", l.path("/events?key="+personal.PublicID), "", "", l.owner.Cookie(), 200)
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if got := logNamesOf(page); !slices.Equal(got, []string{"project.updated", "auth.api_key.created"}) {
		t.Fatalf("workspace key filter: %v", got)
	}
}
