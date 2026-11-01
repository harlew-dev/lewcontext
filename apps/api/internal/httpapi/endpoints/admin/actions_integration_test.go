package admin_test

import (
	"context"
	"encoding/json"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"strings"
	"testing"
	"time"
)

// reasonBody is an action's body: the reason, and any other fields.
func reasonBody(reason string, fields ...any) string {
	body := map[string]any{"reason": reason}
	for i := 0; i+1 < len(fields); i += 2 {
		body[fields[i].(string)] = fields[i+1]
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// actionCase is one account-and-access action, as a request: its method,
// path and a valid body.
type actionCase struct {
	name, method, path, body string
}

// actionCases are A6's actions, each on targets made for it: an ordinary
// account (with MFA on, a session and a live invitation), its workspace,
// another admin, a failed delivery and a live IP block.
func actionCases(t *testing.T, db bun.IDB, f *gofakeit.Faker) []actionCase {
	t.Helper()
	ctx := context.Background()
	target := apitest.WithMFA(t, db, f)
	apitest.Must(factory.SessionFor(ctx, db, f, target.User.ID))(t)
	workspace := apitest.OwnedWorkspace(t, db, f, target.User.ID)
	// Sent by someone else, so suspending the target leaves it.
	apitest.Must(factory.WorkspaceInvitations.State(func(i *factory.WorkspaceInvitation) { i.WorkspaceID = workspace.ID }).CreateOne(ctx, db, f))(t)
	other := withAdmin(t, db, f)
	// Two failed deliveries: one retried alone, one with the bulk retry.
	failed := apitest.Must(factory.Events.Create(ctx, db, f, 2))(t)
	for _, e := range failed {
		apitest.Must(factory.DeliveryOf(e.ID, "mail.password_reset").State(func(d *factory.EventDelivery) { d.FailedAt = time.Now() }).CreateOne(ctx, db, f))(t)
	}
	event := failed[0]
	block := apitest.Must(factory.IPBlocks.State(factory.BlockOf("203.0.113.0/24")).CreateOne(ctx, db, f))(t)
	user := "/api/admin/users/" + target.User.PublicID
	return []actionCase{
		{"sign out everywhere", "DELETE", user + "/sessions", reasonBody(reason)},
		{"reset MFA", "DELETE", user + "/mfa", reasonBody(reason, "confirmed", true)},
		{"suspend", "POST", user + "/suspension", reasonBody(reason)},
		{"revoke invitations", "DELETE", "/api/admin/workspaces/" + workspace.PublicID + "/invitations", reasonBody(reason)},
		{"retry a delivery", "POST", "/api/admin/deliveries/" + event.PublicID + "/mail.password_reset/retry", reasonBody(reason)},
		{"retry failed deliveries", "POST", "/api/admin/deliveries/retry", reasonBody(reason, "listener", "mail.password_reset", "failedSince", time.Now().Add(-time.Hour).Format(time.RFC3339))},
		{"block an IP", "POST", "/api/admin/ip-blocks", reasonBody(reason, "network", "198.51.100.0/24", "expiresIn", "24h")},
		{"unblock an IP", "DELETE", "/api/admin/ip-blocks/" + block.PublicID, reasonBody(reason)},
		{"sign another admin out", "DELETE", "/api/admin/admins/" + other.User.PublicID + "/sessions", reasonBody(reason)},
	}
}

// Every account-and-access action: a viewer gets 403 (never asked for a
// code), an operator outside the step-up window 403 with stepUp, no CSRF
// token 403, none of which counts against admin-action:per-user; then no
// reason, or a short one, 422 on reason; the 61st in an hour 429. And with
// all of it, it works, recording its admin.* event with the reason.
func TestAdminActionGuards(t *testing.T) {
	s, db, _, _ := mailServer(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, viewer := adminSignedIn(t, db, f)
	stepped := apitest.Must(factory.AdminSessions.State(factory.AdminSessionFor(viewer.UserID)).State(factory.SteppedUp).CreateOne(ctx, db, f))(t)
	_, cold := operatorSignedIn(t, db, f)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	for _, c := range actionCases(t, db, f) {
		t.Run(c.name, func(t *testing.T) {
			for _, v := range []factory.AdminSession{viewer, stepped} {
				if w := adminCall(t, h, c.method, c.path, c.body, v.CSRFToken, 403, v.Cookie()); strings.Contains(w.Body.String(), "stepUp") {
					t.Fatalf("a viewer was asked for a code: %s", w.Body.String())
				}
			}
			var asked map[string]any
			apitest.Must(0, json.Unmarshal(adminCall(t, h, c.method, c.path, c.body, cold.CSRFToken, 403, cold.Cookie()).Body.Bytes(), &asked))(t)
			if asked["stepUp"] != true {
				t.Fatalf("outside the window: %+v", asked)
			}
			adminCall(t, h, c.method, c.path, c.body, "", 403, session.Cookie())
			if n := apitest.CountRateLimits(t, db, "admin-action:per-user:%"); n != 0 {
				t.Fatalf("refusals before the step-up counted: %d", n)
			}
			for _, bad := range []string{reasonBody(""), reasonBody("too short"), reasonBody("hidden ‮ text here")} {
				var fields map[string]any
				apitest.Must(0, json.Unmarshal([]byte(c.body), &fields))(t)
				var reasonOnly map[string]any
				apitest.Must(0, json.Unmarshal([]byte(bad), &reasonOnly))(t)
				fields["reason"] = reasonOnly["reason"]
				body, _ := json.Marshal(fields)
				if w := adminCall(t, h, c.method, c.path, string(body), session.CSRFToken, 422, session.Cookie()); apitest.ErrorField(t, w) != "reason" {
					t.Fatalf("reason %s: %s", bad, w.Body.String())
				}
			}
			before := len(adminActionEvents(t, db))
			if w := adminSend(h, c.method, c.path, c.body, factory.AdminRouteKey, session.CSRFToken, session.Cookie()); w.Code >= 300 {
				t.Fatalf("the action: %d %s", w.Code, w.Body.String())
			}
			actions := adminActionEvents(t, db)
			if len(actions) != before+1 {
				t.Fatalf("admin.* events: %d after %d", len(actions), before)
			}
			last := actions[len(actions)-1]
			if last.Payload["admin"] != operator.User.PublicID || last.Payload["reason"] != reason || !apitest.Is(last.ActorID, operator.User.ID) || last.UserID.Valid || last.WorkspaceID.Valid {
				t.Fatalf("%s: %+v", last.Name, last)
			}
			apitest.Must(db.NewDelete().TableExpr("rate_limits").Where("TRUE").Exec(ctx))(t)
		})
	}
	// admin-action:per-user: the 61st in an hour is 429, for every action.
	apitest.Must(factory.RateLimits.State(factory.ForLimit("admin-action:per-user", operator.User.PublicID)).State(factory.Exhausted(60)).CreateOne(ctx, db, f))(t)
	for _, c := range actionCases(t, db, f) {
		w := adminCall(t, h, c.method, c.path, c.body, session.CSRFToken, 429, session.Cookie())
		apitest.RetryWithin(t, w, time.Hour)
	}
	apitest.CheckPayloadIDs(t, db)
}

// adminActionEvents are the admin.* events recording an action (not a
// read, a sign-in or a session), oldest first.
func adminActionEvents(t *testing.T, db bun.IDB) []apitest.Event {
	t.Helper()
	var found []apitest.Event
	for _, name := range []string{"admin.user.signed_out", "admin.user.mfa_reset", "admin.user.suspended", "admin.user.unsuspended", "admin.workspace.invitations_revoked", "admin.delivery.retried", "admin.ip.blocked", "admin.ip.unblocked", "admin.admin.signed_out"} {
		found = append(found, apitest.EventsNamed(t, db, name)...)
	}
	return found
}

// Sign out everywhere: every app session of the account ends, recorded
// once with the count, in the action's transaction. Never yourself, never
// another admin, 404 for an unknown account.
func TestAdminSignOutEverywhere(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	target := apitest.SignedIn(t, db, f)
	apitest.Must(factory.SessionFor(ctx, db, f, target.UserID))(t)
	// A sign-in waiting for its code ends too.
	apitest.Must(factory.MFAChallenges.State(factory.ChallengeFor(target.UserID)).CreateOne(ctx, db, f))(t)
	bystander := apitest.SignedIn(t, db, f)
	path := "/api/admin/users/" + apitest.PublicIDOf(t, db, target.UserID) + "/sessions"
	adminCall(t, h, "DELETE", path, reasonBody(reason), session.CSRFToken, 204, session.Cookie())
	if n := apitest.CountRows(t, db, "sessions", target.UserID) + apitest.CountRows(t, db, "mfa_challenges", target.UserID); n != 0 {
		t.Fatalf("sessions or challenges left: %d", n)
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", target.Cookie(), 401)
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", bystander.Cookie(), 200)
	e := apitest.OnlyEvent(t, db, "admin.user.signed_out")
	if e.Payload["user"] != apitest.PublicIDOf(t, db, target.UserID) || e.Payload["sessionsRevoked"] != float64(2) || e.Payload["reason"] != reason {
		t.Fatalf("admin.user.signed_out: %+v", e.Payload)
	}
	// Yourself, another admin, nobody.
	if w := adminCall(t, h, "DELETE", "/api/admin/users/"+operator.User.PublicID+"/sessions", reasonBody(reason), session.CSRFToken, 403, session.Cookie()); !strings.Contains(w.Body.String(), "your own account") {
		t.Fatalf("on yourself: %s", w.Body.String())
	}
	other := withAdmin(t, db, f)
	otherSession := apitest.Must(factory.SessionFor(ctx, db, f, other.User.ID))(t)
	adminCall(t, h, "DELETE", "/api/admin/users/"+other.User.PublicID+"/sessions", reasonBody(reason), session.CSRFToken, 403, session.Cookie())
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", otherSession.Cookie(), 200)
	adminCall(t, h, "DELETE", "/api/admin/users/usr_2yEHYLwiwPVhLW/sessions", reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	adminCall(t, h, "DELETE", "/api/admin/users/not-an-id/sessions", reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	if len(apitest.EventsNamed(t, db, "admin.user.signed_out")) != 1 {
		t.Fatal("a refusal was recorded")
	}
}

// Resetting MFA from the dashboard: only with the out-of-band
// confirmation; MFA off (secret, recovery codes, challenges), every
// session out, auth.mfa.disabled (reset, the admin as actor) and
// admin.user.mfa_reset in one transaction. Never an admin's: admins must
// keep MFA (the command line resets it and reopens their setup window).
func TestAdminResetMFA(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	target := apitest.WithMFA(t, db, f)
	app := apitest.Must(factory.SessionFor(ctx, db, f, target.User.ID))(t)
	path := "/api/admin/users/" + target.User.PublicID + "/mfa"
	for _, body := range []string{reasonBody(reason), reasonBody(reason, "confirmed", false)} {
		if w := adminCall(t, h, "DELETE", path, body, session.CSRFToken, 422, session.Cookie()); apitest.ErrorField(t, w) != "confirmed" {
			t.Fatalf("without the confirmation: %s", w.Body.String())
		}
	}
	adminCall(t, h, "DELETE", path, reasonBody(reason, "confirmed", true), session.CSRFToken, 204, session.Cookie())
	for _, table := range []string{"mfa_totp", "mfa_recovery_codes", "sessions"} {
		if n := apitest.CountRows(t, db, table, target.User.ID); n != 0 {
			t.Fatalf("%s left: %d", table, n)
		}
	}
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", app.Cookie(), 401)
	disabled := apitest.OnlyEvent(t, db, "auth.mfa.disabled")
	action := apitest.OnlyEvent(t, db, "admin.user.mfa_reset")
	if disabled.Payload["reason"] != "reset" || !apitest.Is(disabled.ActorID, operator.User.ID) || !apitest.Is(disabled.UserID, target.User.ID) {
		t.Fatalf("auth.mfa.disabled: %+v", disabled)
	}
	if action.Payload["user"] != target.User.PublicID || action.Payload["sessionsRevoked"] != float64(1) {
		t.Fatalf("admin.user.mfa_reset: %+v", action.Payload)
	}
	sameTransaction(t, db, "auth.mfa.disabled", "admin.user.mfa_reset")
	// The password alone signs in now.
	apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(target.User.Email), "", nil, 200)
	// Again: MFA is off, 409.
	adminCall(t, h, "DELETE", path, reasonBody(reason, "confirmed", true), session.CSRFToken, 409, session.Cookie())
	// Another admin's MFA, and yours: refused, nothing changed.
	other := withAdmin(t, db, f)
	adminCall(t, h, "DELETE", "/api/admin/users/"+other.User.PublicID+"/mfa", reasonBody(reason, "confirmed", true), session.CSRFToken, 403, session.Cookie())
	adminCall(t, h, "DELETE", "/api/admin/users/"+operator.User.PublicID+"/mfa", reasonBody(reason, "confirmed", true), session.CSRFToken, 403, session.Cookie())
	for _, id := range []int64{other.User.ID, operator.User.ID} {
		if apitest.CountRows(t, db, "mfa_totp", id) != 1 {
			t.Fatal("an admin's MFA was reset")
		}
	}
}

// sameTransaction fails unless the events named have the same created_at:
// PostgreSQL's now() is the transaction's start.
func sameTransaction(t *testing.T, db bun.IDB, names ...string) {
	t.Helper()
	var times []time.Time
	err := db.NewSelect().TableExpr("events").Column("created_at").Where("name IN (?)", bun.In(names)).Scan(context.Background(), &times)
	if err != nil || len(times) < 2 {
		t.Fatalf("events %v: %v %v", names, times, err)
	}
	for _, at := range times {
		if !at.Equal(times[0]) {
			t.Fatalf("not one transaction: %v", times)
		}
	}
}

// Signing another admin out ends every admin and app session of theirs,
// recorded as admin.admin.signed_out; it's the one action on another
// admin. Not on yourself (your own other sessions have their route), and
// 404 for an account that isn't an admin.
func TestAdminSignAnotherAdminOut(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	other, otherAdmin := adminSignedIn(t, db, f)
	otherApp := apitest.Must(factory.SessionFor(ctx, db, f, other.User.ID))(t)
	adminCall(t, h, "DELETE", "/api/admin/admins/"+other.User.PublicID+"/sessions", reasonBody(reason), session.CSRFToken, 204, session.Cookie())
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, otherAdmin.Cookie())
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", otherApp.Cookie(), 401)
	e := apitest.OnlyEvent(t, db, "admin.admin.signed_out")
	if e.Payload["user"] != other.User.PublicID || e.Payload["adminSessionsRevoked"] != float64(1) || e.Payload["sessionsRevoked"] != float64(1) || !apitest.Is(e.ActorID, operator.User.ID) {
		t.Fatalf("admin.admin.signed_out: %+v", e)
	}
	// They're still an admin, and can sign in again.
	adminLogin(t, h, other.User.Email, 200)
	if w := adminCall(t, h, "DELETE", "/api/admin/admins/"+operator.User.PublicID+"/sessions", reasonBody(reason), session.CSRFToken, 403, session.Cookie()); !strings.Contains(w.Body.String(), "Sign out my other sessions") {
		t.Fatalf("yourself: %s", w.Body.String())
	}
	plain := apitest.SignedIn(t, db, f)
	adminCall(t, h, "DELETE", "/api/admin/admins/"+apitest.PublicIDOf(t, db, plain.UserID)+"/sessions", reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	apitest.Call(t, h, "GET", "/api/auth/me", "", "", plain.Cookie(), 200)
}

// Revoking a workspace's pending invitations: every live one, each with
// workspace.invitation.revoked (the admin as actor), and one
// admin.workspace.invitations_revoked with the count; their links stop
// working, the workspace's members and projects are untouched. 409 when
// none is live, 404 for an unknown workspace.
func TestAdminRevokeWorkspaceInvitations(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	apitest.Must(factory.WorkspaceInvitations.State(func(i *factory.WorkspaceInvitation) { i.WorkspaceID = workspace.ID }).Create(ctx, db, f, 3))(t)
	expired := apitest.Must(factory.WorkspaceInvitations.State(func(i *factory.WorkspaceInvitation) {
		i.WorkspaceID, i.ExpiresAt = workspace.ID, time.Now().Add(-time.Hour)
	}).CreateOne(ctx, db, f))(t)
	elsewhere := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	apitest.Must(factory.WorkspaceInvitations.State(func(i *factory.WorkspaceInvitation) { i.WorkspaceID = elsewhere.ID }).CreateOne(ctx, db, f))(t)
	path := "/api/admin/workspaces/" + workspace.PublicID + "/invitations"
	adminCall(t, h, "DELETE", path, reasonBody(reason), session.CSRFToken, 204, session.Cookie())
	var left []string
	apitest.Must(0, db.NewSelect().TableExpr("workspace_invitations").Column("public_id").Scan(ctx, &left))(t)
	if len(left) != 2 || !contains(left, expired.PublicID) {
		t.Fatalf("invitations left: %v", left)
	}
	revoked := apitest.EventsNamed(t, db, "workspace.invitation.revoked")
	if len(revoked) != 3 || !apitest.Is(revoked[0].ActorID, operator.User.ID) || !apitest.Is(revoked[0].WorkspaceID, workspace.ID) || revoked[0].Payload["workspace"] != workspace.PublicID {
		t.Fatalf("workspace.invitation.revoked: %+v", revoked)
	}
	e := apitest.OnlyEvent(t, db, "admin.workspace.invitations_revoked")
	if e.Payload["workspace"] != workspace.PublicID || e.Payload["count"] != float64(3) {
		t.Fatalf("admin.workspace.invitations_revoked: %+v", e.Payload)
	}
	sameTransaction(t, db, "workspace.invitation.revoked", "admin.workspace.invitations_revoked")
	apitest.Call(t, h, "GET", apitest.WorkspacePath(workspace.PublicID, ""), "", "", owner.Cookie(), 200)
	if w := adminCall(t, h, "DELETE", path, reasonBody(reason), session.CSRFToken, 409, session.Cookie()); !strings.Contains(w.Body.String(), "no pending invitations") {
		t.Fatalf("none left: %s", w.Body.String())
	}
	adminCall(t, h, "DELETE", "/api/admin/workspaces/wsp_2yEHYLwiwPVhLW/invitations", reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	if len(apitest.EventsNamed(t, db, "admin.workspace.invitations_revoked")) != 1 {
		t.Fatal("a refusal was recorded")
	}
	// The workspace's page says how many are pending.
	detail := adminCall(t, h, "GET", "/api/admin/workspaces/"+elsewhere.PublicID, "", "", 200, session.Cookie())
	if !strings.Contains(detail.Body.String(), `"pendingInvitations":1`) {
		t.Fatalf("pendingInvitations: %s", detail.Body.String())
	}
}

func contains(list []string, value string) bool {
	for _, v := range list {
		if v == value {
			return true
		}
	}
	return false
}
