package admin_test

import (
	"context"
	"fmt"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/store"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// suspendedAnswer is the sign-in's answer to a suspended account whose
// password (and code) were right.
const suspendedAnswer = `{"error":"This account has been suspended. Contact support.","suspended":true}`

// Suspending ends every way in at once and keeps it shut: the account's
// sessions are deleted, a session made any other way afterwards is refused
// by every session middleware (the API, the recovery routes, the page
// gate), the reset link it had stops working and no new one is sent. Its
// invitations are revoked (the default), its workspaces keep working.
// Unsuspending lets it sign in again.
func TestAdminSuspend(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(*config.Config) {})
	box, deliver := apitest.Mail(t, s.Repo, s.Config.Origin)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	operator, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	target := apitest.SignedIn(t, db, f)
	targetID := apitest.PublicIDOf(t, db, target.UserID)
	email := apitest.EmailOf(t, db, target.UserID)
	workspace := apitest.OwnedWorkspace(t, db, f, target.UserID)
	member := apitest.SignedIn(t, db, f)
	apitest.Must(factory.WorkspaceUsers.State(func(m *factory.WorkspaceUser) { m.WorkspaceID, m.UserID = workspace.ID, member.UserID }).CreateOne(ctx, db, f))(t)
	apitest.Must(factory.WorkspaceInvitations.State(func(i *factory.WorkspaceInvitation) { i.WorkspaceID, i.InvitedBy = workspace.ID, target.UserID }).Create(ctx, db, f, 2))(t)
	byOthers := apitest.Must(factory.WorkspaceInvitations.State(func(i *factory.WorkspaceInvitation) { i.WorkspaceID = workspace.ID }).CreateOne(ctx, db, f))(t)
	reset := apitest.Must(factory.PasswordResets.State(factory.ResetFor(target.UserID)).CreateOne(ctx, db, f))(t)
	path := "/api/admin/users/" + targetID + "/suspension"

	w := adminCall(t, h, "POST", path, reasonBody(reason), session.CSRFToken, 201, session.Cookie())
	if strings.TrimSpace(w.Body.String()) != `{"invitationsRevoked":2,"sessionsRevoked":1}` {
		t.Fatalf("suspended: %s", w.Body.String())
	}
	// Every session middleware refuses it: the old cookie, and a session
	// made afterwards (FindSession's backstop).
	later := apitest.Must(factory.SessionFor(ctx, db, f, target.UserID))(t)
	for _, cookie := range []*factory.Session{&target, &later} {
		apitest.Call(t, h, "GET", "/api/auth/me", "", "", cookie.Cookie(), 401)
		apitest.Call(t, h, "POST", "/api/auth/logout", "", cookie.CSRFToken, cookie.Cookie(), 401)
		apitest.Call(t, h, "GET", apitest.WorkspacePath(workspace.PublicID, ""), "", "", cookie.Cookie(), 401)
		page := apitest.Get(h, "GET", "/app/settings", cookie.Cookie())
		if page.Code != 302 || !strings.HasPrefix(page.Header().Get("Location"), "/auth/login") {
			t.Fatalf("the page gate: %d %v", page.Code, page.Header())
		}
	}
	// The workspace keeps working for everyone else; the membership stays.
	apitest.Call(t, h, "GET", apitest.WorkspacePath(workspace.PublicID, "/projects"), "", "", member.Cookie(), 200)
	if apitest.CountRows(t, db, "workspace_user", target.UserID) != 1 {
		t.Fatal("the membership went")
	}
	// Its invitations were revoked, someone else's in its workspace kept.
	var left []string
	apitest.Must(0, db.NewSelect().TableExpr("workspace_invitations").Column("public_id").Scan(ctx, &left))(t)
	if len(left) != 1 || left[0] != byOthers.PublicID {
		t.Fatalf("invitations left: %v", left)
	}
	// Its reset link stopped working, and a new request sends nothing.
	apitest.Call(t, h, "PATCH", "/api/auth/password-reset", fmt.Sprintf(`{"token":%q,"newPassword":%q}`, reset.Token, apitest.NewPassword), "", nil, 422)
	apitest.Call(t, h, "POST", "/api/auth/password-reset", fmt.Sprintf(`{"email":%q}`, email), "", nil, 202)
	deliver()
	if len(box.Sent) != 0 || apitest.CountRows(t, db, "password_resets", target.UserID) != 0 {
		t.Fatalf("a suspended account was sent: %+v", box.Sent)
	}

	// The events: auth.suspended (the account's, by admin) and
	// admin.user.suspended (the reason), in one transaction.
	fact := apitest.OnlyEvent(t, db, "auth.suspended")
	action := apitest.OnlyEvent(t, db, "admin.user.suspended")
	if fact.Payload["by"] != "admin" || fact.Payload["reason"] != nil || !apitest.Is(fact.UserID, target.UserID) || !apitest.Is(fact.ActorID, operator.User.ID) {
		t.Fatalf("auth.suspended: %+v", fact)
	}
	if action.Payload["user"] != targetID || action.Payload["reason"] != reason || action.Payload["sessionsRevoked"] != float64(1) || action.Payload["invitationsRevoked"] != float64(2) {
		t.Fatalf("admin.user.suspended: %+v", action.Payload)
	}
	sameTransaction(t, db, "auth.suspended", "admin.user.suspended", "workspace.invitation.revoked")

	// Admins see it: the badge, the filter, and the reason, who and when.
	detail := decodeAdmin[store.AdminUserDetail](t, adminCall(t, h, "GET", "/api/admin/users/"+targetID, "", "", 200, session.Cookie()).Body.Bytes())
	if !detail.Suspended || detail.Suspension == nil || detail.Suspension.Reason != reason || detail.Suspension.SuspendedBy == nil || detail.Suspension.SuspendedBy.PublicID != operator.User.PublicID {
		t.Fatalf("the user's page: %+v %+v", detail.AdminUser, detail.Suspension)
	}
	only := decodeAdmin[store.Page[store.AdminUser]](t, adminCall(t, h, "GET", "/api/admin/users?suspended=true", "", "", 200, session.Cookie()).Body.Bytes())
	if len(only.Data) != 1 || only.Data[0].PublicID != targetID {
		t.Fatalf("suspended=true: %+v", only.Data)
	}
	adminCall(t, h, "GET", "/api/admin/users?suspended=yes", "", "", 400, session.Cookie())
	if listed := apitest.EventsNamed(t, db, "admin.user.listed"); listed[len(listed)-1].Payload["suspended"] != true {
		t.Fatalf("the filter wasn't recorded: %+v", listed[len(listed)-1].Payload)
	}

	// Again: 409. Unsuspending: 204, and it signs in again.
	adminCall(t, h, "POST", path, reasonBody(reason), session.CSRFToken, 409, session.Cookie())
	apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(email), "", nil, 403)
	adminCall(t, h, "DELETE", path, reasonBody(reason), session.CSRFToken, 204, session.Cookie())
	apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(email), "", nil, 200)
	unsuspended := apitest.OnlyEvent(t, db, "admin.user.unsuspended")
	if unsuspended.Payload["user"] != targetID || unsuspended.Payload["reason"] != reason || apitest.OnlyEvent(t, db, "auth.unsuspended").Payload["by"] != "admin" {
		t.Fatalf("admin.user.unsuspended: %+v", unsuspended.Payload)
	}
	adminCall(t, h, "DELETE", path, reasonBody(reason), session.CSRFToken, 409, session.Cookie())

	// Without revoking invitations, when asked.
	other := apitest.SignedIn(t, db, f)
	theirs := apitest.Must(factory.WorkspaceInvitations.State(func(i *factory.WorkspaceInvitation) { i.WorkspaceID, i.InvitedBy = workspace.ID, other.UserID }).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "POST", "/api/admin/users/"+apitest.PublicIDOf(t, db, other.UserID)+"/suspension", reasonBody(reason, "revokeInvitations", false), session.CSRFToken, 201, session.Cookie())
	if !apitest.Must(db.NewSelect().TableExpr("workspace_invitations").Where("public_id = ?", theirs.PublicID).Exists(ctx))(t) {
		t.Fatal("an invitation was revoked though not asked")
	}

	// Never yourself, never an admin (only the command line, after revoke).
	adminCall(t, h, "POST", "/api/admin/users/"+operator.User.PublicID+"/suspension", reasonBody(reason), session.CSRFToken, 403, session.Cookie())
	admin := withAdmin(t, db, f)
	adminCall(t, h, "POST", "/api/admin/users/"+admin.User.PublicID+"/suspension", reasonBody(reason), session.CSRFToken, 403, session.Cookie())
	adminCall(t, h, "POST", "/api/admin/users/usr_2yEHYLwiwPVhLW/suspension", reasonBody(reason), session.CSRFToken, 404, session.Cookie())
	apitest.CheckPayloadIDs(t, db)
}

// An invitation the account sends while it's being suspended doesn't
// outlive the suspension, nor reach anyone's inbox: a request whose session
// was checked just before waits for the account's row (CreateInvitation's
// lockInviter, under SuspendUser's lock) and answers 401 as a dead session
// would, or finishes first and is revoked with the others.
func TestSuspendingStopsAnInvitationInFlight(t *testing.T) {
	_, db := server(t)
	s := serverWith(t, db, func(*config.Config) {})
	box, deliver := apitest.Mail(t, s.Repo, s.Config.Origin)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	_, session := operatorSignedIn(t, db, f, factory.SteppedUp)
	invite := func(inviter factory.Session, workspace factory.Workspace, email string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"email":%q,"role":"member"}`, email)
		return apitest.Send(h, "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), body, inviter.CSRFToken, inviter.Cookie())
	}
	var inviters []int64

	// A suspension holding the account's row, not yet committed (as
	// SuspendUser does), while the request is past its session check.
	held := apitest.SignedIn(t, db, f)
	heldWorkspace := apitest.OwnedWorkspace(t, db, f, held.UserID)
	inviters = append(inviters, held.UserID)
	tx := apitest.Must(db.BeginTx(ctx, nil))(t)
	defer func() { _ = tx.Rollback() }()
	apitest.Must(tx.NewSelect().TableExpr("users").Column("id").Where("id = ?", held.UserID).For("UPDATE").Exec(ctx))(t)
	apitest.Must(factory.UserSuspensions.State(factory.SuspensionOf(held.UserID)).CreateOne(ctx, tx, f))(t)
	answer := make(chan *httptest.ResponseRecorder)
	go func() { answer <- invite(held, heldWorkspace, "held@example.test") }()
	time.Sleep(200 * time.Millisecond)
	apitest.Must(0, tx.Commit())(t)
	if w := <-answer; w.Code != 401 || !strings.Contains(w.Body.String(), "Your session has expired.") {
		t.Fatalf("an invitation under a suspension being made: %d %s", w.Code, w.Body.String())
	}

	// The real thing, at the same moment, a few times: whichever wins, no
	// invitation is left.
	for i := range 5 {
		target := apitest.SignedIn(t, db, f)
		workspace := apitest.OwnedWorkspace(t, db, f, target.UserID)
		inviters = append(inviters, target.UserID)
		path := "/api/admin/users/" + apitest.PublicIDOf(t, db, target.UserID) + "/suspension"
		var invited, suspended *httptest.ResponseRecorder
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			invited = invite(target, workspace, fmt.Sprintf("raced-%d@example.test", i))
		}()
		go func() {
			defer wg.Done()
			suspended = adminSend(h, "POST", path, reasonBody(reason), factory.AdminRouteKey, session.CSRFToken, session.Cookie())
		}()
		wg.Wait()
		if suspended.Code != 201 || (invited.Code != 201 && invited.Code != 401) {
			t.Fatalf("round %d: suspend %d %s, invite %d %s", i, suspended.Code, suspended.Body.String(), invited.Code, invited.Body.String())
		}
	}
	left := apitest.Must(db.NewSelect().TableExpr("workspace_invitations").Where("invited_by IN (?)", bun.In(inviters)).Count(ctx))(t)
	deliver()
	if left != 0 || len(box.Sent) != 0 {
		t.Fatalf("invitations left %d, emails sent %d", left, len(box.Sent))
	}
}

// A suspended account learns it's suspended only from its right password
// (and, with MFA on, its right code): a wrong password answers exactly as
// for any account, so a suspension can't be probed. The honest answer is
// recorded as auth.login_attempted with result suspended, and no session
// or cookie is made.
func TestSuspendedSignIn(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	plain := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	withMFA := apitest.WithMFA(t, db, f)
	for _, id := range []int64{plain.ID, withMFA.User.ID} {
		apitest.Must(factory.UserSuspensions.State(factory.SuspensionOf(id)).CreateOne(ctx, db, f))(t)
	}
	wrong := func(email string) *httptest.ResponseRecorder {
		return apitest.Call(t, h, "POST", "/api/auth/login", fmt.Sprintf(`{"email":%q,"password":"not the password at all"}`, email), "", nil, 401)
	}
	// A wrong password: the same answer as an account that isn't suspended,
	// and as no account at all.
	notSuspended := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	for _, email := range []string{plain.Email, withMFA.User.Email} {
		if got, want := wrong(email).Body.String(), wrong(notSuspended.Email).Body.String(); got != want {
			t.Fatalf("a wrong password for a suspended account: %q, want %q", got, want)
		}
	}
	if got := wrong("nobody@example.com").Body.String(); got != wrong(plain.Email).Body.String() {
		t.Fatalf("unknown and suspended differ: %q", got)
	}
	// The right password, MFA off: the honest 403, no session.
	w := apitest.Call(t, h, "POST", "/api/auth/login", apitest.LoginBody(plain.Email), "", nil, 403)
	if strings.TrimSpace(w.Body.String()) != suspendedAnswer || apitest.CookieNamed(w, auth.CookieName(false)) != nil {
		t.Fatalf("the right password: %s %v", w.Body.String(), w.Result().Cookies())
	}
	// MFA on: the password step looks as for anyone (a code is asked for);
	// only the right code gets the honest answer.
	challenge := apitest.StartMFALogin(t, h, withMFA.User.Email)
	if w := apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody("000000"), "", challenge, 422); strings.Contains(w.Body.String(), "suspended") {
		t.Fatalf("a wrong code told: %s", w.Body.String())
	}
	w = apitest.Call(t, h, "POST", "/api/auth/login/mfa", apitest.CodeBody(withMFA.CodeAt(0)), "", challenge, 403)
	if strings.TrimSpace(w.Body.String()) != suspendedAnswer || apitest.CookieNamed(w, auth.CookieName(false)) != nil {
		t.Fatalf("the right code: %s", w.Body.String())
	}
	if n := apitest.CountRows(t, db, "sessions", plain.ID) + apitest.CountRows(t, db, "sessions", withMFA.User.ID); n != 0 {
		t.Fatalf("sessions made: %d", n)
	}
	results := map[string]int{}
	for _, e := range apitest.EventsNamed(t, db, "auth.login_attempted") {
		results[fmt.Sprint(e.Payload["result"])]++
	}
	if results["suspended"] != 2 || results["succeeded"] != 0 {
		t.Fatalf("login results: %v", results)
	}
	if len(apitest.EventsNamed(t, db, "auth.logged_in")) != 0 {
		t.Fatal("a suspended account was recorded as signed in")
	}
}

// A suspended account is never an admin, and every admin check refuses
// one anyway: a stray admin session of it (made by hand, past the store's
// checks) is refused, granting admin to it is refused, and the door
// answers it as an account that isn't an admin.
func TestSuspendedAccountsAreNeverAdmins(t *testing.T) {
	s, db := server(t)
	h := router.New(s)
	ctx := context.Background()
	f := factory.TestFaker(t)
	admin, session := adminSignedIn(t, db, f)
	adminCall(t, h, "GET", "/api/admin/me", "", "", 200, session.Cookie())
	apitest.Must(factory.UserSuspensions.State(factory.SuspensionOf(admin.User.ID)).CreateOne(ctx, db, f))(t)
	adminCall(t, h, "GET", "/api/admin/me", "", "", 401, session.Cookie())
	// The door: the password step can't tell, the code step refuses.
	challenge := apitest.CookieNamed(adminLogin(t, h, admin.User.Email, 200), auth.AdminMFACookieName(false))
	if w := adminCall(t, h, "POST", "/api/admin/login/mfa", apitest.CodeBody(admin.CodeAt(0)), "", 401, challenge); apitest.CookieNamed(w, auth.AdminCookieName(false)) != nil {
		t.Fatal("an admin session for a suspended account")
	}
	plain := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
	apitest.Must(factory.UserSuspensions.State(factory.SuspensionOf(plain.ID)).CreateOne(ctx, db, f))(t)
	if _, _, err := s.Repo.GrantAdmin(ctx, plain.Email, auth.AdminViewer); err != store.ErrSuspended {
		t.Fatalf("granting a suspended account: %v", err)
	}
}
