package account_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
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
// account's own admin searches. The sixth export in a day is 429.
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
	} {
		if value != "" && strings.Contains(body, value) {
			t.Errorf("the export holds %s", name)
		}
	}
	var export store.AccountExport
	apitest.Must(0, json.Unmarshal(w.Body.Bytes(), &export))(t)
	if export.Account.ID != user.PublicID || export.Account.Email != user.Email || export.Account.MFAEnabledAt == nil {
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
	if len(byName["admin.user.listed"]) != 0 {
		t.Error("its admin reads were exported")
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
