package workspace_test

import (
	"context"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"strings"
	"testing"
	"time"
)

// Deleting an account erases its email, in the same transaction, from the
// events that outlive it: a sign-in attempt in any case, an invitation to
// it, one already minimised (its pseudonym), an admin's Events search and
// a list search for exactly it, with the name beside the email. IPs stay.
// Another address's events, a partial search, and everything after a
// refused deletion are untouched.
func TestDeletingAnAccountErasesItsEmail(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	user := apitest.SignedIn(t, db, f)
	email := apitest.EmailOf(t, db, user.UserID)
	bystander := apitest.SignedIn(t, db, f)
	otherEmail := apitest.EmailOf(t, db, bystander.UserID)
	theirs := apitest.OwnedWorkspace(t, db, f, bystander.UserID)
	event := func(name string, payload map[string]any) int64 {
		e := apitest.Must(factory.Events.State(func(e *factory.Event) {
			e.Name, e.Payload, e.IP, e.CreatedAt = name, payload, "198.51.100.7", time.Now().Add(-time.Hour)
		}).CreateOne(ctx, db, f))(t)
		return e.ID
	}
	ids := map[string]int64{
		"attempt":    event("auth.login_attempted", map[string]any{"email": strings.ToUpper(email), "result": "wrong_password"}),
		"invitation": event("workspace.invitation.created", map[string]any{"email": email, "workspace": theirs.PublicID, "invitation": "inv_00000000000001", "role": "member"}),
		"minimised":  event("auth.registration.requested", map[string]any{"email": s.EmailPseudonym(email)}),
		"named":      event("auth.registered", map[string]any{"email": email, "name": "Ada Example", "user": "usr_00000000000001", "verifiedBy": "code"}),
		"search":     event("admin.event.listed", map[string]any{"admin": "usr_00000000000002", "filters": map[string]any{"email": email, "window": "24h"}}),
		"list":       event("admin.user.listed", map[string]any{"admin": "usr_00000000000002", "q": strings.ToUpper(email), "sort": "name", "page": 1}),
		"partial":    event("admin.user.listed", map[string]any{"admin": "usr_00000000000002", "q": email[:3], "sort": "name", "page": 1}),
		"other":      event("auth.login_attempted", map[string]any{"email": otherEmail, "result": "wrong_password"}),
	}
	payloads := func() map[string]string {
		out := map[string]string{}
		for key, id := range ids {
			var payload, ip string
			apitest.Must(0, db.NewSelect().Table("events").ColumnExpr("payload::text, host(ip)").Where("id = ?", id).Scan(ctx, &payload, &ip))(t)
			if ip != "198.51.100.7" {
				t.Errorf("%s: its IP changed: %s", key, ip)
			}
			out[key] = payload
		}
		return out
	}
	before := payloads()

	// Refused (the only owner of a workspace): nothing erased either.
	alone := apitest.OwnedWorkspace(t, db, f, user.UserID)
	apitest.Call(t, h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), user.CSRFToken, user.Cookie(), 409)
	for key, payload := range payloads() {
		if payload != before[key] {
			t.Fatalf("a refused deletion changed %s: %s", key, payload)
		}
	}
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(alone.PublicID, ""), deleteWorkspaceBody(alone.Name, factory.DefaultPassword), user.CSRFToken, user.Cookie(), 204)
	apitest.Call(t, h, "DELETE", "/api/auth/me", deleteAccountBody(factory.DefaultPassword), user.CSRFToken, user.Cookie(), 204)

	after := payloads()
	for _, key := range []string{"attempt", "invitation", "minimised", "named", "search", "list"} {
		lower := strings.ToLower(after[key])
		if strings.Contains(lower, strings.ToLower(email)) || strings.Contains(after[key], s.EmailPseudonym(email)) || !strings.Contains(after[key], store.Redacted) {
			t.Errorf("%s still names the account: %s", key, after[key])
		}
	}
	if !strings.Contains(after["named"], `"name": "[deleted]"`) || strings.Contains(after["named"], "Ada Example") {
		t.Errorf("the name beside the email: %s", after["named"])
	}
	if !strings.Contains(after["invitation"], `"role": "member"`) || !strings.Contains(after["search"], `"window": "24h"`) {
		t.Errorf("more than the address was erased: %s %s", after["invitation"], after["search"])
	}
	for _, key := range []string{"partial", "other"} {
		if after[key] != before[key] {
			t.Errorf("%s changed: %s", key, after[key])
		}
	}
	// Nothing anywhere in the trail holds the address.
	var left int
	apitest.Must(0, db.NewSelect().Table("events").ColumnExpr("count(*)").Where("lower(payload::text) LIKE ?", "%"+strings.ToLower(email)+"%").Scan(ctx, &left))(t)
	if left != 0 {
		t.Fatalf("%d events still hold the address", left)
	}
}
