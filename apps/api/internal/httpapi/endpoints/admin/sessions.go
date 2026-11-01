// Package admin answers /api/admin/*, the admin dashboard (docs/api/admin.md):
// signing in at the door and the read-only views across every workspace.
// Every route is registered in the router's adminRoutes, behind
// httpapi.RequireAdminKey, and every read is recorded.
package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"net/netip"
	"time"
)

// Sessions are the admin door's sign-in and session routes, named
// actions like /api/auth/* (router/routes.go). Every sign-in needs the password and
// then a second factor; an admin without MFA sets it up here instead, only
// inside the setup window that admin grant opens.
type Sessions struct{ *httpapi.Server }

// wrongAdminLogin is the one refusal of the admin door's password step, for
// a wrong password, an unknown email, a non-admin and an admin who can't get
// in yet, so the door never confirms a password to someone who can't use it.
const wrongAdminLogin = "Email or password is incorrect."

// Login checks the password at the admin door and starts the second factor:
// a code, or setting MFA up. POST /api/admin/login.
func (h Sessions) Login(w http.ResponseWriter, r *http.Request) {
	var in httpapi.Credentials
	if !httpapi.Decode(w, r, &in) || !signInAddress(w, r) {
		return
	}
	if !httpapi.ValidCredentials(&in) {
		httpapi.Fail(w, 401, wrongAdminLogin)
		return
	}
	if !h.AccountLimit(w, r, in.Email) {
		return
	}
	user, hash, err := h.Repo.FindUser(r.Context(), in.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		httpapi.InternalError(w, err)
		return
	}
	known := err == nil
	var admin store.Admin
	adminErr := store.ErrNotAdmin
	if known {
		// Looked up before the password check, so a right password doesn't
		// take longer than a wrong one.
		admin, adminErr = h.Repo.FindAdmin(r.Context(), user.ID)
		if adminErr != nil && !errors.Is(adminErr, store.ErrNotAdmin) {
			httpapi.InternalError(w, adminErr)
			return
		}
	} else {
		hash = h.DummyHash()
	}
	valid := auth.CheckPassword(in.Password, hash)
	result := adminLoginResult(known, valid, admin, adminErr)
	if result == events.AdminLoginMFAOff {
		slog.Warn("an admin without two-factor authentication signed in outside the setup window; run admin grant again", "user", user.PublicID)
	}
	e := events.AdminLoginAttempted{Email: in.Email, Result: result}
	if known {
		e.User = user.PublicID
	}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	switch result {
	case events.AdminLoginMFARequired:
		h.startAdminChallenge(w, r, user, store.ChallengeAdmin, auth.MFAChallengeLifetime, "mfaRequired")
	case events.AdminLoginMFASetupRequired:
		h.startAdminChallenge(w, r, user, store.ChallengeAdminSetup, auth.AdminSetupLifetime, "mfaSetupRequired")
	default:
		httpapi.Fail(w, 401, wrongAdminLogin)
	}
}

// signInAddress answers 503 unless the request's client address is an IP
// address, which the sign-in's event and the admin session store (inet). A
// server reached through a unix socket has none: admin sign-in is refused
// there, with the reason logged, rather than failing on the insert. Run it
// before anything is checked or used up.
func signInAddress(w http.ResponseWriter, r *http.Request) bool {
	if _, err := netip.ParseAddr(httpapi.ClientIP(r)); err == nil {
		return true
	}
	slog.Error("admin sign-in refused: the client address isn't an IP address; serve the API over TCP (or set CLIENT_IP_HEADER behind a proxy)", "clientIP", httpapi.ClientIP(r))
	httpapi.Fail(w, 503, "Admin sign-in isn't available on this server.")
	return false
}

// adminLoginResult is what the password step at the door comes to, for
// admin.login_attempted: only an admin with the right password goes on, to
// a code (MFA on) or to setting MFA up (inside the setup window).
func adminLoginResult(known, valid bool, admin store.Admin, adminErr error) string {
	switch {
	case !known:
		return events.AdminLoginUnknownAccount
	case !valid:
		return events.AdminLoginWrongPassword
	case adminErr != nil:
		return events.AdminLoginNotAdmin
	case admin.User.MFAEnabledAt != nil:
		return events.AdminLoginMFARequired
	case admin.SetupOpen():
		return events.AdminLoginMFASetupRequired
	default:
		return events.AdminLoginMFAOff
	}
}

// startAdminChallenge answers a right password at the door: no session yet,
// a challenge with purpose in the admin door's own cookie, and {flag: true}.
func (h Sessions) startAdminChallenge(w http.ResponseWriter, r *http.Request, user store.User, purpose string, lifetime time.Duration, flag string) {
	token, err := auth.Token()
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	if err := h.Repo.CreateMFAChallenge(r.Context(), user.ID, auth.Digest(token), purpose, time.Now().Add(lifetime)); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	http.SetCookie(w, auth.AdminMFACookie(token, h.Config.Production, lifetime))
	httpapi.JSON(w, 200, map[string]bool{flag: true})
}

// LoginMFASetup starts setting MFA up for an admin who has it off, inside
// their setup window: a new secret for the QR code. POST
// /api/admin/login/mfa/setup.
func (h Sessions) LoginMFASetup(w http.ResponseWriter, r *http.Request) {
	if !httpapi.Decode(w, r, &struct{}{}) {
		return
	}
	hash, admin, purpose, ok := h.adminChallenge(w, r)
	if !ok {
		return
	}
	if purpose != store.ChallengeAdminSetup || !admin.SetupOpen() {
		h.endAdminChallenge(w, r, hash, httpapi.MFASignInAgain)
		return
	}
	h.BeginMFASetup(w, r, admin.User)
}

// LoginMFA finishes an admin sign-in: a code for an admin with MFA on, or
// the first code of the setup, which turns MFA on for the whole account and
// returns its recovery codes. Either starts the admin session. POST
// /api/admin/login/mfa.
func (h Sessions) LoginMFA(w http.ResponseWriter, r *http.Request) {
	var in httpapi.MFACode
	if !httpapi.Decode(w, r, &in) || !signInAddress(w, r) {
		return
	}
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return
	}
	hash, admin, purpose, ok := h.adminChallenge(w, r)
	if !ok {
		return
	}
	if !h.Allow(w, r, httpapi.MFAPerAccount, httpapi.MFAPerAccount.Key(admin.User.PublicID)) {
		return
	}
	if purpose == store.ChallengeAdminSetup {
		h.confirmAdminSetup(w, r, hash, admin, in.Code)
		return
	}
	factor := h.SecondFactor(admin.User, in.Code)
	result, err := h.Repo.AnswerMFAChallenge(r.Context(), hash, store.ChallengeAdmin, admin.User.ID, factor)
	if errors.Is(err, store.ErrNoChallenge) {
		h.endAdminChallenge(w, r, "", httpapi.MFASignInAgain)
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	if err := h.RecordMFACheck(r, admin.User, factor, events.MFAStepAdminLogin, result.OK, result.FactorUsed); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	switch {
	case result.Ended:
		h.endAdminChallenge(w, r, "", httpapi.MFATooManyCodes)
	case !result.OK:
		httpapi.FailField(w, 422, "code", httpapi.WrongMFACode)
	default:
		h.startAdminSession(w, r, result.User, admin.Role, 200, nil)
	}
}

// confirmAdminSetup turns MFA on with the setup's first code, while the
// window is still open, and starts the admin session with the new recovery
// codes in the answer (201).
func (h Sessions) confirmAdminSetup(w http.ResponseWriter, r *http.Request, hash string, admin store.Admin, code string) {
	if !admin.SetupOpen() {
		h.endAdminChallenge(w, r, hash, httpapi.MFASignInAgain)
		return
	}
	codes, ok := h.ConfirmMFASetup(w, r, admin.User, h.SessionDigest(r), code)
	if !ok {
		return
	}
	if err := h.Repo.DeleteMFAChallenge(r.Context(), hash); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	now := time.Now()
	admin.User.MFAEnabledAt = &now
	h.startAdminSession(w, r, admin.User, admin.Role, 201, map[string]any{"recoveryCodes": codes})
}

// adminChallenge reads the admin door's challenge cookie and returns the
// live challenge's digest, admin and purpose. It answers 401 (back to the
// password) when there is none, or its user is no longer an admin.
func (h Sessions) adminChallenge(w http.ResponseWriter, r *http.Request) (string, store.Admin, string, bool) {
	cookie, err := r.Cookie(auth.AdminMFACookieName(h.Config.Production))
	if err != nil || !input.Token(cookie.Value) {
		h.endAdminChallenge(w, r, "", httpapi.MFASignInAgain)
		return "", store.Admin{}, "", false
	}
	hash := auth.Digest(cookie.Value)
	admin, purpose, err := h.Repo.FindAdminChallenge(r.Context(), hash)
	if errors.Is(err, store.ErrNoChallenge) {
		h.endAdminChallenge(w, r, "", httpapi.MFASignInAgain)
		return "", admin, "", false
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return "", admin, "", false
	}
	return hash, admin, purpose, true
}

// endAdminChallenge sends the browser back to the password: 401, without the
// challenge cookie, and the challenge hash deleted when given.
func (h Sessions) endAdminChallenge(w http.ResponseWriter, r *http.Request, hash, message string) {
	if hash != "" {
		if err := h.Repo.DeleteMFAChallenge(r.Context(), hash); err != nil {
			httpapi.InternalError(w, err)
			return
		}
	}
	clearCookie(w, auth.AdminMFACookie("", h.Config.Production, 0))
	httpapi.Fail(w, 401, message)
}

// startAdminSession signs the admin in to the dashboard, records
// admin.logged_in, and answers with the session as /api/admin/me does, plus
// extra.
func (h Sessions) startAdminSession(w http.ResponseWriter, r *http.Request, user store.User, role auth.AdminRole, status int, extra map[string]any) {
	token, err := auth.Token()
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	csrf, err := auth.Token()
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	expires := time.Now().Add(auth.AdminSessionLifetime)
	e := events.AdminLoggedIn{User: user.PublicID}
	// Always under the current key: the door never opens to the previous
	// one (RequireAdminKey). Where it signed in from is kept for the Admins
	// page and for noticing it move.
	err = h.Repo.CreateAdminSession(r.Context(), store.NewAdminSession{
		UserID: user.ID, TokenHash: auth.Digest(token), KeyHash: h.AdminKeyHash(), CSRF: csrf, ExpiresAt: expires,
		IP: httpapi.ClientIP(r), UserAgent: events.UserAgent(r.Context()),
	}, e)
	if errors.Is(err, store.ErrNotAdmin) {
		h.endAdminChallenge(w, r, "", httpapi.MFASignInAgain)
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	clearCookie(w, auth.AdminMFACookie("", h.Config.Production, 0))
	http.SetCookie(w, auth.AdminCookie(token, h.Config.Production))
	body := map[string]any{"user": adminUser(user), "role": role, "csrfToken": csrf, "expiresAt": expires}
	for key, value := range extra {
		body[key] = value
	}
	httpapi.JSON(w, status, body)
}

// Me is the signed-in admin's session and role; the admin pages' first
// call, which answers 401 without one. GET /api/admin/me.
func (h Sessions) Me(w http.ResponseWriter, r *http.Request) {
	session := httpapi.Admin(r)
	httpapi.JSON(w, 200, map[string]any{"user": adminUser(session.User), "role": session.Role, "csrfToken": session.CSRF, "expiresAt": session.ExpiresAt})
}

// sessionUser is the signed-in admin as the admin pages see them: who they
// are, and nothing else of the account (not its policy agreement, which the
// admin queries don't load and the pages don't need).
type sessionUser struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	CreatedAt       time.Time  `json:"createdAt"`
	EmailVerifiedAt *time.Time `json:"emailVerifiedAt"`
	MFAEnabledAt    *time.Time `json:"mfaEnabledAt"`
}

func adminUser(u store.User) sessionUser {
	return sessionUser{ID: u.PublicID, Name: u.Name, Email: u.Email, CreatedAt: u.CreatedAt, EmailVerifiedAt: u.EmailVerifiedAt, MFAEnabledAt: u.MFAEnabledAt}
}

// Logout ends the admin session (the app session, if any, stays), under
// either key. POST /api/admin/logout.
func (h Sessions) Logout(w http.ResponseWriter, r *http.Request) {
	if err := h.Repo.DeleteAdminSession(r.Context(), httpapi.Admin(r).User, h.AdminSessionDigest(r)); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	clearCookie(w, auth.AdminCookie("", h.Config.Production))
	w.WriteHeader(204)
}

// RevokeOthers signs out every other admin session of the signed-in
// admin's, keeping this one, and records admin.sessions_revoked. It only
// takes access away, so it needs no role and no code (signing out another
// admin is an action). DELETE /api/admin/sessions.
func (h Sessions) RevokeOthers(w http.ResponseWriter, r *http.Request) {
	if _, err := h.Repo.DeleteOtherAdminSessions(r.Context(), httpapi.Admin(r).User, h.AdminSessionDigest(r)); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	w.WriteHeader(204)
}

// clearCookie tells the browser to drop cookie (an admin session or
// challenge cookie, built empty).
func clearCookie(w http.ResponseWriter, cookie *http.Cookie) {
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(0, 0)
	http.SetCookie(w, cookie)
}
