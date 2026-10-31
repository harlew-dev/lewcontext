package account

import (
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"net/http"
)

type RecoveryCodes struct{ *httpapi.Server }

// Store replaces the recovery codes, after the password and a second factor,
// and returns the new ones, once. POST /api/auth/mfa/recovery-codes.
func (h RecoveryCodes) Store(w http.ResponseWriter, r *http.Request) {
	var in mfaConfirmation
	if !httpapi.Decode(w, r, &in) {
		return
	}
	user, ok := mfaConfirmed(h.Server, w, r, in, events.PasswordStepRecoveryCodes)
	if !ok {
		return
	}
	codes, digests, err := h.NewRecoveryCodes()
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	factor := h.SecondFactor(user, in.Code)
	used, err := h.Repo.CreateRecoveryCodes(r.Context(), user, factor, digests)
	if answerFactor(h.Server, w, r, user, factor, events.MFAStepRecoveryCodes, used, err) {
		httpapi.JSON(w, 201, map[string]any{"recoveryCodes": codes})
	}
}
