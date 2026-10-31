package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// Events are the admin dashboard's /api/admin/events routes:
// the audit trail, filtered and paged by a cursor. Every read is recorded
// (admin.event.listed, .viewed) with the filters used.
type Events struct{ *Server }

// Events list page sizes.
const (
	defaultEventsPerPage = 50
	maxEventsPerPage     = 100
)

// Narrowest IP ranges the filter accepts, so a range can't cover most of
// the internet.
const (
	minIPv4Prefix = 16
	minIPv6Prefix = 32
)

// catalogueNames are every event's name, in catalogue order.
func catalogueNames() []string {
	names := make([]string, len(events.Catalogue))
	for i, e := range events.Catalogue {
		names[i] = e.EventName()
	}
	return names
}

// adminReadNames are the events recording what an admin read
// (admin.<resource>.listed and .viewed), hidden from the list unless asked
// for, so the trail doesn't drown in its own reads.
func adminReadNames() []string {
	var names []string
	for _, name := range catalogueNames() {
		parts := strings.Split(name, ".")
		if len(parts) == 3 && parts[0] == "admin" && (parts[2] == "listed" || parts[2] == "viewed") {
			names = append(names, name)
		}
	}
	return names
}

// eventNames expands the name filter: a catalogue name, or a prefix ending
// in "." to every catalogue name under it. Anything else is refused, so the
// query is always name IN (…) on known names, never LIKE.
func eventNames(filter string) ([]string, bool) {
	if !strings.HasSuffix(filter, ".") {
		return []string{filter}, slices.Contains(catalogueNames(), filter)
	}
	var names []string
	for _, name := range catalogueNames() {
		if strings.HasPrefix(name, filter) {
			names = append(names, name)
		}
	}
	return names, len(names) > 0
}

// ipFilter reads an address or a CIDR range no wider than /16 (IPv4) or
// /32 (IPv6), returning it as PostgreSQL's inet reads it.
func ipFilter(raw string) (string, bool) {
	if addr, err := netip.ParseAddr(raw); err == nil {
		return addr.Unmap().WithZone("").String(), true
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return "", false
	}
	if prefix.Addr().Is4() && prefix.Bits() < minIPv4Prefix || prefix.Addr().Is6() && prefix.Bits() < minIPv6Prefix {
		return "", false
	}
	return prefix.Masked().String(), true
}

// timeFilter reads an RFC 3339 time; "" is no filter.
func timeFilter(raw string) (*time.Time, bool) {
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	return &t, err == nil
}

// adminEventQuery parses the Events list's query string into the store's
// filter, the page size and what to record of it.
func adminEventQuery(r *http.Request) (store.AdminEventFilter, int, events.EventFilters, bool) {
	var f store.AdminEventFilter
	params, ok := input.Query(r, "name", "account", "workspace", "email", "ip", "from", "to", "adminReads", "before", "perPage")
	if !ok {
		return f, 0, events.EventFilters{}, false
	}
	perPage, ok := input.Int(params["perPage"], defaultEventsPerPage, maxEventsPerPage)
	if !ok {
		return f, 0, events.EventFilters{}, false
	}
	used := events.EventFilters{
		Name: params["name"], Account: params["account"], Workspace: params["workspace"],
		Email: params["email"], IP: params["ip"], AdminReads: params["adminReads"] == "include", Before: params["before"],
	}
	f.Account, f.Workspace, f.Before = used.Account, used.Workspace, used.Before
	valid := (used.Account == "" || input.PublicID("usr", used.Account)) &&
		(used.Workspace == "" || input.PublicID("wsp", used.Workspace)) &&
		(used.Before == "" || input.PublicID("evt", used.Before)) &&
		(params["adminReads"] == "" || used.AdminReads)
	if used.Name != "" {
		f.Names, ok = eventNames(used.Name)
		valid = valid && ok
	}
	// Admin reads stay hidden unless asked for, or named exactly.
	if !used.AdminReads && !(len(f.Names) == 1 && f.Names[0] == used.Name) {
		f.Hide = adminReadNames()
	}
	if used.Email != "" {
		f.Email = used.Email
		valid = valid && ValidEmail(&f.Email)
		used.Email = f.Email
	}
	if used.IP != "" {
		f.IP, ok = ipFilter(used.IP)
		valid = valid && ok
		used.IP = f.IP
	}
	f.From, ok = timeFilter(params["from"])
	valid = valid && ok
	f.To, ok = timeFilter(params["to"])
	valid = valid && ok
	used.From, used.To = f.From, f.To
	return f, perPage, used, valid
}

func (h Events) Index(w http.ResponseWriter, r *http.Request) {
	f, perPage, used, ok := adminEventQuery(r)
	if !ok {
		Fail(w, 400, "Check the filters.")
		return
	}
	page, err := h.Repo.IndexAdminEvents(r.Context(), f, perPage)
	if errors.Is(err, store.ErrNoCursor) {
		Fail(w, 400, "That page is no longer available. Start from the newest events.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	e := events.AdminEventListed{Admin: Admin(r).User.PublicID, Filters: used}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, page)
}

func (h Events) Show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("event")
	if !input.PublicID("evt", id) {
		Fail(w, 404, "Event not found.")
		return
	}
	event, err := h.Repo.FindAdminEvent(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "Event not found.")
		return
	}
	if err != nil {
		InternalError(w, err)
		return
	}
	e := events.AdminEventViewed{Admin: Admin(r).User.PublicID, Event: event.PublicID}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		InternalError(w, err)
		return
	}
	JSON(w, 200, event)
}

// adminCatalogue is what the filters offer: every event, rate limit and
// listener name. It holds no user data, so reading it is the one admin read
// not recorded.
type adminCatalogue struct {
	Events    []string `json:"events"`
	Limits    []string `json:"limits"`
	Listeners []string `json:"listeners"`
}

func (h Events) Catalogue(w http.ResponseWriter, r *http.Request) {
	limits := []string{mail.AccountExistsPerAccount.Name}
	for _, l := range RateLimits {
		limits = append(limits, l.Name)
	}
	slices.Sort(limits)
	JSON(w, 200, adminCatalogue{Events: catalogueNames(), Limits: limits, Listeners: h.listeners()})
}

// listeners names every registered listener, sorted.
func (s *Server) listeners() []string {
	names := []string{}
	if s.Repo.Listeners == nil {
		return names
	}
	for _, event := range catalogueNames() {
		names = append(names, s.Repo.Listeners.ListenersFor(event)...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}
