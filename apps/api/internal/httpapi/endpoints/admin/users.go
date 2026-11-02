package admin

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"strings"
)

// Users are the admin dashboard's /api/admin/users routes. Every
// read is recorded (admin.user.listed, .viewed), as the trail of what each
// admin looked at.
type Users struct{ *httpapi.Server }

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
// is malformed. extra names the list's own parameters, left in params.
func adminListQuery(w http.ResponseWriter, r *http.Request, fallback string, sortable func(string) bool, extra ...string) (store.AdminFilter, store.PageRequest, bool) {
	f, req, _, ok := adminListParams(w, r, fallback, sortable, extra...)
	return f, req, ok
}

// adminListParams is adminListQuery, also returning every parameter.
func adminListParams(w http.ResponseWriter, r *http.Request, fallback string, sortable func(string) bool, extra ...string) (store.AdminFilter, store.PageRequest, map[string]string, bool) {
	params, ok := input.Query(r, append([]string{"page", "perPage", "q", "sort"}, extra...)...)
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return store.AdminFilter{}, store.PageRequest{}, nil, false
	}
	req, ok := input.Page(params)
	if !ok {
		httpapi.Fail(w, 400, "Invalid page.")
		return store.AdminFilter{}, req, nil, false
	}
	f, ok := adminFilter(params, fallback, sortable)
	if !ok {
		httpapi.Fail(w, 400, "Check the search and sort.")
	}
	return f, req, params, ok
}

// Index is one page of the accounts, with a search, a sort and
// suspended=true for only the suspended ones. GET /api/admin/users.
func (h Users) Index(w http.ResponseWriter, r *http.Request) {
	f, req, params, ok := adminListParams(w, r, store.DefaultAdminUserSort, store.AdminUserSortable, "suspended")
	if !ok {
		return
	}
	switch params["suspended"] {
	case "":
	case "true":
		f.Suspended = true
	default:
		httpapi.Fail(w, 400, "Check the filters.")
		return
	}
	page, err := h.Repo.IndexAdminUsers(r.Context(), f, req)
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	e := events.AdminUserListed{Admin: httpapi.Admin(r).User.PublicID, Q: f.Search, Sort: f.Sort, Page: req.Page, Suspended: f.Suspended}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, page)
}

func (h Users) Show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("user")
	if !input.PublicID("usr", id) {
		httpapi.Fail(w, 404, "User not found.")
		return
	}
	user, err := h.Repo.FindAdminUser(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "User not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	// No user scope: deleting the account looked at keeps the record that
	// an admin looked.
	e := events.AdminUserViewed{Admin: httpapi.Admin(r).User.PublicID, User: user.PublicID}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, r, err)
		return
	}
	httpapi.JSON(w, 200, user)
}
