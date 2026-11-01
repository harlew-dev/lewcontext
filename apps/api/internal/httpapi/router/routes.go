package router

import (
	"errors"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/endpoints/account"
	"lewcontext/api/internal/httpapi/endpoints/admin"
	"lewcontext/api/internal/httpapi/endpoints/workspace"
	"lewcontext/api/internal/observe"
	"net/http"
	"strings"
)

// New is the single registry of API endpoints and their authorization
// requirements and rate limits, and of the web app's pages when Pages is set.
// Every route behind RequireSession has a budget (limits.go); routes that
// create rows also name their own limit with s.Limit. The client's address
// and the request's ID are worked out first (ClientAddress), once for both,
// which also logs each request's access line; a panic in either becomes a
// 500 (RecoverPanics).
func New(s *httpapi.Server) http.Handler {
	api := s.Security(httpapi.RecoverPanics(unknownRoutesAsJSON(apiRoutes(s))))
	pages := httpapi.RecoverPanics(pageRoutes(s))
	return s.ClientAddress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		pages.ServeHTTP(w, r)
	}))
}

// apiRoutes are the JSON endpoints under /api.
func apiRoutes(s *httpapi.Server) *http.ServeMux {
	mux := http.NewServeMux()
	// Readiness (the database answers, and the server isn't shutting down)
	// and liveness (the process answers), for load balancers and
	// orchestrators; both log at debug.
	mux.HandleFunc("GET /api/health", health(s))
	mux.HandleFunc("GET /api/health/live", live)
	policies := account.Policies{Server: s}
	mux.HandleFunc("GET /api/policies", policies.Show)
	registrations := account.Registrations{Server: s}
	sessions := account.Sessions{Server: s}
	me := account.Me{Server: s}
	password := account.Password{Server: s}
	passwordResets := account.PasswordResets{Server: s}
	// Sign-up, sign-in and asking for a reset link may be asked to solve a
	// CAPTCHA above a limit's challenge level (Challengeable, outside the
	// counting); no other route is, least of all one on a page with a token
	// in its URL.
	mux.Handle("POST /api/auth/register", s.Challengeable(httpapi.ChallengeRegister, s.LimitAuth(http.HandlerFunc(registrations.Register))))
	mux.Handle("POST /api/auth/login", s.Challengeable(httpapi.ChallengeLogin, s.LimitAuth(http.HandlerFunc(sessions.Login))))
	// A login's second factor, with the challenge in its cookie (mfa.go).
	mux.HandleFunc("POST /api/auth/login/mfa", sessions.LoginMFA)
	// An account that must verify its email first (mustVerify) may only read
	// its session, sign out, sign out other sessions and verify: every
	// other signed-in route refuses it.
	mux.Handle("GET /api/auth/me", httpapi.AllowUnverified(s.RequireSession(http.HandlerFunc(me.Show))))
	mux.Handle("PATCH /api/auth/me", s.RequireSession(http.HandlerFunc(me.Update)))
	// Deleting the account checks the password, like the routes below.
	mux.Handle("DELETE /api/auth/me", s.RequireSession(s.LimitAuth(http.HandlerFunc(me.Destroy))))
	// Downloading the account's data checks the password too; then
	// export:per-user, in the handler, so typos don't count against it.
	dataExport := account.DataExport{Server: s}
	mux.Handle("POST /api/auth/export", s.RequireSession(s.LimitAuth(http.HandlerFunc(dataExport.Store))))
	// The recovery routes skip the signed-in budget, so a stolen session can't
	// use it up to keep the owner from signing it out (limits.go). Signing
	// out the others has a limit of its own instead, counted after the CSRF
	// check (RequireSessionUnmetered), so no other site can spend it.
	mux.Handle("POST /api/auth/logout", httpapi.AllowUnverified(s.RequireSessionUnmetered(http.HandlerFunc(sessions.Logout))))
	mux.Handle("DELETE /api/auth/sessions", httpapi.AllowUnverified(s.RequireSessionUnmetered(s.LimitSession(httpapi.SessionsRevokePerUser, sessions.RevokeOthers))))
	// Password checks hash like sign-in, so they share its IP limit and hashing slots.
	mux.Handle("POST /api/auth/password/verify", s.RequireSessionUnmetered(s.LimitAuth(http.HandlerFunc(password.Verify))))
	mux.Handle("PATCH /api/auth/password", s.RequireSessionUnmetered(s.LimitAuth(http.HandlerFunc(password.Update))))
	// Two-factor authentication (mfa.go). Setting up checks the password; so
	// do turning it off and new recovery codes, which are recovery routes
	// too: a stolen session must not use up the budget to keep the owner
	// from taking the account back.
	mfa := account.MFA{Server: s}
	recoveryCodes := account.RecoveryCodes{Server: s}
	mux.Handle("POST /api/auth/mfa/setup", s.RequireSession(s.LimitAuth(http.HandlerFunc(mfa.Setup))))
	mux.Handle("POST /api/auth/mfa", s.RequireSession(http.HandlerFunc(mfa.Store)))
	mux.Handle("DELETE /api/auth/mfa", s.RequireSessionUnmetered(s.LimitAuth(http.HandlerFunc(mfa.Destroy))))
	mux.Handle("POST /api/auth/mfa/recovery-codes", s.RequireSessionUnmetered(s.LimitAuth(http.HandlerFunc(recoveryCodes.Store))))
	// Password reset by link, signed out (password_reset.go). Requesting a
	// link and checking one don't hash, so they take only the IP quota.
	mux.Handle("POST /api/auth/password-reset", s.Challengeable(httpapi.ChallengePasswordReset, http.HandlerFunc(passwordResets.Request)))
	mux.HandleFunc("POST /api/auth/password-reset/check", passwordResets.Check)
	mux.Handle("PATCH /api/auth/password-reset", s.LimitAuth(http.HandlerFunc(passwordResets.Reset)))
	if s.Config.Verify {
		verificationRoutes(s, mux)
	}
	// An invitation link's page checks it signed out, and accepts it signed in.
	invitations := workspace.Invitations{Server: s}
	mux.HandleFunc("POST /api/invitations/check", invitations.Check)
	mux.Handle("POST /api/invitations/accept", s.RequireSession(http.HandlerFunc(invitations.Accept)))
	// A block link's page checks it, then blocks or unblocks its address,
	// signed in or out (email_blocks.go). None hash: the IP quota only.
	emailBlocks := account.EmailBlocks{Server: s}
	mux.HandleFunc("POST /api/email-blocks/check", emailBlocks.Check)
	mux.HandleFunc("POST /api/email-blocks", emailBlocks.Store)
	mux.HandleFunc("DELETE /api/email-blocks", emailBlocks.Destroy)
	// The web app's own errors, signed in only (client_errors.go): grouped
	// with the server's, never stored per report. Outside the write budget,
	// so a page stuck in an error loop can't use it up and leave its user
	// unable to save anything: client-error:per-user (counted after the
	// CSRF check, so no other site can spend it) and client-error:global
	// (in the handler) bound it instead.
	clientErrors := account.ClientErrors{Server: s}
	mux.Handle("POST /api/client-errors", s.RequireSessionUnmetered(s.Limit(httpapi.ClientErrorPerUser, clientErrors.Store)))
	workspaces := workspace.Workspaces{Server: s}
	mux.Handle("GET /api/workspaces", s.RequireSession(http.HandlerFunc(workspaces.Index)))
	mux.Handle("POST /api/workspaces", s.RequireSession(s.Limit(httpapi.WorkspacePerUser, workspaces.Store)))

	// Everything inside a workspace requires a session and membership.
	workspaceRoutes(s, group(mux, "/api/workspaces/{workspace}", s.RequireSession, s.RequireWorkspace))
	adminRoutes(s, mux)
	return mux
}

// verificationRoutes exist only with EMAIL_VERIFICATION on
// (registration.go, email_verification.go). A sign-up's code page works
// signed out, with the registration cookie; none of them hash, so they take
// only the IP limit, and each counts its own. A signed-in account verifies
// with its session, unverified or not.
func verificationRoutes(s *httpapi.Server, mux *http.ServeMux) {
	registrations := account.Registrations{Server: s}
	mux.HandleFunc("POST /api/auth/register/check", registrations.Check)
	// A new code is counted like a sign-up (register:global), so it can be
	// challenged like one; its page holds a cookie, not a token in the URL.
	mux.Handle("POST /api/auth/register/resend", s.Challengeable(httpapi.ChallengeRegister, http.HandlerFunc(registrations.Resend)))
	mux.HandleFunc("POST /api/auth/register/verify", registrations.Verify)
	verification := account.EmailVerification{Server: s}
	mux.Handle("POST /api/auth/email-verification", httpapi.AllowUnverified(s.RequireSession(http.HandlerFunc(verification.Store))))
	mux.Handle("PATCH /api/auth/email-verification", httpapi.AllowUnverified(s.RequireSession(http.HandlerFunc(verification.Update))))
}

// adminRoutes are the admin dashboard's (admin.go), and the only place an
// /api/admin/ route may be registered (TestAdminRoutesStayInTheAdminGroup).
// Every one checks X-Admin-Key (and ADMIN_ALLOWED_IPS) first, so without the
// key they answer as an unknown route; then the sign-in steps, under the
// current key only, or an admin session, under the key it was made with
// (the current one, or ADMIN_ROUTE_KEY_PREVIOUS while it is set). Apart
// from signing in and out, stepping up and the actions they read, and
// every read is recorded. A route that changes anything names the admin
// role it needs: an account-and-access action is an Action (the operator
// role, a fresh code from RequireAdminStepUp, admin-action:per-user), a
// housekeeping one a Housekeeping (the operator role and the limit, no
// code: plans/features/admin/actions.md), and each records who did it and
// why.
func adminRoutes(s *httpapi.Server, mux *http.ServeMux) {
	sessions := admin.Sessions{Server: s}
	users := admin.Users{Server: s}
	workspaces := admin.Workspaces{Server: s}
	workspaceProjects := admin.WorkspaceProjects{Server: s}
	adminEvents := admin.Events{Server: s}
	eventExport := admin.EventExport{Server: s}
	mail := admin.Mail{Server: s}
	monitoring := admin.Monitoring{Server: s}
	ipAddresses := admin.IPAddresses{Server: s}
	appErrors := admin.Errors{Server: s}
	deliveries := admin.Deliveries{Server: s}
	admins := admin.Admins{Server: s}
	emailBlocks := admin.EmailBlocks{Server: s}
	stepUp := admin.StepUp{Server: s}
	userSessions := admin.UserSessions{Server: s}
	userMFA := admin.UserMFA{Server: s}
	userSuspension := admin.UserSuspension{Server: s}
	workspaceInvitations := admin.WorkspaceInvitations{Server: s}
	failedDeliveries := admin.FailedDeliveries{Server: s}
	ipBlocks := admin.IPBlocks{Server: s}
	adminSessions := admin.AdminSessions{Server: s}
	// Any other path or method under /api/admin is an unknown route, with
	// the right key or without it.
	mux.Handle("/api/admin/", s.RequireAdminKey(http.HandlerFunc(httpapi.NotFound)))
	door := group(mux, "/api/admin", s.RequireAdminKey)
	// The password step hashes like sign-in, so it shares its limits.
	door.Handle("POST", "/login", s.LimitAuth(http.HandlerFunc(sessions.Login)))
	door.HandleFunc("POST", "/login/mfa/setup", sessions.LoginMFASetup)
	door.HandleFunc("POST", "/login/mfa", sessions.LoginMFA)
	signedIn := group(mux, "/api/admin", s.RequireAdminKeyOrPrevious, s.RequireAdminSession)
	signedIn.requireAdminRoles = true
	signedIn.server = s
	signedIn.HandleFunc("GET", "/me", sessions.Me)
	// Signing out, and signing out your other admin sessions, only take
	// your own access away: every admin may, whatever their role.
	signedIn.OwnSessions("POST", "/logout", sessions.Logout)
	signedIn.OwnSessions("DELETE", "/sessions", sessions.RevokeOthers)
	signedIn.HandleFunc("GET", "/users", users.Index)
	signedIn.HandleFunc("GET", "/users/{user}", users.Show)
	signedIn.HandleFunc("GET", "/workspaces", workspaces.Index)
	signedIn.HandleFunc("GET", "/workspaces/{workspace}", workspaces.Show)
	signedIn.HandleFunc("GET", "/workspaces/{workspace}/projects", workspaceProjects.Index)
	signedIn.HandleFunc("GET", "/events", adminEvents.Index)
	signedIn.HandleFunc("GET", "/events/{event}", adminEvents.Show)
	// The list as CSV, at most 10,000 rows, recorded with its filters
	// (admin.event.exported); bounded like the Events charts.
	signedIn.HandleFunc("GET", "/events/export", eventExport.Show)
	// The filters' names: no user data, so the one read not recorded.
	signedIn.HandleFunc("GET", "/catalogue", adminEvents.Catalogue)
	signedIn.HandleFunc("GET", "/mail", mail.Show)
	signedIn.HandleFunc("GET", "/deliveries", deliveries.Index)
	// A Monitoring page, read whole and recorded once; bounded like the
	// Events charts (httpapi.Summaries).
	signedIn.HandleFunc("GET", "/monitoring/{page}", monitoring.Show)
	// One address's page, from the IP addresses page: bounded the same
	// way, and recorded with the address looked at.
	signedIn.HandleFunc("GET", "/ips/{ip}", ipAddresses.Show)
	// One grouped error's page, from the Errors page: bounded the same way,
	// and recorded with the group looked at.
	signedIn.HandleFunc("GET", "/errors/{error}", appErrors.Show)
	signedIn.HandleFunc("GET", "/admins", admins.Index)
	signedIn.HandleFunc("GET", "/email-blocks", emailBlocks.Index)
	signedIn.HandleFunc("GET", "/ip-blocks", ipBlocks.Index)
	// A fresh code opens the session's 5-minute step-up window. Only the
	// actions need it, and they need an operator, so it does too; the code
	// counts against mfa:per-account, as at the door.
	signedIn.Role("POST", "/step-up", auth.AdminOperator, stepUp.Store)
	// The account-and-access actions (Action: the operator role, the
	// step-up window, then admin-action:per-user; each needs a reason).
	// Unblocking an address.
	signedIn.Action("DELETE", "/email-blocks", emailBlocks.Destroy)
	// On an account that isn't an admin, nor yours: Sign out everywhere,
	// resetting its MFA, suspending and unsuspending it.
	signedIn.Action("DELETE", "/users/{user}/sessions", userSessions.Destroy)
	signedIn.Action("DELETE", "/users/{user}/mfa", userMFA.Destroy)
	signedIn.Action("POST", "/users/{user}/suspension", userSuspension.Store)
	signedIn.Action("DELETE", "/users/{user}/suspension", userSuspension.Destroy)
	// Revoking a workspace's live invitations.
	signedIn.Action("DELETE", "/workspaces/{workspace}/invitations", workspaceInvitations.Destroy)
	// Queuing failed deliveries again: one, or a listener's since a time.
	signedIn.Action("POST", "/deliveries/{event}/{listener}/retry", deliveries.Retry)
	signedIn.Action("POST", "/deliveries/retry", failedDeliveries.Retry)
	// Blocking a client address or range, and lifting a block.
	signedIn.Action("POST", "/ip-blocks", ipBlocks.Store)
	signedIn.Action("DELETE", "/ip-blocks/{block}", ipBlocks.Destroy)
	// Signing another admin out: the one action on another admin.
	signedIn.Action("DELETE", "/admins/{user}/sessions", adminSessions.Destroy)
	// Housekeeping (Housekeeping: the operator role and
	// admin-action:per-user, no code) arrives with the plans whose pages
	// it acts on: acknowledging and silencing an alert with alerts (A5),
	// resolving an error group with monitoring (A4). Silencing hides
	// alerts, so it will be an Action.
}

// unknownRoutesAsJSON answers a request no API route matches with the JSON
// 404 (NotFound) instead of ServeMux's plain text, so an unknown route and an
// admin route without its key look the same. A known path with the wrong
// method keeps its 405.
//
// It also names the request's route for its access line: the pattern that
// matched ("GET /api/workspaces/{workspace}/projects"), never the path.
// A CONNECT request is the one whose "pattern" can be a path it sent (a
// redirect's), so it stays unmatched.
func unknownRoutesAsJSON(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			if r.Method != http.MethodConnect {
				observe.SetRoute(r.Context(), pattern)
			}
			mux.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(&jsonNotFound{ResponseWriter: w, r: r}, r)
	})
}

// jsonNotFound replaces ServeMux's plain-text 404 with NotFound's JSON.
type jsonNotFound struct {
	http.ResponseWriter
	r        *http.Request
	replaced bool
}

func (w *jsonNotFound) WriteHeader(status int) {
	if status != http.StatusNotFound {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	w.replaced = true
	httpapi.NotFound(w.ResponseWriter, w.r)
}
func (w *jsonNotFound) Write(body []byte) (int, error) {
	if w.replaced {
		return len(body), nil
	}
	return w.ResponseWriter.Write(body)
}

// workspaceRoutes are the routes inside a workspace. Reading needs only
// membership; every mutation is registered with Can and names the permission
// it needs (auth/permissions.go), or the server won't start.
func workspaceRoutes(s *httpapi.Server, ws routeGroup) {
	ws.requirePermissions = true
	workspaces := workspace.Workspaces{Server: s}
	projects := workspace.Projects{Server: s}
	members := workspace.Members{Server: s}
	membership := workspace.Membership{Server: s}
	invitations := workspace.Invitations{Server: s}
	ws.HandleFunc("GET", "", workspaces.Show)
	ws.Can("PATCH", "", auth.WorkspaceUpdate, workspaces.Update)
	// Deleting checks the owner's password, so it shares sign-in's limits.
	ws.Can("DELETE", "", auth.WorkspaceDestroy, s.LimitAuth(http.HandlerFunc(workspaces.Destroy)).ServeHTTP)
	ws.HandleFunc("GET", "/projects", projects.Index)
	ws.Can("POST", "/projects", auth.ProjectStore, s.Limit(httpapi.ProjectPerUser, projects.Store))
	ws.HandleFunc("GET", "/projects/{project}", projects.Show)
	ws.Can("PATCH", "/projects/{project}", auth.ProjectUpdate, projects.Update)
	ws.Can("DELETE", "/projects/{project}", auth.ProjectDestroy, projects.Destroy)
	ws.Can("GET", "/members", auth.WorkspaceMemberIndex, members.Index)
	ws.Can("PATCH", "/members/{member}", auth.WorkspaceMemberUpdate, members.Update)
	ws.Can("DELETE", "/members/{member}", auth.WorkspaceMemberDestroy, members.Destroy)
	// Invitations email an address the inviter chooses, so sending has its
	// own limit (and invitation:global in the handler).
	ws.Can("GET", "/invitations", auth.WorkspaceInvitationIndex, invitations.Index)
	ws.Can("POST", "/invitations", auth.WorkspaceInvitationStore, s.ChallengeableFunc(httpapi.ChallengeInvitation, s.Limit(httpapi.InvitationPerUser, invitations.Store)))
	ws.Can("DELETE", "/invitations/{invitation}", auth.WorkspaceInvitationDestroy, invitations.Destroy)
	// Your own membership: anyone may leave, apart from the last owner.
	ws.Can("DELETE", "/membership", auth.AnyMember, membership.Destroy)
}

// pageRoutes serve the built web app (internal/web) for every other GET.
// Signed-in pages (/app/*) need a session before any HTML is sent; the app
// itself decides which page a URL is (apps/web/src/routes.ts).
func pageRoutes(s *httpapi.Server) http.Handler {
	mux := http.NewServeMux()
	if s.Pages == nil {
		return mux // API only: in development Vite serves the pages
	}
	// Each names its route for the access line with a fixed label, never
	// the path, which holds the admin key or a link's token (pageRoute);
	// s.Pages names what it serves itself ("asset", "page 404", …).
	mux.Handle("GET /", s.Pages)
	mux.Handle("GET /app", pageRoute("page /app/*", s.RequirePageSession(s.Pages)))
	mux.Handle("GET /app/", pageRoute("page /app/*", s.RequirePageSession(s.Pages)))
	// The admin dashboard, only at its secret key; anything else under
	// /admin is an unknown page (admin.go).
	mux.Handle("GET /admin/{key}", pageRoute("page /admin/*", s.AdminPage(false)))
	mux.Handle("GET /admin/{key}/{page...}", pageRoute("page /admin/*", s.AdminPage(true)))
	// The admin build's files, only with the assets pass the admin pages set;
	// without one, an unknown page. The bare path is one too: without it the
	// mux would redirect it to /admin-assets/, showing the route exists.
	mux.Handle("GET /admin-assets/{file...}", pageRoute("admin asset", http.HandlerFunc(s.AdminAssets)))
	mux.Handle("GET /admin-assets", s.Pages)
	return mux
}

// pageRoute names a page route's access-line label before h runs.
func pageRoute(label string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observe.SetRoute(r.Context(), label)
		h.ServeHTTP(w, r)
	})
}

// health is readiness: 200 while the API can reach its database, 503 when it
// can't or once graceful shutdown has begun (Server.Drain), so a load
// balancer stops sending requests to a server that is going away.
func health(s *httpapi.Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch err := s.Ready(r); {
		case errors.Is(err, httpapi.ErrDraining):
			httpapi.Fail(w, 503, "Shutting down")
			return
		case err != nil:
			httpapi.Fail(w, 503, "Database unavailable")
			return
		}
		httpapi.JSON(w, 200, map[string]string{"status": "ok"})
	}
}

// live is liveness: 200 while the process answers, without the database,
// so an orchestrator doesn't restart every server during a database outage.
func live(w http.ResponseWriter, r *http.Request) {
	httpapi.JSON(w, 200, map[string]string{"status": "ok"})
}

// routeGroup registers routes under a shared path prefix, wrapped in shared
// middleware. net/http has no groups, and http.StripPrefix can't strip a
// prefix with a {wildcard}, so the group prefixes each pattern instead.
type routeGroup struct {
	mux        *http.ServeMux
	prefix     string
	middleware []func(http.Handler) http.Handler
	// requirePermissions makes Handle refuse anything but GET and HEAD, so
	// each mutation has to be registered with Can.
	requirePermissions bool
	// requireAdminRoles does the same for the admin dashboard: each
	// mutation is registered with Role, Action, Housekeeping or
	// OwnSessions.
	requireAdminRoles bool
	// server is the admin group's, for Action's and Housekeeping's limit.
	server *httpapi.Server
	// registered collects each pattern, when set (the permission matrix test).
	registered *[]string
}

// group starts a route group; the first middleware runs first.
func group(mux *http.ServeMux, prefix string, middleware ...func(http.Handler) http.Handler) routeGroup {
	return routeGroup{mux: mux, prefix: prefix, middleware: middleware}
}
func (g routeGroup) Handle(method, path string, h http.Handler) {
	if g.requirePermissions && method != "GET" && method != "HEAD" {
		panic(fmt.Sprintf("%s %s%s: register it with Can and the permission it needs", method, g.prefix, path))
	}
	if g.requireAdminRoles && method != "GET" && method != "HEAD" {
		panic(fmt.Sprintf("%s %s%s: register it with Role and the admin role it needs", method, g.prefix, path))
	}
	g.handle(method, path, h)
}

// Role registers an admin route only admins whose role may do role can use
// (httpapi.RequireAdminRole); others get 403. An operator's mutation must
// be an Action instead, apart from the step-up itself (operatorRoutes):
// anything else stops the server starting, so an action can't ship without
// its fresh code and limit.
func (g routeGroup) Role(method, path string, role auth.AdminRole, h http.HandlerFunc) {
	if g.requireAdminRoles && role == auth.AdminOperator && method != "GET" && method != "HEAD" && !operatorRoutes[method+" "+path] {
		panic(fmt.Sprintf("%s %s%s: register an operator's mutation with Action", method, g.prefix, path))
	}
	g.handle(method, path, httpapi.RequireAdminRole(role, h))
}

// operatorRoutes are the only operator mutations registered with Role
// rather than Action: the step-up opens the window the actions need, so it
// can't need one itself.
var operatorRoutes = map[string]bool{
	"POST /step-up": true,
}

// Action registers one of the admin dashboard's account-and-access actions:
// the operator role (httpapi.RequireAdminRole), then a fresh code
// (httpapi.RequireAdminStepUp), then admin-action:per-user (LimitAdmin),
// in that order, so a refusal before the step-up spends nothing. Every
// operator mutation but the step-up is registered this way.
func (g routeGroup) Action(method, path string, h http.HandlerFunc) {
	if !g.requireAdminRoles || g.server == nil {
		panic(fmt.Sprintf("%s %s%s: Action is for the signed-in admin group", method, g.prefix, path))
	}
	limited := g.server.LimitAdmin(httpapi.AdminActionPerUser, h)
	g.handle(method, path, httpapi.RequireAdminRole(auth.AdminOperator, httpapi.RequireAdminStepUp(limited)))
}

// housekeepingRoutes are the only admin mutations registered with
// Housekeeping: an operator's routine actions that touch no account,
// address or delivery (acknowledging an alert, resolving an error group),
// so they need no fresh code. None exists yet: alerts (A5) and monitoring
// (A4) add theirs here, each with its reason.
var housekeepingRoutes = map[string]bool{}

// Housekeeping registers one of housekeepingRoutes: the operator role
// (httpapi.RequireAdminRole), then admin-action:per-user (LimitAdmin), with
// no step-up. Anything else stops the server starting, so an
// account-and-access action can't be registered here to skip its code.
func (g routeGroup) Housekeeping(method, path string, h http.HandlerFunc) {
	if !g.requireAdminRoles || g.server == nil || !housekeepingRoutes[method+" "+path] {
		panic(fmt.Sprintf("%s %s%s isn't one of the admin's housekeeping routes: register it with Action", method, g.prefix, path))
	}
	limited := g.server.LimitAdmin(httpapi.AdminActionPerUser, h)
	g.handle(method, path, httpapi.RequireAdminRole(auth.AdminOperator, limited))
}

// ownSessionRoutes are the only admin mutations registered without a role,
// under the admin group's prefix: they act on the signed-in admin's own
// sessions, which any admin may do, since they take access away, never
// give it.
var ownSessionRoutes = map[string]bool{
	"POST /logout":     true, // signing out
	"DELETE /sessions": true, // signing out your other admin sessions
}

// OwnSessions registers one of ownSessionRoutes in the admin group;
// anything else stops the server starting, so it can't be used to skip a
// role.
func (g routeGroup) OwnSessions(method, path string, h http.HandlerFunc) {
	if !g.requireAdminRoles || !ownSessionRoutes[method+" "+path] {
		panic(fmt.Sprintf("%s %s%s isn't one of the admin's own session routes: register it with Role", method, g.prefix, path))
	}
	g.handle(method, path, h)
}

// Can registers a route only members whose role grants p may use; others get
// 403. auth.AnyMember allows every member.
func (g routeGroup) Can(method, path string, p auth.Permission, h http.HandlerFunc) {
	g.handle(method, path, httpapi.RequirePermission(p, h))
}
func (g routeGroup) handle(method, path string, h http.Handler) {
	for i := len(g.middleware) - 1; i >= 0; i-- {
		h = g.middleware[i](h)
	}
	pattern := method + " " + g.prefix + path
	if g.registered != nil {
		*g.registered = append(*g.registered, pattern)
	}
	g.mux.Handle(pattern, h)
}
func (g routeGroup) HandleFunc(method, path string, h http.HandlerFunc) {
	g.Handle(method, path, h)
}
