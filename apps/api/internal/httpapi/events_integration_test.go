package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"forma/api/internal/factory"
	"forma/api/internal/store"
	"github.com/uptrace/bun"
	"strings"
	"testing"
)

type storedEvent struct {
	Name                         string
	ActorID, UserID, WorkspaceID sql.NullInt64
	Payload                      map[string]any
	Raw                          string
	UserAgent                    string
}

// eventsNamed returns the events with name, oldest first.
func eventsNamed(t *testing.T, db bun.IDB, name string) []storedEvent {
	t.Helper()
	rows := must(db.QueryContext(context.Background(), "SELECT name, actor_id, user_id, workspace_id, payload::text, user_agent FROM events WHERE name = ? ORDER BY id", name))(t)
	defer rows.Close()
	var found []storedEvent
	for rows.Next() {
		var e storedEvent
		if err := rows.Scan(&e.Name, &e.ActorID, &e.UserID, &e.WorkspaceID, &e.Raw, &e.UserAgent); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(e.Raw), &e.Payload); err != nil {
			t.Fatal(err)
		}
		found = append(found, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// onlyEvent is the single event with name; it fails unless there is exactly one.
func onlyEvent(t *testing.T, db bun.IDB, name string) storedEvent {
	t.Helper()
	found := eventsNamed(t, db, name)
	if len(found) != 1 {
		t.Fatalf("want one %s event, got %d: %+v", name, len(found), found)
	}
	return found[0]
}

// is reports whether a nullable ID column holds id (0 meaning NULL).
func is(column sql.NullInt64, id int64) bool {
	if id == 0 {
		return !column.Valid
	}
	return column.Valid && column.Int64 == id
}

// Payloads identify records by public ID only and never hold numeric IDs.
func checkPayloadIDs(t *testing.T, db bun.IDB) {
	t.Helper()
	rows := must(db.QueryContext(context.Background(), "SELECT name, payload::text FROM events"))(t)
	defer rows.Close()
	prefixes := map[string]string{"user": "usr", "workspace": "wsp", "project": "prj"}
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(raw), &payload)
		for key, prefix := range prefixes {
			if value, ok := payload[key]; ok && !validPublicID(prefix, fmt.Sprint(value)) {
				t.Errorf("%s: %s is not a public ID: %s", name, key, raw)
			}
		}
		if numericID.MatchString(raw) || strings.Contains(raw, `"id"`) {
			t.Errorf("%s: internal ID in payload: %s", name, raw)
		}
	}
}

func TestAuthEvents(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	const password = "correct horse battery staple"
	registered := call(t, h, "POST", "/api/auth/register", `{"name":"Ada Lovelace","email":"ada@example.com","password":"`+password+`"}`, "", nil, 201)
	var signed response
	_ = json.Unmarshal(registered.Body.Bytes(), &signed)
	userID := idOf(t, db, "users", signed.User.PublicID)

	e := onlyEvent(t, db, "auth.registered")
	if !is(e.ActorID, userID) || !is(e.UserID, userID) || !is(e.WorkspaceID, 0) || e.UserAgent != testUserAgent ||
		e.Payload["user"] != signed.User.PublicID || e.Payload["name"] != "Ada Lovelace" || e.Payload["email"] != "ada@example.com" {
		t.Fatalf("registered: %+v", e)
	}
	if len(eventsNamed(t, db, "auth.logged_in")) != 0 {
		t.Fatal("registration recorded a login")
	}

	// Every login that reaches the password check is recorded, never with the password.
	call(t, h, "POST", "/api/auth/login", `{"email":"ada@example.com","password":"a wrong guess here"}`, "", nil, 401)
	call(t, h, "POST", "/api/auth/login", `{"email":"nobody@example.com","password":"a wrong guess here"}`, "", nil, 401)
	call(t, h, "POST", "/api/auth/login", `{"email":"not an email","password":"a wrong guess here"}`, "", nil, 401)
	login := call(t, h, "POST", "/api/auth/login", `{"email":" ADA@example.com ","password":"`+password+`"}`, "", nil, 200)
	attempts := eventsNamed(t, db, "auth.login_attempted")
	if len(attempts) != 3 {
		t.Fatalf("want 3 login attempts (the malformed email isn't one), got %+v", attempts)
	}
	for i, want := range []struct {
		email, result string
		userID        int64
	}{{"ada@example.com", "wrong_password", userID}, {"nobody@example.com", "unknown_account", 0}, {"ada@example.com", "succeeded", userID}} {
		a := attempts[i]
		if a.Payload["email"] != want.email || a.Payload["result"] != want.result || a.Payload["ip"] != "127.0.0.1" ||
			!is(a.UserID, want.userID) || !is(a.ActorID, 0) || strings.Contains(a.Raw, "wrong guess") || strings.Contains(a.Raw, password) {
			t.Errorf("attempt %d: %+v", i, a)
		}
		if _, hasUser := a.Payload["user"]; hasUser != (want.userID != 0) {
			t.Errorf("attempt %d: user in payload: %s", i, a.Raw)
		}
	}
	e = onlyEvent(t, db, "auth.logged_in")
	if !is(e.ActorID, userID) || !is(e.UserID, userID) || e.Payload["user"] != signed.User.PublicID {
		t.Fatalf("logged in: %+v", e)
	}

	_ = json.Unmarshal(login.Body.Bytes(), &signed)
	cookie := login.Result().Cookies()[0]
	call(t, h, "DELETE", "/api/auth/sessions", "", signed.CSRF, cookie, 204)
	e = onlyEvent(t, db, "auth.other_sessions_revoked")
	if !is(e.ActorID, userID) || !is(e.UserID, userID) || e.Payload["count"] != float64(1) {
		t.Fatalf("other sessions revoked (the registration's session): %+v", e)
	}
	call(t, h, "POST", "/api/auth/logout", "{}", signed.CSRF, cookie, 204)
	e = onlyEvent(t, db, "auth.logged_out")
	if !is(e.ActorID, userID) || !is(e.UserID, userID) || e.Payload["user"] != signed.User.PublicID {
		t.Fatalf("logged out: %+v", e)
	}
	checkPayloadIDs(t, db)
}

// A flood of refused requests records the limit being reached once.
func TestRateLimitRecordedOnce(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := must(factory.Users.CreateOne(ctx, db, f))(t)
	must(factory.AuthAttempts.State(factory.ForAccount(user.Email)).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	login := fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword)
	for range 3 {
		call(t, h, "POST", "/api/auth/login", login, "", nil, 429)
	}
	e := onlyEvent(t, db, "auth.rate_limited")
	if e.Payload["scope"] != "account" || !is(e.UserID, 0) {
		t.Fatalf("rate limited: %+v", e)
	}
	if len(eventsNamed(t, db, "auth.login_attempted")) != 0 {
		t.Fatal("refused logins recorded as attempts")
	}
}

func TestIPRateLimitRecorded(t *testing.T) {
	s, db := integrationServer(t)
	f := factory.TestFaker(t)
	must(factory.AuthAttempts.State(factory.ForIP("127.0.0.1")).State(factory.Exhausted(60)).CreateOne(context.Background(), db, f))(t)
	for range 2 {
		call(t, s.Routes(), "POST", "/api/auth/login", `{"email":"a@example.com","password":"anything at all"}`, "", nil, 429)
	}
	if e := onlyEvent(t, db, "auth.rate_limited"); e.Payload["scope"] != "ip" {
		t.Fatalf("rate limited: %+v", e)
	}
}

func TestPasswordEvents(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := signedIn(t, db, f)
	must(factory.SessionFor(ctx, db, f, session.UserID))(t)
	cookie := session.Cookie()
	verify := func(password string, want int) {
		call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, password), session.CSRFToken, cookie, want)
	}
	change := func(current, next string, want int) {
		call(t, h, "PUT", "/api/auth/password", changeBody(current, next), session.CSRFToken, cookie, want)
	}
	verify("not my password", 422)
	verify(factory.DefaultPassword, 204)
	change("not my password", newPassword, 422)
	change(factory.DefaultPassword, factory.DefaultPassword, 422) // unchanged: the check passed
	change(factory.DefaultPassword, "too short", 422)             // refused before any check
	change(factory.DefaultPassword, newPassword, 200)
	checks := eventsNamed(t, db, "auth.password_checked")
	want := []struct {
		ok   bool
		step string
	}{{false, "verify"}, {true, "verify"}, {false, "update"}, {true, "update"}, {true, "update"}}
	if len(checks) != len(want) {
		t.Fatalf("want %d checks, got %+v", len(want), checks)
	}
	for i, w := range want {
		c := checks[i]
		if c.Payload["ok"] != w.ok || c.Payload["step"] != w.step || !is(c.UserID, session.UserID) || !is(c.ActorID, session.UserID) || strings.Contains(c.Raw, "password\"") {
			t.Errorf("check %d: %+v", i, c)
		}
	}
	e := onlyEvent(t, db, "auth.password_changed")
	if !is(e.UserID, session.UserID) || !is(e.ActorID, session.UserID) || e.Payload["otherSessionsRevoked"] != float64(1) || strings.Contains(e.Raw, newPassword) {
		t.Fatalf("password changed: %+v", e)
	}
	checkPayloadIDs(t, db)
}

func TestWorkspaceAndProjectEvents(t *testing.T) {
	s, db := integrationServer(t)
	h := s.Routes()
	session := signedIn(t, db, factory.TestFaker(t))
	cookie := session.Cookie()
	var workspace store.Workspace
	_ = json.Unmarshal(call(t, h, "POST", "/api/workspaces", `{"name":"Acme"}`, session.CSRFToken, cookie, 201).Body.Bytes(), &workspace)
	workspaceID := idOf(t, db, "workspaces", workspace.PublicID)
	e := onlyEvent(t, db, "workspace.created")
	if !is(e.ActorID, session.UserID) || !is(e.WorkspaceID, workspaceID) || !is(e.UserID, 0) || e.Payload["workspace"] != workspace.PublicID || e.Payload["name"] != "Acme" {
		t.Fatalf("workspace created: %+v", e)
	}

	path := workspacePath(workspace.PublicID, "")
	call(t, h, "PATCH", path, `{"name":"Acme"}`, session.CSRFToken, cookie, 200)
	if len(eventsNamed(t, db, "workspace.renamed")) != 0 {
		t.Fatal("a rename to the same name was recorded")
	}
	call(t, h, "PATCH", path, `{"name":"Acme Inc"}`, session.CSRFToken, cookie, 200)
	e = onlyEvent(t, db, "workspace.renamed")
	if !is(e.WorkspaceID, workspaceID) || !is(e.ActorID, session.UserID) || !strings.Contains(e.Raw, `"changes": {"name": {"to": "Acme Inc", "from": "Acme"}}`) {
		t.Fatalf("workspace renamed: %s", e.Raw)
	}

	body := `{"name":"Launch","description":"First steps","status":"Planned","color":"violet"}`
	var project store.Project
	_ = json.Unmarshal(call(t, h, "POST", workspacePath(workspace.PublicID, "/projects"), body, session.CSRFToken, cookie, 201).Body.Bytes(), &project)
	e = onlyEvent(t, db, "project.created")
	if !is(e.WorkspaceID, workspaceID) || !is(e.ActorID, session.UserID) || e.Payload["project"] != project.PublicID || e.Payload["workspace"] != workspace.PublicID ||
		e.Payload["name"] != "Launch" || e.Payload["description"] != "First steps" || e.Payload["status"] != "Planned" || e.Payload["color"] != "violet" {
		t.Fatalf("project created: %+v", e)
	}
	projectPath := workspacePath(workspace.PublicID, "/projects/"+project.PublicID)
	call(t, h, "PATCH", projectPath, body, session.CSRFToken, cookie, 200)
	if len(eventsNamed(t, db, "project.updated")) != 0 {
		t.Fatal("an update that changed nothing was recorded")
	}
	call(t, h, "PATCH", projectPath, strings.Replace(body, "Planned", "Completed", 1), session.CSRFToken, cookie, 200)
	e = onlyEvent(t, db, "project.updated")
	changes, _ := e.Payload["changes"].(map[string]any)
	if len(changes) != 1 || !strings.Contains(e.Raw, `"status": {"to": "Completed", "from": "Planned"}`) || e.Payload["project"] != project.PublicID {
		t.Fatalf("project updated: %s", e.Raw)
	}
	// A project in someone else's workspace is a 404 and records nothing.
	other := signedIn(t, db, factory.TestFaker(t))
	call(t, h, "PATCH", projectPath, body, other.CSRFToken, other.Cookie(), 404)
	if len(eventsNamed(t, db, "project.updated")) != 1 {
		t.Fatal("refused update recorded")
	}
	checkPayloadIDs(t, db)
}

// A change and its event are saved together or not at all.
func TestChangeAndEventSavedTogether(t *testing.T) {
	_, db := integrationServer(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	repository := &store.Store{DB: db}
	user := must(factory.Users.CreateOne(ctx, db, f))(t)
	owned := must(factory.WorkspaceFor(ctx, db, f, user.ID))(t)
	workspace := store.Workspace{ID: owned.ID, PublicID: owned.PublicID}
	project := must(factory.ProjectsIn(owned.ID).CreateOne(ctx, db, f))(t)
	// The status CHECK constraint fails after the row is locked and compared.
	invalid := store.ProjectInput{Name: "Renamed", Status: "Nope", Color: "violet"}
	if _, err := repository.UpdateProject(ctx, workspace, project.PublicID, invalid); err == nil {
		t.Fatal("invalid update saved")
	}
	if _, err := repository.CreateProject(ctx, workspace, invalid); err == nil {
		t.Fatal("invalid project saved")
	}
	for _, name := range []string{"project.updated", "project.created"} {
		if n := len(eventsNamed(t, db, name)); n != 0 {
			t.Errorf("%d %s events after a failed change", n, name)
		}
	}
	var unchanged string
	if err := db.QueryRowContext(ctx, "SELECT name FROM projects WHERE id = ?", project.ID).Scan(&unchanged); err != nil || unchanged != project.Name {
		t.Fatalf("project changed: %q (%v)", unchanged, err)
	}

	// And an event that can't be written undoes its change. The table is
	// renamed only in this test's private schema.
	must(db.ExecContext(ctx, "ALTER TABLE events RENAME TO events_unavailable"))(t)
	valid := store.ProjectInput{Name: "Never saved", Status: "Planned", Color: "blue"}
	if _, err := repository.CreateProject(ctx, workspace, valid); err == nil {
		t.Fatal("project saved without its event")
	}
	if _, err := repository.UpdateProject(ctx, workspace, project.PublicID, valid); err == nil {
		t.Fatal("project updated without its event")
	}
	if _, err := repository.RenameWorkspace(ctx, owned.ID, store.WorkspaceInput{Name: "Never renamed"}); err == nil {
		t.Fatal("workspace renamed without its event")
	}
	var projects int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM projects WHERE name IN (?, ?)", valid.Name, project.Name).Scan(&projects); err != nil || projects != 1 {
		t.Fatalf("want only the untouched project, got %d (%v)", projects, err)
	}
	var name string
	if err := db.QueryRowContext(ctx, "SELECT name FROM workspaces WHERE id = ?", owned.ID).Scan(&name); err != nil || name != owned.Name {
		t.Fatalf("workspace renamed to %q (%v)", name, err)
	}
}
