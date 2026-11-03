package account

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/store"
	"net/http"
)

const emailChangeEnded = "This email change has ended. Start a new request."
const emailUnavailable = "This email address cannot be used. Choose another address."

type emailChangeInput struct {
	Email           string `json:"email"`
	CurrentPassword string `json:"currentPassword"`
	Code            string `json:"code"`
}

// EmailChange is the signed-in account's session-bound singleton. It remains
// reachable at onboarding and the required-verification gate.
type EmailChange struct{ *httpapi.Server }

func (h EmailChange) Show(w http.ResponseWriter, r *http.Request) {
	pending, err := h.Repo.FindEmailChange(r.Context(), httpapi.Session(r).User.ID, h.SessionDigest(r))
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, map[string]any{"pending": pending})
}

func (h EmailChange) Store(w http.ResponseWriter, r *http.Request) {
	var in emailChangeInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.ValidEmail(&in.Email) {
		httpapi.FailField(w, 422, "email", "Enter a valid email address.")
		return
	}
	email := in.Email
	user := httpapi.Session(r).User
	if !h.Allow(w, r, httpapi.EmailChangePerUser, httpapi.EmailChangePerUser.Key(user.PublicID)) ||
		!h.Allow(w, r, httpapi.EmailChangePerEmail, httpapi.EmailChangePerEmail.Key(email)) ||
		!h.Allow(w, r, httpapi.EmailChangeGlobal, httpapi.EmailChangeGlobal.Everyone()) {
		return
	}
	if !(Password{Server: h.Server}).confirmationAllowed(w, r, user, passwordChange{CurrentPassword: in.CurrentPassword, Code: in.Code}) {
		return
	}
	proof := store.EmailChangeProof{
		ExpectedMFA: user.MFAEnabledAt != nil,
		PasswordConfirmation: store.PasswordConfirmation{
			Factor: h.SecondFactor(r.Context(), user, in.Code),
			Password: func(hash string) error {
				if !httpapi.PasswordInBounds(in.CurrentPassword) || !auth.CheckPassword(in.CurrentPassword, hash) {
					return errWrongPassword
				}
				return nil
			},
		},
		CompletionLimit: h.completionLimit(user),
	}
	result, err := h.Repo.CreateEmailChange(r.Context(), user.ID, h.SessionDigest(r), email, h.Config.Verify, proof)
	if errors.Is(err, store.ErrEmailChangeProofChanged) {
		if result.User.MFAEnabledAt != nil {
			httpapi.FailField(w, 422, "code", "Account security settings changed. Enter a fresh authenticator or recovery code.")
		} else {
			httpapi.FailField(w, 422, "currentPassword", "Account security settings changed. Enter your current password.")
		}
		return
	}
	if result.ProofChecked && (err == nil || errors.Is(err, errWrongPassword) || errors.Is(err, store.ErrWrongCode)) {
		var recorded error
		if result.Confirmed.MFA {
			recorded = h.RecordMFACheck(r, user, proof.Factor, events.PasswordStepEmailChange, !errors.Is(err, store.ErrWrongCode), result.Confirmed.FactorUsed)
		} else {
			recorded = h.RecordPasswordCheck(r, user, !errors.Is(err, errWrongPassword), events.PasswordStepEmailChange)
		}
		if recorded != nil {
			httpapi.InternalError(w, r, recorded)
			return
		}
	}
	if h.answerError(w, r, err) {
		return
	}
	if result.LimitedUntil != nil {
		httpapi.RefuseLimit(w, httpapi.EmailChangeCompletePerUser, *result.LimitedUntil)
		return
	}
	if result.Unavailable {
		httpapi.Fail(w, 409, emailUnavailable)
		return
	}
	if result.OK {
		h.StartEmailChangeSession(w, r, result.User)
		return
	}
	httpapi.JSON(w, 202, map[string]any{"pending": result.Pending})
}

func (h EmailChange) Update(w http.ResponseWriter, r *http.Request) {
	var in httpapi.MFACode
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !h.Config.Verify {
		httpapi.Fail(w, 410, emailChangeEnded)
		return
	}
	user := httpapi.Session(r).User
	// Meter the stored destination before opening the transaction. Correlation
	// is rechecked under the locks, so a resend cannot switch the metered inbox.
	pending, err := h.Repo.FindEmailChange(r.Context(), user.ID, h.SessionDigest(r))
	if h.answerError(w, r, err) {
		return
	}
	if pending == nil {
		httpapi.Fail(w, 410, emailChangeEnded)
		return
	}
	if pending.State == "sending" {
		httpapi.JSON(w, 409, map[string]any{"error": "Your code is still being sent. Try again shortly.", "pending": true})
		return
	}
	if !h.Allow(w, r, httpapi.EmailCodePerEmail, httpapi.EmailCodePerEmail.Key(pending.Email)) {
		return
	}
	result, err := h.Repo.UpdateEmailChange(r.Context(), user.ID, h.SessionDigest(r), pending.RequestID, h.EmailCodeDigest(in.Code), h.completionLimit(user))
	if h.answerError(w, r, err) {
		return
	}
	switch {
	case result.LimitedUntil != nil:
		httpapi.RefuseLimit(w, httpapi.EmailChangeCompletePerUser, *result.LimitedUntil)
	case result.Waiting:
		httpapi.JSON(w, 409, map[string]any{"error": "Your code is still being sent. Try again shortly.", "pending": true})
	case result.Unavailable:
		httpapi.Fail(w, 409, emailUnavailable)
	case result.Ended:
		httpapi.Fail(w, 410, emailChangeEnded)
	case !result.OK:
		httpapi.FailField(w, 422, "code", wrongEmailCode)
	default:
		h.StartEmailChangeSession(w, r, result.User)
	}
}

func (h EmailChange) Destroy(w http.ResponseWriter, r *http.Request) {
	err := h.Repo.DeleteEmailChange(r.Context(), httpapi.Session(r).User.ID, h.SessionDigest(r))
	if h.answerError(w, r, err) {
		return
	}
	w.WriteHeader(204)
}

func (h EmailChange) completionLimit(u store.User) store.EmailChangeCompletionLimit {
	if h.Config.RateLimits == config.RateLimitsOff {
		return store.EmailChangeCompletionLimit{}
	}
	l := httpapi.EmailChangeCompletePerUser
	return store.EmailChangeCompletionLimit{Name: l.Name, Key: l.Key(u.PublicID), Maximum: l.Block.At(), Window: l.Window}
}

func (h EmailChange) answerError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errWrongPassword):
		httpapi.FailField(w, 422, "currentPassword", httpapi.WrongCurrentPassword)
	case errors.Is(err, store.ErrWrongCode):
		httpapi.FailField(w, 422, "code", httpapi.WrongMFACode)
	case errors.Is(err, store.ErrEmailUnchanged):
		httpapi.FailField(w, 422, "email", "Choose an email address different from your current one.")
	case errors.Is(err, store.ErrNoEmailChange):
		httpapi.Fail(w, 410, emailChangeEnded)
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, store.ErrSuspended):
		httpapi.Fail(w, 401, "Sign in to continue.")
	default:
		httpapi.InternalError(w, r, err)
	}
	return true
}
