package httpapi_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
)

func TestAuthEvents(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	const password = "correct horse battery staple"
	registered := apitest.Call(t, h, "POST", "/api/auth/register", `{"name":"Ada Lovelace","email":"ada@example.com","password":"`+password+`"}`, "", nil, 201)
	var signed apitest.SessionResponse
	_ = json.Unmarshal(registered.Body.Bytes(), &signed)
	userID := apitest.IDOf(t, db, "users", signed.User.PublicID)

	e := apitest.OnlyEvent(t, db, "auth.registered")
	if !apitest.Is(e.ActorID, userID) || !apitest.Is(e.UserID, userID) || !apitest.Is(e.WorkspaceID, 0) || e.UserAgent != apitest.UserAgent ||
		e.Payload["user"] != signed.User.PublicID || e.Payload["name"] != "Ada Lovelace" || e.Payload["email"] != "ada@example.com" {
		t.Fatalf("registered: %+v", e)
	}
	if len(apitest.EventsNamed(t, db, "auth.logged_in")) != 0 {
		t.Fatal("registration recorded a login")
	}

	// Every login that reaches the password check is recorded, never with the password.
	apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"ada@example.com","password":"a wrong guess here"}`, "", nil, 401)
	apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"nobody@example.com","password":"a wrong guess here"}`, "", nil, 401)
	apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"not an email","password":"a wrong guess here"}`, "", nil, 401)
	login := apitest.Call(t, h, "POST", "/api/auth/login", `{"email":" ADA@example.com ","password":"`+password+`"}`, "", nil, 200)
	attempts := apitest.EventsNamed(t, db, "auth.login_attempted")
	if len(attempts) != 3 {
		t.Fatalf("want 3 login attempts (the malformed email isn't one), got %+v", attempts)
	}
	for i, want := range []struct {
		email, result string
		userID        int64
	}{{"ada@example.com", "wrong_password", userID}, {"nobody@example.com", "unknown_account", 0}, {"ada@example.com", "succeeded", userID}} {
		a := attempts[i]
		if a.Payload["email"] != want.email || a.Payload["result"] != want.result || a.IP != "127.0.0.1" ||
			!apitest.Is(a.UserID, want.userID) || !apitest.Is(a.ActorID, 0) || strings.Contains(a.Raw, "wrong guess") || strings.Contains(a.Raw, password) {
			t.Errorf("attempt %d: %+v", i, a)
		}
		if _, hasUser := a.Payload["user"]; hasUser != (want.userID != 0) {
			t.Errorf("attempt %d: user in payload: %s", i, a.Raw)
		}
	}
	e = apitest.OnlyEvent(t, db, "auth.logged_in")
	if !apitest.Is(e.ActorID, userID) || !apitest.Is(e.UserID, userID) || e.Payload["user"] != signed.User.PublicID {
		t.Fatalf("logged in: %+v", e)
	}

	_ = json.Unmarshal(login.Body.Bytes(), &signed)
	cookie := login.Result().Cookies()[0]
	apitest.Call(t, h, "DELETE", "/api/auth/sessions", "", signed.CSRF, cookie, 204)
	e = apitest.OnlyEvent(t, db, "auth.other_sessions_revoked")
	if !apitest.Is(e.ActorID, userID) || !apitest.Is(e.UserID, userID) || e.Payload["count"] != float64(1) {
		t.Fatalf("other sessions revoked (the registration's session): %+v", e)
	}
	// With no other session left, asking again changes nothing and records
	// nothing.
	apitest.Call(t, h, "DELETE", "/api/auth/sessions", "", signed.CSRF, cookie, 204)
	apitest.OnlyEvent(t, db, "auth.other_sessions_revoked")
	apitest.Call(t, h, "POST", "/api/auth/logout", "{}", signed.CSRF, cookie, 204)
	e = apitest.OnlyEvent(t, db, "auth.logged_out")
	if !apitest.Is(e.ActorID, userID) || !apitest.Is(e.UserID, userID) || e.Payload["user"] != signed.User.PublicID {
		t.Fatalf("logged out: %+v", e)
	}
	apitest.CheckPayloadIDs(t, db)
}

// A flood of refused requests records the limit being reached once.
func TestRateLimitRecordedOnce(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.RateLimits.State(factory.ForAccount(user.Email)).State(factory.Exhausted(10)).CreateOne(ctx, db, f))(t)
	login := fmt.Sprintf(`{"email":%q,"password":%q}`, user.Email, factory.DefaultPassword)
	for range 3 {
		apitest.Call(t, h, "POST", "/api/auth/login", login, "", nil, 429)
	}
	e := apitest.OnlyEvent(t, db, "rate_limit.reached")
	if e.Payload["limit"] != "auth:per-account:block" || e.IP != "127.0.0.1" || e.Payload["window"] != float64(900) || !apitest.Is(e.UserID, 0) {
		t.Fatalf("rate limited: %+v", e)
	}
	if len(apitest.EventsNamed(t, db, "auth.login_attempted")) != 0 {
		t.Fatal("refused logins recorded as attempts")
	}
}

func TestIPRateLimitRecorded(t *testing.T) {
	s, db := server(t)
	f := factory.TestFaker(t)
	apitest.Must(factory.RateLimits.State(factory.ForIP("127.0.0.1")).State(factory.Exhausted(60)).CreateOne(context.Background(), db, f))(t)
	for range 2 {
		apitest.Call(t, router.New(s), "POST", "/api/auth/login", `{"email":"a@example.com","password":"anything at all"}`, "", nil, 429)
	}
	if e := apitest.OnlyEvent(t, db, "rate_limit.reached"); e.Payload["limit"] != "auth:per-ip:block" {
		t.Fatalf("rate limited: %+v", e)
	}
}

func TestPasswordEvents(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	apitest.Must(factory.SessionFor(ctx, db, f, session.UserID))(t)
	cookie := session.Cookie()
	verify := func(password string, want int) {
		apitest.Call(t, h, "POST", "/api/auth/password/verify", fmt.Sprintf(`{"password":%q}`, password), session.CSRFToken, cookie, want)
	}
	change := func(current, next string, want int) {
		apitest.Call(t, h, "PATCH", "/api/auth/password", apitest.ChangeBody(current, next), session.CSRFToken, cookie, want)
	}
	verify("not my password", 422)
	verify(factory.DefaultPassword, 204)
	change("not my password", apitest.NewPassword, 422)
	change(factory.DefaultPassword, factory.DefaultPassword, 422) // unchanged: the check passed
	change(factory.DefaultPassword, "too short", 422)             // refused before any check
	change(factory.DefaultPassword, apitest.NewPassword, 200)
	checks := apitest.EventsNamed(t, db, "auth.password_checked")
	want := []struct {
		ok   bool
		step string
	}{{false, "verify"}, {true, "verify"}, {false, "update"}, {true, "update"}, {true, "update"}}
	if len(checks) != len(want) {
		t.Fatalf("want %d checks, got %+v", len(want), checks)
	}
	for i, w := range want {
		c := checks[i]
		if c.Payload["ok"] != w.ok || c.Payload["step"] != w.step || !apitest.Is(c.UserID, session.UserID) || !apitest.Is(c.ActorID, session.UserID) || strings.Contains(c.Raw, "password\"") {
			t.Errorf("check %d: %+v", i, c)
		}
	}
	e := apitest.OnlyEvent(t, db, "auth.password_changed")
	if !apitest.Is(e.UserID, session.UserID) || !apitest.Is(e.ActorID, session.UserID) || e.Payload["otherSessionsRevoked"] != float64(1) || strings.Contains(e.Raw, apitest.NewPassword) {
		t.Fatalf("password changed: %+v", e)
	}
	apitest.CheckPayloadIDs(t, db)
}

func TestWorkspaceAndProjectEvents(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	session := apitest.SignedIn(t, db, factory.TestFaker(t))
	cookie := session.Cookie()
	var workspace store.Workspace
	_ = json.Unmarshal(apitest.Call(t, h, "POST", "/api/workspaces", `{"name":"Acme"}`, session.CSRFToken, cookie, 201).Body.Bytes(), &workspace)
	workspaceID := apitest.IDOf(t, db, "workspaces", workspace.PublicID)
	e := apitest.OnlyEvent(t, db, "workspace.created")
	if !apitest.Is(e.ActorID, session.UserID) || !apitest.Is(e.WorkspaceID, workspaceID) || !apitest.Is(e.UserID, 0) || e.Payload["workspace"] != workspace.PublicID || e.Payload["name"] != "Acme" {
		t.Fatalf("workspace created: %+v", e)
	}

	path := apitest.WorkspacePath(workspace.PublicID, "")
	apitest.Call(t, h, "PATCH", path, `{"name":"Acme"}`, session.CSRFToken, cookie, 200)
	if len(apitest.EventsNamed(t, db, "workspace.renamed")) != 0 {
		t.Fatal("a rename to the same name was recorded")
	}
	apitest.Call(t, h, "PATCH", path, `{"name":"Acme Inc"}`, session.CSRFToken, cookie, 200)
	e = apitest.OnlyEvent(t, db, "workspace.renamed")
	if !apitest.Is(e.WorkspaceID, workspaceID) || !apitest.Is(e.ActorID, session.UserID) || !strings.Contains(e.Raw, `"changes": {"name": {"to": "Acme Inc", "from": "Acme"}}`) {
		t.Fatalf("workspace renamed: %s", e.Raw)
	}

	body := `{"name":"Launch","description":"First steps","status":"Planned","color":"violet"}`
	var project store.Project
	_ = json.Unmarshal(apitest.Call(t, h, "POST", apitest.WorkspacePath(workspace.PublicID, "/projects"), body, session.CSRFToken, cookie, 201).Body.Bytes(), &project)
	e = apitest.OnlyEvent(t, db, "project.created")
	if !apitest.Is(e.WorkspaceID, workspaceID) || !apitest.Is(e.ActorID, session.UserID) || e.Payload["project"] != project.PublicID || e.Payload["workspace"] != workspace.PublicID ||
		e.Payload["name"] != "Launch" || e.Payload["description"] != "First steps" || e.Payload["status"] != "Planned" || e.Payload["color"] != "violet" {
		t.Fatalf("project created: %+v", e)
	}
	projectPath := apitest.WorkspacePath(workspace.PublicID, "/projects/"+project.PublicID)
	apitest.Call(t, h, "PATCH", projectPath, body, session.CSRFToken, cookie, 200)
	if len(apitest.EventsNamed(t, db, "project.updated")) != 0 {
		t.Fatal("an update that changed nothing was recorded")
	}
	apitest.Call(t, h, "PATCH", projectPath, strings.Replace(body, "Planned", "Completed", 1), session.CSRFToken, cookie, 200)
	e = apitest.OnlyEvent(t, db, "project.updated")
	changes, _ := e.Payload["changes"].(map[string]any)
	if len(changes) != 1 || !strings.Contains(e.Raw, `"status": {"to": "Completed", "from": "Planned"}`) || e.Payload["project"] != project.PublicID {
		t.Fatalf("project updated: %s", e.Raw)
	}
	// A project in someone else's workspace is a 404 and records nothing.
	other := apitest.SignedIn(t, db, factory.TestFaker(t))
	apitest.Call(t, h, "PATCH", projectPath, body, other.CSRFToken, other.Cookie(), 404)
	if len(apitest.EventsNamed(t, db, "project.updated")) != 1 {
		t.Fatal("refused update recorded")
	}
	apitest.CheckPayloadIDs(t, db)
}

// A change and its event are saved together or not at all.
func TestChangeAndEventSavedTogether(t *testing.T) {
	_, db := server(t)
	ctx := context.Background()
	f := factory.TestFaker(t)
	repository := &store.Store{DB: db}
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	owned := apitest.Must(factory.WorkspaceFor(ctx, db, f, user.ID))(t)
	workspace := store.Workspace{ID: owned.ID, PublicID: owned.PublicID}
	project := apitest.Must(factory.ProjectsIn(owned.ID).CreateOne(ctx, db, f))(t)
	// The status CHECK constraint fails after the row is locked and compared.
	invalid := store.ProjectInput{Name: "Renamed", Status: "Nope", Color: "violet"}
	if _, err := repository.UpdateProject(ctx, workspace, project.PublicID, invalid); err == nil {
		t.Fatal("invalid update saved")
	}
	if _, err := repository.CreateProject(ctx, workspace, invalid); err == nil {
		t.Fatal("invalid project saved")
	}
	for _, name := range []string{"project.updated", "project.created"} {
		if n := len(apitest.EventsNamed(t, db, name)); n != 0 {
			t.Errorf("%d %s events after a failed change", n, name)
		}
	}
	var unchanged string
	if err := db.NewSelect().TableExpr("projects").Column("name").Where("id = ?", project.ID).Scan(ctx, &unchanged); err != nil || unchanged != project.Name {
		t.Fatalf("project changed: %q (%v)", unchanged, err)
	}

	// And an event that can't be written undoes its change. The table is
	// dropped only in this test's private schema.
	apitest.Must(db.NewDropTable().Table("events").Cascade().Exec(ctx))(t)
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
	projects, err := db.NewSelect().TableExpr("projects").Where("name IN (?)", bun.In([]string{valid.Name, project.Name})).Count(ctx)
	if err != nil || projects != 1 {
		t.Fatalf("want only the untouched project, got %d (%v)", projects, err)
	}
	var name string
	if err := db.NewSelect().TableExpr("workspaces").Column("name").Where("id = ?", owned.ID).Scan(ctx, &name); err != nil || name != owned.Name {
		t.Fatalf("workspace renamed to %q (%v)", name, err)
	}
}

// A change made through the API reaches a typed listener through the worker,
// exactly once.
func TestListenerReceivesEventsFromTheAPI(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	bus := events.NewBus()
	var received []events.ProjectCreated
	var meta events.Delivered
	events.Listen(bus, "test.projects", func(_ context.Context, e events.ProjectCreated, d events.Delivered) error {
		received, meta = append(received, e), d
		return nil
	})
	s.Repo.Listeners = bus
	session := apitest.SignedIn(t, db, factory.TestFaker(t))
	workspace := apitest.OwnedWorkspace(t, db, factory.TestFaker(t), session.UserID)
	var project store.Project
	body := `{"name":"Launch","description":"","status":"Planned","color":"violet"}`
	_ = json.Unmarshal(apitest.Call(t, h, "POST", apitest.WorkspacePath(workspace.PublicID, "/projects"), body, session.CSRFToken, session.Cookie(), 201).Body.Bytes(), &project)

	worker := &events.Worker{Bus: bus, Queue: s.Repo}
	if n, err := worker.RunOnce(context.Background()); n != 1 || err != nil {
		t.Fatalf("RunOnce: %d, %v", n, err)
	}
	if len(received) != 1 || received[0].Project != project.PublicID || received[0].Workspace != workspace.PublicID || received[0].Name != "Launch" ||
		!input.PublicID("evt", meta.EventID) || meta.Attempt != 1 {
		t.Fatalf("listener got %+v %+v", received, meta)
	}
	// Delivered once: nothing is left to claim.
	if n, err := worker.RunOnce(context.Background()); n != 0 || err != nil {
		t.Fatalf("second RunOnce: %d, %v", n, err)
	}
	var deliveredAt sql.NullTime
	if err := db.NewSelect().TableExpr("event_deliveries").Column("delivered_at").Where("listener = ?", "test.projects").Scan(context.Background(), &deliveredAt); err != nil || !deliveredAt.Valid {
		t.Fatalf("delivery not marked delivered: %v (%v)", deliveredAt, err)
	}
}

// Signing out the other sessions skips the write budget (a recovery route)
// but has a limit of its own, sessions-revoke:per-user, counted per
// session: a second session of the same user (the owner, when the first
// was stolen) isn't refused once the first has used its budget up.
func TestSessionsRevokeLimited(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	session := apitest.Must(factory.SessionFor(ctx, db, f, user.ID))(t)
	apitest.Call(t, h, "DELETE", "/api/auth/sessions", "", session.CSRFToken, session.Cookie(), 204)
	// The first request opened the session's window; use the rest of it up.
	key := httpapi.SessionsRevokePerUser.Key(auth.Digest(session.Token))
	apitest.Must(db.NewUpdate().Model((*factory.RateLimit)(nil)).Set("hits = ?", httpapi.SessionsRevokePerUser.Block.At()).Where("key = ?", key).Exec(ctx))(t)
	apitest.Call(t, h, "DELETE", "/api/auth/sessions", "", session.CSRFToken, session.Cookie(), 429)
	if e := apitest.OnlyEvent(t, db, "rate_limit.reached"); e.Payload["limit"] != "sessions-revoke:per-user:block" {
		t.Fatalf("rate limited: %+v", e)
	}
	if n := len(apitest.EventsNamed(t, db, "auth.other_sessions_revoked")); n != 0 {
		t.Fatalf("%d revokes recorded with nothing signed out", n)
	}
	// The same user's other session still signs the first out.
	owner := apitest.Must(factory.SessionFor(ctx, db, f, user.ID))(t)
	apitest.Call(t, h, "DELETE", "/api/auth/sessions", "", owner.CSRFToken, owner.Cookie(), 204)
	if e := apitest.EventsNamed(t, db, "auth.other_sessions_revoked"); len(e) != 1 || e[0].Payload["count"] != 1.0 {
		t.Fatalf("revoked: %+v", e)
	}
}
