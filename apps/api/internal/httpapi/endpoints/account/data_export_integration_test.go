package account_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// exportBody confirms an export with password.
func exportBody(password string) string {
	return fmt.Sprintf(`{"password":%q}`, password)
}

// secretKeys finds keys anywhere in a JSON value that name a secret: a
// password, hash, digest, token, secret, CSRF token or recovery code.
func secretKeys(value any, path string) []string {
	var found []string
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			lower := strings.ToLower(key)
			for _, word := range []string{"password", "hash", "digest", "token", "secret", "csrf", "recoverycode", "sealed"} {
				if strings.Contains(lower, word) {
					found = append(found, path+"."+key)
				}
			}
			if _, number := child.(float64); key == "id" && number {
				found = append(found, path+".id (numeric)")
			}
			found = append(found, secretKeys(child, path+"."+key)...)
		}
	case []any:
		for i, child := range v {
			found = append(found, secretKeys(child, fmt.Sprintf("%s[%d]", path, i))...)
		}
	}
	return found
}

// Download my data: the password first (a wrong one 422, counted by the
// auth limits but not the export's), then the file: the profile, MFA on
// (never its secret or codes), live sessions (never a token), memberships,
// the owned workspace's projects and the invitations the account sent,
// and the events about or by it; recorded as auth.data_exported. No
// secret, internal ID or anyone else's data appears anywhere in it, by
// key or by value: not another member's name or email, an invitation a
// co-owner sent, someone else's IP on an event about the account, nor the
// account's own admin searches, nor what it did to strangers from the
// dashboard. The sixth export in a day is 429.
func TestDownloadMyData(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	account := apitest.WithMFA(t, db, f)
	user := account.User
	session := apitest.Must(factory.SessionFor(ctx, db, f, user.ID))(t)
	other := apitest.Must(factory.SessionFor(ctx, db, f, user.ID))(t)
	coOwner := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	mine := apitest.Must(factory.WorkspaceFor(ctx, db, f, user.ID, factory.Named("Mine")))(t)
	apitest.Must(factory.MemberOf(ctx, db, f, coOwner.ID, mine.ID, auth.RoleOwner))(t)
	projects := apitest.Must(factory.ProjectsIn(mine.ID).Create(ctx, db, f, 2))(t)
	sent := apitest.Must(factory.InvitationsTo(mine.ID).State(func(i *factory.WorkspaceInvitation) { i.InvitedBy = user.ID }).State(factory.Sent).CreateOne(ctx, db, f))(t)
	theirs := apitest.Must(factory.InvitationsTo(mine.ID).State(func(i *factory.WorkspaceInvitation) { i.InvitedBy = coOwner.ID }).CreateOne(ctx, db, f))(t)
	joined := apitest.Must(factory.WorkspaceFor(ctx, db, f, coOwner.ID, factory.Named("Theirs")))(t)
	apitest.Must(factory.MemberOf(ctx, db, f, user.ID, joined.ID, auth.RoleViewer))(t)
	event := func(name string, actor, about int64, ip string, payload map[string]any) {
		apitest.Must(factory.Events.State(func(e *factory.Event) {
			e.Name, e.ActorID, e.UserID, e.IP, e.Payload, e.CreatedAt = name, actor, about, ip, payload, time.Now().Add(-time.Hour)
		}).CreateOne(ctx, db, f))(t)
	}
	event("project.created", user.ID, 0, "198.51.100.10", map[string]any{"project": projects[0].PublicID, "workspace": mine.PublicID, "name": projects[0].Name})
	event("workspace.member.updated", coOwner.ID, user.ID, "203.0.113.99", map[string]any{"workspace": joined.PublicID, "user": user.PublicID})
	event("auth.login_attempted", 0, user.ID, "203.0.113.98", map[string]any{"email": user.Email, "user": user.PublicID, "result": "wrong_password"})
	event("admin.user.listed", user.ID, 0, "198.51.100.10", map[string]any{"admin": user.PublicID, "q": "someone-searched@example.test", "sort": "name", "page": 1})
	event("project.created", coOwner.ID, 0, "203.0.113.97", map[string]any{"project": projects[1].PublicID, "workspace": mine.PublicID, "name": "by the co-owner"})
	// The account is an operator too, and acts on strangers from the
	// dashboard: what that records holds their data, and stays out.
	stranger := strangerActedOnByAdmin(t, h, db, f, user.ID)

	apitest.Call(t, h, "POST", "/api/auth/export", exportBody(factory.DefaultPassword), "", session.Cookie(), 403)
	apitest.Call(t, h, "POST", "/api/auth/export", exportBody(factory.DefaultPassword), session.CSRFToken, nil, 401)
	wrong := apitest.Call(t, h, "POST", "/api/auth/export", exportBody("not my password"), session.CSRFToken, session.Cookie(), 422)
	if apitest.ErrorField(t, wrong) != "password" || len(apitest.EventsNamed(t, db, "auth.data_exported")) != 0 {
		t.Fatalf("a wrong password: %s", wrong.Body.String())
	}

	w := apitest.Call(t, h, "POST", "/api/auth/export", exportBody(factory.DefaultPassword), session.CSRFToken, session.Cookie(), 200)
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		w.Header().Get("Content-Disposition") != `attachment; filename="lewcontext-export-`+time.Now().UTC().Format(time.DateOnly)+`.json"` ||
		w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %v", w.Header())
	}
	body := w.Body.String()
	var raw any
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &raw))(t)
	if found := secretKeys(raw, ""); len(found) > 0 {
		t.Fatalf("secret fields in the export: %v", found)
	}
	var hash string
	apitest.Must(0, db.NewSelect().Table("users").Column("password_hash").Where("id = ?", user.ID).Scan(ctx, &hash))(t)
	for name, value := range map[string]string{
		"the password hash":            hash,
		"the TOTP secret, sealed":      hex.EncodeToString(account.TOTP.Secret),
		"the TOTP secret":              string(account.TOTP.Plain),
		"a recovery code":              account.Codes[0].Code,
		"a recovery code's digest":     account.Codes[0].CodeHash,
		"a session's token":            session.Token,
		"a session's digest":           session.TokenHash,
		"a CSRF token":                 other.CSRFToken,
		"an invitation's token":        sent.Token,
		"an invitation's digest":       sent.TokenHash.String,
		"the co-owner's email":         coOwner.Email,
		"the co-owner's name":          coOwner.Name,
		"the co-owner's public ID":     coOwner.PublicID,
		"an invitation they sent":      theirs.Email,
		"their IP":                     "203.0.113.99",
		"an attacker's IP":             "203.0.113.98",
		"the co-owner's event's IP":    "203.0.113.97",
		"the account's admin searches": "someone-searched@example.test",
		"a stranger's invitee":         stranger.invitee,
		"an address it unblocked":      stranger.unblocked,
		"a stranger's workspace":       stranger.workspace,
		"a stranger it suspended":      stranger.user,
		"its reason as an admin":       adminReason,
	} {
		if value != "" && strings.Contains(body, value) {
			t.Errorf("the export holds %s", name)
		}
	}
	var export store.AccountExport
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &export))(t)
	// Last seen: its signed-in requests moved users.last_seen_at.
	if export.Account.ID != user.PublicID || export.Account.Email != user.Email || export.Account.MFAEnabledAt == nil ||
		export.Account.LastSeenAt == nil || time.Since(*export.Account.LastSeenAt) > time.Minute {
		t.Errorf("account: %+v", export.Account)
	}
	if len(export.Sessions) != 2 || len(export.Memberships) != 2 {
		t.Errorf("sessions %d, memberships %+v", len(export.Sessions), export.Memberships)
	}
	if len(export.OwnedWorkspaces) != 1 || export.OwnedWorkspaces[0].Name != "Mine" || len(export.OwnedWorkspaces[0].Projects) != 2 ||
		len(export.OwnedWorkspaces[0].Invitations) != 1 || export.OwnedWorkspaces[0].Invitations[0].Email != sent.Email {
		t.Errorf("owned workspaces: %+v", export.OwnedWorkspaces)
	}
	byName := map[string][]store.ExportedEvent{}
	for _, e := range export.Events {
		byName[e.Name] = append(byName[e.Name], e)
	}
	if created := byName["project.created"]; len(created) != 1 || created[0].IP != "198.51.100.10" {
		t.Errorf("its own event, with its IP: %+v", created)
	}
	for _, name := range []string{"workspace.member.updated", "auth.login_attempted"} {
		if about := byName[name]; len(about) != 1 || about[0].IP != "" || about[0].UserAgent != "" {
			t.Errorf("%s, about it by someone else: %+v", name, about)
		}
	}
	for _, name := range []string{"admin.user.listed", "workspace.invitation.revoked", "email.unblocked", "auth.suspended"} {
		if len(byName[name]) != 0 {
			t.Errorf("its admin reads and actions were exported: %s", name)
		}
	}
	e := apitest.OnlyEvent(t, db, "auth.data_exported")
	if !apitest.Is(e.UserID, user.ID) || !apitest.Is(e.ActorID, user.ID) || e.Raw != `{"user": "`+user.PublicID+`"}` {
		t.Fatalf("auth.data_exported: %+v", e)
	}
	checked := apitest.EventsNamed(t, db, "auth.password_checked")
	if len(checked) != 2 || checked[1].Payload["step"] != "export_data" {
		t.Fatalf("password checks: %+v", checked)
	}

	// Five a day: four more, then 429 (with rate_limit.reached).
	for range 4 {
		apitest.Call(t, h, "POST", "/api/auth/export", exportBody(factory.DefaultPassword), session.CSRFToken, session.Cookie(), 200)
	}
	refused := apitest.Call(t, h, "POST", "/api/auth/export", exportBody(factory.DefaultPassword), session.CSRFToken, session.Cookie(), 429)
	apitest.RetryWithin(t, refused, 24*time.Hour)
	apitest.Reached(t, db, "export:per-user:block")
	if n := len(apitest.EventsNamed(t, db, "auth.data_exported")); n != 5 {
		t.Fatalf("%d exports recorded", n)
	}
}

// adminReason is the reason the account gives for its dashboard actions.
const adminReason = "Spam reported by several people, see ticket 4521."

// actedOn is what the account's dashboard actions touched, none of it its
// own data.
type actedOn struct{ invitee, unblocked, workspace, user string }

// strangerActedOnByAdmin makes adminID an operator with a fresh code and,
// through the dashboard's routes, revokes a stranger's workspace's
// invitation, unblocks an address and suspends the stranger: each records
// an event with the admin as actor that isn't an admin.* name
// (workspace.invitation.revoked, email.unblocked, auth.suspended).
func strangerActedOnByAdmin(t *testing.T, h http.Handler, db bun.IDB, f *gofakeit.Faker, adminID int64) actedOn {
	t.Helper()
	ctx := context.Background()
	apitest.Must(factory.Admins.State(factory.AdminFor(adminID)).State(factory.AdminRole(auth.AdminOperator)).CreateOne(ctx, db, f))(t)
	session := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(adminID)).State(factory.SteppedUp).CreateOne(ctx, db, f))(t)
	stranger := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	spam := apitest.Must(factory.WorkspaceFor(ctx, db, f, stranger.ID, factory.Named("Spam")))(t)
	done := actedOn{invitee: "third-party-invitee@example.test", unblocked: "blocked-person@example.test", workspace: spam.PublicID, user: stranger.PublicID}
	apitest.Must(factory.InvitationsTo(spam.ID).State(func(i *factory.WorkspaceInvitation) { i.InvitedBy, i.Email = stranger.ID, done.invitee }).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.EmailBlocks.State(factory.Blocked(done.unblocked)).CreateOne(ctx, db, f))(t)
	for _, action := range []struct{ method, path, body string }{
		{"DELETE", "/api/admin/workspaces/" + spam.PublicID + "/invitations", jsonBody(map[string]any{"reason": adminReason})},
		{"DELETE", "/api/admin/email-blocks", jsonBody(map[string]any{"email": done.unblocked, "reason": adminReason})},
		{"POST", "/api/admin/users/" + stranger.PublicID + "/suspension", jsonBody(map[string]any{"reason": adminReason})},
	} {
		r := httptest.NewRequest(action.method, action.path, strings.NewReader(action.body))
		r.Header.Set("Origin", "http://localhost:3000")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Admin-Key", factory.AdminRouteKey)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.AddCookie(session.Cookie())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code >= 300 {
			t.Fatalf("%s %s: %d %s", action.method, action.path, w.Code, w.Body.String())
		}
	}
	for _, name := range []string{"workspace.invitation.revoked", "email.unblocked", "auth.suspended"} {
		if e := apitest.OnlyEvent(t, db, name); !apitest.Is(e.ActorID, adminID) {
			t.Fatalf("%s isn't the admin's: %s", name, e.Raw)
		}
	}
	return done
}

// jsonBody is a request body as JSON.
func jsonBody(body map[string]any) string {
	raw, _ := json.Marshal(body)
	return string(raw)
}

// A suspended account has no session, so it can't export: the usual 401.
func TestSuspendedAccountsCantExport(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	session := apitest.SignedIn(t, db, f)
	publicID := apitest.PublicIDOf(t, db, session.UserID)
	apitest.Must(s.Repo.SuspendUser(ctx, publicID, store.Suspension{Reason: "A test suspension, for export."}, nil))(t)
	apitest.Call(t, h, "POST", "/api/auth/export", exportBody(factory.DefaultPassword), session.CSRFToken, session.Cookie(), 401)
	if len(apitest.EventsNamed(t, db, "auth.data_exported")) != 0 {
		t.Fatal("a suspended account's export was recorded")
	}
}

// An operator's note stays out of the export: suspended and unsuspended
// from the command line, the account's auth.suspended and auth.unsuspended
// are exported with who did it (by cli), never the reason typed.
func TestExportLeavesOutOperatorNotes(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	const suspended, unsuspended = "Operator note: chargeback fraud, ticket 77.", "Operator note: cleared by the payments team."
	apitest.Must(s.Repo.SuspendUser(ctx, user.PublicID, store.Suspension{Reason: suspended}, nil))(t)
	apitest.Must(s.Repo.UnsuspendUser(ctx, user.PublicID, unsuspended, nil))(t)
	session := apitest.Must(factory.SessionFor(ctx, db, f, user.ID))(t)
	w := apitest.Call(t, h, "POST", "/api/auth/export", exportBody(factory.DefaultPassword), session.CSRFToken, session.Cookie(), 200)
	if body := w.Body.String(); strings.Contains(body, "Operator note") {
		t.Fatalf("the export holds an operator's note: %s", body)
	}
	var export store.AccountExport
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &export))(t)
	found := 0
	for _, e := range export.Events {
		if e.Name != "auth.suspended" && e.Name != "auth.unsuspended" {
			continue
		}
		found++
		var payload map[string]any
		apitest.Must(0, json.Unmarshal(e.Payload, &payload))(t)
		if _, ok := payload["reason"]; ok || payload["by"] != "cli" || payload["user"] != user.PublicID {
			t.Errorf("%s: %v", e.Name, payload)
		}
	}
	if found != 2 {
		t.Fatalf("%d suspension events exported", found)
	}
	// Stored as they were: only the export leaves the note out.
	if e := apitest.OnlyEvent(t, db, "auth.suspended"); e.Payload["reason"] != suspended {
		t.Fatalf("auth.suspended as stored: %s", e.Raw)
	}
}
