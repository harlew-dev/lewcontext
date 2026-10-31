package httpapi

// Second factors (plans/features/mfa.md): an authenticator app's codes (TOTP)
// plus single-use recovery codes. Setting MFA up is shared by the account's
// routes and the admin door: BeginMFASetup and ConfirmMFASetup take the user,
// and each entry point finds the user its own way. The emailed codes of
// sign-up and email verification are digested here too (EmailCodeDigest).

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

const (
	WrongMFACode    = "That code didn’t work. Enter the current code from your authenticator app, or a recovery code."
	wrongSetupCode  = "That code didn’t match. Enter the current code from your authenticator app."
	MFASignInAgain  = "Your sign-in expired. Enter your password again."
	MFATooManyCodes = "Too many wrong codes. Enter your password again."
)

type MFACode struct {
	Code string `json:"code"`
}

// BeginMFASetup stores a new secret for user, sealed, and answers with it
// for the QR code: the only time it leaves the server. 409 while MFA is on.
func (s *Server) BeginMFASetup(w http.ResponseWriter, r *http.Request, user store.User) {
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		InternalError(w, err)
		return
	}
	sealed, err := s.keys.Seal(secret, user.ID)
	if err != nil {
		InternalError(w, err)
		return
	}
	err = s.Repo.SetupMFA(r.Context(), user.ID, sealed)
	if errors.Is(err, store.ErrMFAEnabled) {
		Fail(w, 409, "Two-factor authentication is already on.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, map[string]string{
		"secret": auth.EncodeTOTPSecret(secret),
		"uri":    auth.TOTPURI(s.Config.AppName, user.Email, secret),
	})
}

// ConfirmMFASetup turns MFA on for user with the pending secret's first
// code, signing out every session but sessionHash, and returns the new
// recovery codes. It answers every refusal itself and then returns false.
func (s *Server) ConfirmMFASetup(w http.ResponseWriter, r *http.Request, user store.User, sessionHash, code string) ([]string, bool) {
	codes, digests, err := s.NewRecoveryCodes()
	if err != nil {
		InternalError(w, err)
		return nil, false
	}
	factor := s.totpFactor(user, code)
	err = s.Repo.EnableMFA(r.Context(), user, sessionHash, factor, digests)
	switch {
	case errors.Is(err, store.ErrMFAEnabled):
		Fail(w, 409, "Two-factor authentication is already on.")
		return nil, false
	case errors.Is(err, store.ErrMFANotPending):
		Fail(w, 409, "Setup expired. Start again.")
		return nil, false
	case err != nil && !errors.Is(err, store.ErrWrongCode):
		InternalError(w, err)
		return nil, false
	}
	if err := s.RecordMFACheck(r, user, factor, events.MFAStepEnable, err == nil, store.FactorUsed{}); err != nil {
		InternalError(w, err)
		return nil, false
	}
	if err != nil {
		FailField(w, 422, "code", wrongSetupCode)
		return nil, false
	}
	return codes, true
}

// SecondFactor reads a code as typed: 6 digits (spaces allowed, as apps show
// "123 456") are an authenticator code; anything shaped like a recovery code
// is one. Anything else is an authenticator code that can't match, so it
// counts as a wrong try like any other.
func (s *Server) SecondFactor(user store.User, code string) store.SecondFactor {
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

// NewRecoveryCodes makes a set of recovery codes, as shown, and their digests.
func (s *Server) NewRecoveryCodes() ([]string, []string, error) {
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

// RecordMFACheck records auth.mfa.checked.
func (s *Server) RecordMFACheck(r *http.Request, user store.User, f store.SecondFactor, step string, ok bool, used store.FactorUsed) error {
	e := events.MFAChecked{User: user.PublicID, OK: ok, Method: f.Method(), Step: step, RecoveryCodesLeft: used.RecoveryCodesLeft}
	return s.Repo.Record(r.Context(), e, store.EventScope{UserID: user.ID})
}

// StartMFAChallenge answers a login whose password was right but needs a
// second factor: no session yet, a challenge in the MFA cookie instead.
func (s *Server) StartMFAChallenge(w http.ResponseWriter, r *http.Request, user store.User, purpose string) {
	token, err := auth.Token()
	if err != nil {
		InternalError(w, err)
		return
	}
	expires := time.Now().Add(auth.MFAChallengeLifetime)
	if err := s.Repo.CreateMFAChallenge(r.Context(), user.ID, auth.Digest(token), purpose, expires); err != nil {
		InternalError(w, err)
		return
	}
	http.SetCookie(w, auth.MFACookie(token, s.Config.Production))
	JSON(w, 200, map[string]bool{"mfaRequired": true})
}

// MFAChallengeDigest is the stored digest of the MFA cookie's challenge
// token, when the cookie holds a well-formed one.
func (s *Server) MFAChallengeDigest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(auth.MFACookieName(s.Config.Production))
	if err != nil || !input.Token(cookie.Value) {
		return "", false
	}
	return auth.Digest(cookie.Value), true
}

// EndMFAChallenge sends the browser back to the password: 401, without the
// challenge cookie.
func (s *Server) EndMFAChallenge(w http.ResponseWriter, message string) {
	s.ClearMFACookie(w)
	Fail(w, 401, message)
}

func (s *Server) ClearMFACookie(w http.ResponseWriter) {
	expired := auth.MFACookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
}

// EmailCodeDigest is the HMAC of a code as typed. Anything that can't be a
// code still gets a digest, of a value no code has, so a malformed code is
// compared like a wrong one.
func (s *Server) EmailCodeDigest(typed string) string {
	code, ok := auth.NormalizeEmailCode(typed)
	if !ok {
		code = "not a code"
	}
	return s.keys.EmailCodeDigest(code)
}
