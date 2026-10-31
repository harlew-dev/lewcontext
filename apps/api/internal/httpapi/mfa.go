package httpapi

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Two-factor authentication (plans/features/mfa.md): an authenticator app's
// codes (TOTP) plus single-use recovery codes. Setting it up is shared with
// the admin door (admin_auth.go): beginMFASetup and confirmMFASetup take
// the user, and each entry point finds the user its own way.

const (
	wrongMFACode    = "That code didn’t work. Enter the current code from your authenticator app, or a recovery code."
	wrongSetupCode  = "That code didn’t match. Enter the current code from your authenticator app."
	mfaSignInAgain  = "Your sign-in expired. Enter your password again."
	mfaTooManyCodes = "Too many wrong codes. Enter your password again."
)

type mfaHandlers struct{ *Server }
type recoveryCodeHandlers struct{ *Server }

type mfaCode struct {
	Code string `json:"code"`
}
type mfaConfirmation struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

// setup starts setting up MFA once the password is confirmed: a new secret,
// returned once for the QR code. POST /api/auth/mfa/setup.
func (h mfaHandlers) setup(w http.ResponseWriter, r *http.Request) {
	var in passwordCheck
	if !decode(w, r, &in) {
		return
	}
	user := current(r).User
	if user.MFAEnabledAt != nil {
		fail(w, 409, "Two-factor authentication is already on.")
		return
	}
	if h.passwordConfirmed(w, r, in.Password, events.PasswordStepMFASetup) {
		h.beginMFASetup(w, r, user)
	}
}

// store turns MFA on with the first code from the authenticator, and returns
// the recovery codes, once. POST /api/auth/mfa.
func (h mfaHandlers) store(w http.ResponseWriter, r *http.Request) {
	var in mfaCode
	if !decode(w, r, &in) {
		return
	}
	user := current(r).User
	if !h.allow(w, r, mfaPerAccount, mfaPerAccount.Key(user.PublicID)) {
		return
	}
	if codes, ok := h.confirmMFASetup(w, r, user, h.sessionDigest(r), in.Code); ok {
		jsonResponse(w, 201, map[string]any{"recoveryCodes": codes})
	}
}

// destroy turns MFA off, after the password and a second factor.
// DELETE /api/auth/mfa.
func (h mfaHandlers) destroy(w http.ResponseWriter, r *http.Request) {
	var in mfaConfirmation
	if !decode(w, r, &in) {
		return
	}
	user, ok := h.mfaConfirmed(w, r, in, events.PasswordStepMFADisable)
	if !ok {
		return
	}
	factor := h.secondFactor(user, in.Code)
	used, err := h.Store.DisableMFA(r.Context(), user, factor)
	if h.answerFactor(w, r, user, factor, events.MFAStepDisable, used, err) {
		w.WriteHeader(204)
	}
}

// store replaces the recovery codes, after the password and a second factor,
// and returns the new ones, once. POST /api/auth/mfa/recovery-codes.
func (h recoveryCodeHandlers) store(w http.ResponseWriter, r *http.Request) {
	var in mfaConfirmation
	if !decode(w, r, &in) {
		return
	}
	user, ok := h.mfaConfirmed(w, r, in, events.PasswordStepRecoveryCodes)
	if !ok {
		return
	}
	codes, digests, err := h.newRecoveryCodes()
	if err != nil {
		internalError(w, err)
		return
	}
	factor := h.secondFactor(user, in.Code)
	used, err := h.Store.CreateRecoveryCodes(r.Context(), user, factor, digests)
	if h.answerFactor(w, r, user, factor, events.MFAStepRecoveryCodes, used, err) {
		jsonResponse(w, 201, map[string]any{"recoveryCodes": codes})
	}
}

// mfaConfirmed checks what turning MFA off and renewing recovery codes need
// before the code: MFA on, the password, and the account's code limit. It
// returns false once it has answered.
func (s *Server) mfaConfirmed(w http.ResponseWriter, r *http.Request, in mfaConfirmation, step string) (store.User, bool) {
	user := current(r).User
	if user.MFAEnabledAt == nil {
		fail(w, 409, "Two-factor authentication is off.")
		return user, false
	}
	if !s.passwordConfirmed(w, r, in.Password, step) {
		return user, false
	}
	return user, s.allow(w, r, mfaPerAccount, mfaPerAccount.Key(user.PublicID))
}

// answerFactor records the second-factor check a store call made and answers
// its refusal: 409 when MFA turned off meanwhile or the user is an admin
// (who can't turn it off), 422 with field "code" for a wrong code. It returns true when the factor was right.
func (s *Server) answerFactor(w http.ResponseWriter, r *http.Request, user store.User, f store.SecondFactor, step string, used store.FactorUsed, err error) bool {
	if errors.Is(err, store.ErrMFANotEnabled) {
		fail(w, 409, "Two-factor authentication is off.")
		return false
	}
	if errors.Is(err, store.ErrAdminNeedsMFA) {
		fail(w, 409, "Admins must keep two-factor authentication on.")
		return false
	}
	if err != nil && !errors.Is(err, store.ErrWrongCode) {
		internalError(w, err)
		return false
	}
	if err := s.recordMFACheck(r, user, f, step, err == nil, used); err != nil {
		internalError(w, err)
		return false
	}
	if err != nil {
		failField(w, 422, "code", wrongMFACode)
		return false
	}
	return true
}

// beginMFASetup stores a new secret for user, sealed, and answers with it
// for the QR code: the only time it leaves the server. 409 while MFA is on.
func (s *Server) beginMFASetup(w http.ResponseWriter, r *http.Request, user store.User) {
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		internalError(w, err)
		return
	}
	sealed, err := s.keys.Seal(secret, user.ID)
	if err != nil {
		internalError(w, err)
		return
	}
	err = s.Store.SetupMFA(r.Context(), user.ID, sealed)
	if errors.Is(err, store.ErrMFAEnabled) {
		fail(w, 409, "Two-factor authentication is already on.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, map[string]string{
		"secret": auth.EncodeTOTPSecret(secret),
		"uri":    auth.TOTPURI(s.Config.AppName, user.Email, secret),
	})
}

// confirmMFASetup turns MFA on for user with the pending secret's first
// code, signing out every session but sessionHash, and returns the new
// recovery codes. It answers every refusal itself and then returns false.
func (s *Server) confirmMFASetup(w http.ResponseWriter, r *http.Request, user store.User, sessionHash, code string) ([]string, bool) {
	codes, digests, err := s.newRecoveryCodes()
	if err != nil {
		internalError(w, err)
		return nil, false
	}
	factor := s.totpFactor(user, code)
	err = s.Store.EnableMFA(r.Context(), user, sessionHash, factor, digests)
	switch {
	case errors.Is(err, store.ErrMFAEnabled):
		fail(w, 409, "Two-factor authentication is already on.")
		return nil, false
	case errors.Is(err, store.ErrMFANotPending):
		fail(w, 409, "Setup expired. Start again.")
		return nil, false
	case err != nil && !errors.Is(err, store.ErrWrongCode):
		internalError(w, err)
		return nil, false
	}
	if err := s.recordMFACheck(r, user, factor, events.MFAStepEnable, err == nil, store.FactorUsed{}); err != nil {
		internalError(w, err)
		return nil, false
	}
	if err != nil {
		failField(w, 422, "code", wrongSetupCode)
		return nil, false
	}
	return codes, true
}

// loginMFA finishes a sign-in whose password was right with its second
// factor: the challenge in the MFA cookie (login) and a code. POST
// /api/auth/login/mfa.
func (s *Server) loginMFA(w http.ResponseWriter, r *http.Request) {
	var in mfaCode
	if !decode(w, r, &in) {
		return
	}
	if !s.allow(w, r, authPerIP, authPerIP.IPKey(ClientIP(r))) {
		return
	}
	hash, ok := s.mfaChallengeDigest(r)
	if !ok {
		s.endMFAChallenge(w, mfaSignInAgain)
		return
	}
	user, err := s.Store.FindMFAChallenge(r.Context(), hash, store.ChallengeLogin)
	if errors.Is(err, store.ErrNoChallenge) {
		s.endMFAChallenge(w, mfaSignInAgain)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if !s.allow(w, r, mfaPerAccount, mfaPerAccount.Key(user.PublicID)) {
		return
	}
	factor := s.secondFactor(user, in.Code)
	result, err := s.Store.AnswerMFAChallenge(r.Context(), hash, store.ChallengeLogin, user.ID, factor)
	if errors.Is(err, store.ErrNoChallenge) {
		s.endMFAChallenge(w, mfaSignInAgain)
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	if err := s.recordMFACheck(r, user, factor, events.MFAStepLogin, result.OK, result.FactorUsed); err != nil {
		internalError(w, err)
		return
	}
	switch {
	case result.Ended:
		s.endMFAChallenge(w, mfaTooManyCodes)
	case !result.OK:
		failField(w, 422, "code", wrongMFACode)
	default:
		s.clearMFACookie(w)
		s.startSession(w, r, result.User, 200, events.LoggedIn{User: result.User.PublicID})
	}
}

// startMFAChallenge answers a login whose password was right but needs a
// second factor: no session yet, a challenge in the MFA cookie instead.
func (s *Server) startMFAChallenge(w http.ResponseWriter, r *http.Request, user store.User, purpose string) {
	token, err := auth.Token()
	if err != nil {
		internalError(w, err)
		return
	}
	expires := time.Now().Add(auth.MFAChallengeLifetime)
	if err := s.Store.CreateMFAChallenge(r.Context(), user.ID, auth.Digest(token), purpose, expires); err != nil {
		internalError(w, err)
		return
	}
	http.SetCookie(w, auth.MFACookie(token, s.Config.Production))
	jsonResponse(w, 200, map[string]bool{"mfaRequired": true})
}

// mfaChallengeDigest is the stored digest of the MFA cookie's challenge
// token, when the cookie holds a well-formed one.
func (s *Server) mfaChallengeDigest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(auth.MFACookieName(s.Config.Production))
	if err != nil || !input.Token(cookie.Value) {
		return "", false
	}
	return auth.Digest(cookie.Value), true
}

// endMFAChallenge sends the browser back to the password: 401, without the
// challenge cookie.
func (s *Server) endMFAChallenge(w http.ResponseWriter, message string) {
	s.clearMFACookie(w)
	fail(w, 401, message)
}

func (s *Server) clearMFACookie(w http.ResponseWriter) {
	expired := auth.MFACookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
}

// secondFactor reads a code as typed: 6 digits (spaces allowed, as apps show
// "123 456") are an authenticator code; anything shaped like a recovery code
// is one. Anything else is an authenticator code that can't match, so it
// counts as a wrong try like any other.
func (s *Server) secondFactor(user store.User, code string) store.SecondFactor {
	if len(code) > 64 {
		return s.totpFactor(user, "")
	}
	if digits := strings.ReplaceAll(strings.TrimSpace(code), " ", ""); auth.ValidTOTPCode(digits) {
		return s.totpFactor(user, digits)
	}
	if normal, ok := auth.NormalizeRecoveryCode(code); ok {
		return store.SecondFactor{RecoveryDigest: s.keys.RecoveryCodeDigest(normal)}
	}
	return s.totpFactor(user, "")
}

// totpFactor checks code against user's sealed authenticator secret.
func (s *Server) totpFactor(user store.User, code string) store.SecondFactor {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	return store.SecondFactor{TOTP: func(sealed []byte, last int64) (int64, bool) {
		secret, err := s.keys.Open(sealed, user.ID)
		if err != nil {
			// Only a changed APP_KEY (or a damaged row) gets here.
			slog.Error("can't open a TOTP secret: was APP_KEY changed?", "user", user.PublicID)
			return 0, false
		}
		return auth.CheckTOTP(secret, code, time.Now(), last)
	}}
}

// newRecoveryCodes makes a set of recovery codes, as shown, and their digests.
func (s *Server) newRecoveryCodes() ([]string, []string, error) {
	codes, err := auth.GenerateRecoveryCodes()
	if err != nil {
		return nil, nil, err
	}
	digests := make([]string, len(codes))
	for i, code := range codes {
		normal, _ := auth.NormalizeRecoveryCode(code)
		digests[i] = s.keys.RecoveryCodeDigest(normal)
	}
	return codes, digests, nil
}

// recordMFACheck records auth.mfa.checked.
func (s *Server) recordMFACheck(r *http.Request, user store.User, f store.SecondFactor, step string, ok bool, used store.FactorUsed) error {
	e := events.MFAChecked{User: user.PublicID, OK: ok, Method: f.Method(), Step: step, RecoveryCodesLeft: used.RecoveryCodesLeft}
	return s.Store.Record(r.Context(), e, store.EventScope{UserID: user.ID})
}
