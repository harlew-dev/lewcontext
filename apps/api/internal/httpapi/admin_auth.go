package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"time"
)

// AdminSessions are the admin door's sign-in and session routes, named
// actions like /api/auth/* (router/routes.go). Every sign-in needs the password and
// then a second factor; an admin without MFA sets it up here instead, only
// inside the setup window that admin grant opens.
type AdminSessions struct{ *Server }

// wrongAdminLogin is the one refusal of the admin door's password step, for
// a wrong password, an unknown email, a non-admin and an admin who can't get
// in yet, so the door never confirms a password to someone who can't use it.
const wrongAdminLogin = "Email or password is incorrect."

// Login checks the password at the admin door and starts the second factor:
// a code, or setting MFA up. POST /api/admin/login.
func (h AdminSessions) Login(w http.ResponseWriter, r *http.Request) {
	var in Credentials
	if !Decode(w, r, &in) {
		return
	}
	if !ValidCredentials(&in) {
		Fail(w, 401, wrongAdminLogin)
		return
	}
	if !h.AccountLimit(w, r, in.Email) {
		return
	}
	user, hash, err := h.Repo.FindUser(r.Context(), in.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		InternalError(w, err)
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
			InternalError(w, adminErr)
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
		InternalError(w, err)
		return
	}
	switch result {
	case events.AdminLoginMFARequired:
		h.startAdminChallenge(w, r, user, store.ChallengeAdmin, auth.MFAChallengeLifetime, "mfaRequired")
	case events.AdminLoginMFASetupRequired:
		h.startAdminChallenge(w, r, user, store.ChallengeAdminSetup, auth.AdminSetupLifetime, "mfaSetupRequired")
	default:
		Fail(w, 401, wrongAdminLogin)
	}
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
func (h AdminSessions) startAdminChallenge(w http.ResponseWriter, r *http.Request, user store.User, purpose string, lifetime time.Duration, flag string) {
	token, err := auth.Token()
	if err != nil {
		InternalError(w, err)
		return
	}
	if err := h.Repo.CreateMFAChallenge(r.Context(), user.ID, auth.Digest(token), purpose, time.Now().Add(lifetime)); err != nil {
		InternalError(w, err)
		return
	}
	http.SetCookie(w, auth.AdminMFACookie(token, h.Config.Production, lifetime))
	JSON(w, 200, map[string]bool{flag: true})
}

// LoginMFASetup starts setting MFA up for an admin who has it off, inside
// their setup window: a new secret for the QR code. POST
// /api/admin/login/mfa/setup.
func (h AdminSessions) LoginMFASetup(w http.ResponseWriter, r *http.Request) {
	if !Decode(w, r, &struct{}{}) {
		return
	}
	hash, admin, purpose, ok := h.adminChallenge(w, r)
	if !ok {
		return
	}
	if purpose != store.ChallengeAdminSetup || !admin.SetupOpen() {
		h.endAdminChallenge(w, r, hash, MFASignInAgain)
		return
	}
	h.BeginMFASetup(w, r, admin.User)
}

// LoginMFA finishes an admin sign-in: a code for an admin with MFA on, or
// the first code of the setup, which turns MFA on for the whole account and
// returns its recovery codes. Either starts the admin session. POST
// /api/admin/login/mfa.
func (h AdminSessions) LoginMFA(w http.ResponseWriter, r *http.Request) {
	var in MFACode
	if !Decode(w, r, &in) {
		return
	}
	if !h.Allow(w, r, AuthPerIP, AuthPerIP.IPKey(ClientIP(r))) {
		return
	}
	hash, admin, purpose, ok := h.adminChallenge(w, r)
	if !ok {
		return
	}
	if !h.Allow(w, r, MFAPerAccount, MFAPerAccount.Key(admin.User.PublicID)) {
		return
	}
	if purpose == store.ChallengeAdminSetup {
		h.confirmAdminSetup(w, r, hash, admin, in.Code)
		return
	}
	factor := h.SecondFactor(admin.User, in.Code)
	result, err := h.Repo.AnswerMFAChallenge(r.Context(), hash, store.ChallengeAdmin, admin.User.ID, factor)
	if errors.Is(err, store.ErrNoChallenge) {
		h.endAdminChallenge(w, r, "", MFASignInAgain)
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	if err := h.RecordMFACheck(r, admin.User, factor, events.MFAStepAdminLogin, result.OK, result.FactorUsed); err != nil {
		InternalError(w, err)
		return
	}
	switch {
	case result.Ended:
		h.endAdminChallenge(w, r, "", MFATooManyCodes)
	case !result.OK:
		FailField(w, 422, "code", WrongMFACode)
	default:
		h.startAdminSession(w, r, result.User, 200, nil)
	}
}

// confirmAdminSetup turns MFA on with the setup's first code, while the
// window is still open, and starts the admin session with the new recovery
// codes in the answer (201).
func (h AdminSessions) confirmAdminSetup(w http.ResponseWriter, r *http.Request, hash string, admin store.Admin, code string) {
	if !admin.SetupOpen() {
		h.endAdminChallenge(w, r, hash, MFASignInAgain)
		return
	}
	codes, ok := h.ConfirmMFASetup(w, r, admin.User, h.SessionDigest(r), code)
	if !ok {
		return
	}
	if err := h.Repo.DeleteMFAChallenge(r.Context(), hash); err != nil {
		InternalError(w, err)
		return
	}
	now := time.Now()
	admin.User.MFAEnabledAt = &now
	h.startAdminSession(w, r, admin.User, 201, map[string]any{"recoveryCodes": codes})
}

// adminChallenge reads the admin door's challenge cookie and returns the
// live challenge's digest, admin and purpose. It answers 401 (back to the
// password) when there is none, or its user is no longer an admin.
func (h AdminSessions) adminChallenge(w http.ResponseWriter, r *http.Request) (string, store.Admin, string, bool) {
	cookie, err := r.Cookie(auth.AdminMFACookieName(h.Config.Production))
	if err != nil || !input.Token(cookie.Value) {
		h.endAdminChallenge(w, r, "", MFASignInAgain)
		return "", store.Admin{}, "", false
	}
	hash := auth.Digest(cookie.Value)
	admin, purpose, err := h.Repo.FindAdminChallenge(r.Context(), hash)
	if errors.Is(err, store.ErrNoChallenge) {
		h.endAdminChallenge(w, r, "", MFASignInAgain)
		return "", admin, "", false
	}
	if err != nil {
		InternalError(w, err)
		return "", admin, "", false
	}
	return hash, admin, purpose, true
}

// endAdminChallenge sends the browser back to the password: 401, without the
// challenge cookie, and the challenge hash deleted when given.
func (h AdminSessions) endAdminChallenge(w http.ResponseWriter, r *http.Request, hash, message string) {
	if hash != "" {
		if err := h.Repo.DeleteMFAChallenge(r.Context(), hash); err != nil {
			InternalError(w, err)
			return
		}
	}
	h.clearAdminCookie(w, auth.AdminMFACookie("", h.Config.Production, 0))
	Fail(w, 401, message)
}

// startAdminSession signs the admin in to the dashboard, records
// admin.logged_in, and answers with the session as /api/admin/me does, plus
// extra.
func (h AdminSessions) startAdminSession(w http.ResponseWriter, r *http.Request, user store.User, status int, extra map[string]any) {
	token, err := auth.Token()
	if err != nil {
		InternalError(w, err)
		return
	}
	csrf, err := auth.Token()
	if err != nil {
		InternalError(w, err)
		return
	}
	expires := time.Now().Add(auth.AdminSessionLifetime)
	e := events.AdminLoggedIn{User: user.PublicID}
	err = h.Repo.CreateAdminSession(r.Context(), user.ID, auth.Digest(token), h.AdminKeyHash(), csrf, expires, e)
	if errors.Is(err, store.ErrNotAdmin) {
		h.endAdminChallenge(w, r, "", MFASignInAgain)
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	h.clearAdminCookie(w, auth.AdminMFACookie("", h.Config.Production, 0))
	http.SetCookie(w, auth.AdminCookie(token, h.Config.Production))
	body := map[string]any{"user": user, "csrfToken": csrf, "expiresAt": expires}
	for key, value := range extra {
		body[key] = value
	}
	JSON(w, status, body)
}

// Me is the signed-in admin's session; the admin pages' first call, which
// answers 401 without one. GET /api/admin/me.
func (h AdminSessions) Me(w http.ResponseWriter, r *http.Request) {
	session := Admin(r)
	JSON(w, 200, map[string]any{"user": session.User, "csrfToken": session.CSRF, "expiresAt": session.ExpiresAt})
}

// Logout ends the admin session (the app session, if any, stays). POST
// /api/admin/logout.
func (h AdminSessions) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(auth.AdminCookieName(h.Config.Production))
	if err := h.Repo.DeleteAdminSession(r.Context(), Admin(r).User, auth.Digest(cookie.Value)); err != nil {
		InternalError(w, err)
		return
	}
	h.clearAdminCookie(w, auth.AdminCookie("", h.Config.Production))
	w.WriteHeader(204)
}

// clearAdminCookie tells the browser to drop cookie (an admin session or
// challenge cookie, built empty).
func (h AdminSessions) clearAdminCookie(w http.ResponseWriter, cookie *http.Cookie) {
	cookie.MaxAge = -1
	cookie.Expires = time.Unix(0, 0)
	http.SetCookie(w, cookie)
}
