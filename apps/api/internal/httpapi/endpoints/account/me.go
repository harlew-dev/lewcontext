package account

import (
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Me is the signed-in account, /api/auth/me (router/routes.go): reading it, its
// profile and deleting it.
type Me struct{ *httpapi.Server }

func (h Me) Show(w http.ResponseWriter, r *http.Request) {
	session := httpapi.Session(r)
	httpapi.JSON(w, 200, map[string]any{"user": session.User, "csrfToken": session.CSRF, "expiresAt": session.ExpiresAt, "signedInAt": session.CreatedAt, "emailVerification": h.Config.VerificationMode()})
}

const invalidProfile = "Use a name of 1–80 characters, without invisible formatting characters."

// profileInput is what a user can change about their account. The email
// address can't change: it's how they sign in.
type profileInput struct {
	Name string `json:"name"`
}

// validProfile trims the name and requires 1–80 characters without
// invisible formatting characters (validName).
func validProfile(in *profileInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	return validName(in.Name)
}

// validName is an account's name, at sign-up and in the profile: 1–80
// characters, none of them invisible formatting (input.ValidText).
func validName(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= 1 && n <= 80 && input.ValidText(name, false)
}

func (h Me) Update(w http.ResponseWriter, r *http.Request) {
	in, ok := httpapi.DecodeValid(w, r, validProfile, invalidProfile)
	if !ok {
		return
	}
	user, err := h.Repo.UpdateProfile(r.Context(), httpapi.Session(r).User, in.Name)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, user)
}

// accountDeletion confirms deleting the signed-in account.
type accountDeletion struct {
	Password string `json:"password"`
}

// Destroy deletes the signed-in account once its password is
// confirmed, and clears the session cookie. The only owner of a workspace
// gets 409 with those workspaces, so the web app can list them.
func (h Me) Destroy(w http.ResponseWriter, r *http.Request) {
	var in accountDeletion
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !h.PasswordConfirmed(w, r, in.Password, events.PasswordStepDestroyAccount) {
		return
	}
	err := h.Repo.DeleteAccount(r.Context(), httpapi.Session(r).User)
	var lastOwner store.LastOwnerError
	if errors.As(err, &lastOwner) {
		httpapi.JSON(w, 409, map[string]any{
			"error":      "Delete the workspaces you own first, or make someone else an owner.",
			"workspaces": lastOwner.Workspaces,
		})
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	h.ClearSessionCookie(w)
	w.WriteHeader(204)
}
