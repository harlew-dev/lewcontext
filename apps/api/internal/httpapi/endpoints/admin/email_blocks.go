package admin

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

// EmailBlocks are the admin dashboard's /api/admin/email-blocks routes
// (plans/features/implemented/admin/email-blocks.md): the block list, every
// read recorded, and Unblock, the dashboard's first write. Blocking stays
// the inbox owner's alone, from the link in an email: there is no Store.
type EmailBlocks struct{ *httpapi.Server }

// Bounds of an action's reason, in characters, after trimming.
const (
	minReason = 10
	maxReason = 500
)

// Index is one page of the block list. GET /api/admin/email-blocks.
func (h EmailBlocks) Index(w http.ResponseWriter, r *http.Request) {
	f, req, ok := adminListQuery(w, r, store.DefaultAdminEmailBlockSort, store.AdminEmailBlockSortable)
	if !ok {
		return
	}
	page, err := h.Repo.IndexAdminEmailBlocks(r.Context(), f, req)
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	e := events.AdminEmailBlockListed{Admin: httpapi.Admin(r).User.PublicID, Q: f.Search, Sort: f.Sort, Page: req.Page}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, page)
}

// unblockInput is Destroy's body: the address in the body, never the path,
// so it stays out of access logs and browser history.
type unblockInput struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// validReason trims the reason and checks it is 10 to 500 characters
// (counted as Unicode characters, as sign-up counts a password's), with no
// character that could hide or rearrange what an audit record says: line
// breaks and tabs are allowed, but nothing input.ValidText refuses (control
// characters, NUL among them, bidi overrides, zero-width spaces, line
// separators).
func validReason(reason *string) bool {
	*reason = strings.TrimSpace(*reason)
	n := utf8.RuneCountInString(*reason)
	return n >= minReason && n <= maxReason && input.ValidText(*reason, true)
}

// Destroy takes an address off the block list, behind the operator role, a
// fresh code (httpapi.RequireAdminStepUp) and admin-action:per-user
// (router/routes.go), recording admin.email.unblocked with the reason and
// email.unblocked (by: admin) with the delete. The address is normalised
// as sign-up does. 404 when it isn't blocked (its owner may have unblocked
// it meanwhile), recording nothing. DELETE /api/admin/email-blocks.
func (h EmailBlocks) Destroy(w http.ResponseWriter, r *http.Request) {
	var in unblockInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !httpapi.ValidEmail(&in.Email) {
		httpapi.FailField(w, 422, "email", "Enter a valid email address.")
		return
	}
	if !validReason(&in.Reason) {
		httpapi.FailField(w, 422, "reason", "Give a reason of 10 to 500 characters.")
		return
	}
	err := h.Repo.UnblockEmailAddress(r.Context(), in.Email, &store.AdminUnblock{Admin: httpapi.Admin(r).User, Reason: in.Reason})
	if errors.Is(err, store.ErrEmailNotBlocked) {
		httpapi.Fail(w, 404, "This address isn't blocked.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	w.WriteHeader(204)
}
