package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
)

// Users are the admin dashboard's /api/admin/users routes. Every
// read is recorded (admin.user.listed, .viewed), as the trail of what each
// admin looked at.
type Users struct{ *Server }

// adminFilter reads an admin list's q and sort, with sortable saying which
// sorts are allowed; an empty sort is the default.
func adminFilter(params map[string]string, fallback string, sortable func(string) bool) (store.AdminFilter, bool) {
	f := store.AdminFilter{Search: strings.TrimSpace(params["q"]), Sort: params["sort"]}
	if f.Sort == "" {
		f.Sort = fallback
	}
	return f, input.Search(f.Search) && sortable(f.Sort)
}

// adminListQuery parses an admin list's query string, answering 400 when it
// is malformed.
func adminListQuery(w http.ResponseWriter, r *http.Request, fallback string, sortable func(string) bool) (store.AdminFilter, store.PageRequest, bool) {
	params, ok := input.Query(r, "page", "perPage", "q", "sort")
	if !ok {
		Fail(w, 400, "Invalid query parameters.")
		return store.AdminFilter{}, store.PageRequest{}, false
	}
	req, ok := input.Page(params)
	if !ok {
		Fail(w, 400, "Invalid page.")
		return store.AdminFilter{}, req, false
	}
	f, ok := adminFilter(params, fallback, sortable)
	if !ok {
		Fail(w, 400, "Check the search and sort.")
	}
	return f, req, ok
}

func (h Users) Index(w http.ResponseWriter, r *http.Request) {
	f, req, ok := adminListQuery(w, r, store.DefaultAdminUserSort, store.AdminUserSortable)
	if !ok {
		return
	}
	page, err := h.Repo.IndexAdminUsers(r.Context(), f, req)
	if err != nil {
		InternalError(w, err)
		return
	}
	e := events.AdminUserListed{Admin: Admin(r).User.PublicID, Q: f.Search, Sort: f.Sort, Page: req.Page}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}

func (h Users) Show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("user")
	if !input.PublicID("usr", id) {
		Fail(w, 404, "User not found.")
		return
	}
	user, err := h.Repo.FindAdminUser(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "User not found.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	// No user scope: deleting the account looked at keeps the record that
	// an admin looked.
	e := events.AdminUserViewed{Admin: Admin(r).User.PublicID, User: user.PublicID}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, user)
}
