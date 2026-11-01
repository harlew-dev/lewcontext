package admin

import (
	"context"
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// Events are the admin dashboard's /api/admin/events routes: the audit
// trail, filtered and paged by a cursor, with the charts' summary on the
// newest page when asked for. Every read is recorded (admin.event.listed,
// .viewed) with the filters used, once per request, summary included.
type Events struct{ *httpapi.Server }

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

// The name filter's bounds: the catalogue has 60 names, so a canonical
// list of tokens is far shorter than either.
const (
	maxNameTokens = 64
	maxNameParam  = 4096
)

// eventNames expands the name filter: comma-separated tokens, each a
// catalogue name or a group ID (auth.*, auth.mfa.*, admin-reads), to the
// union of their names in catalogue order. Anything else is refused (an
// unknown, empty or repeated token, too many, too long), so the query is
// always name IN (…) on known names, never LIKE.
func eventNames(param string) (names, tokens []string, ok bool) {
	if len(param) > maxNameParam {
		return nil, nil, false
	}
	tokens = strings.Split(param, ",")
	if len(tokens) > maxNameTokens {
		return nil, nil, false
	}
	groups := events.GroupNames()
	catalogue := events.Names()
	picked := map[string]bool{}
	for i, token := range tokens {
		if token == "" || slices.Contains(tokens[:i], token) {
			return nil, nil, false
		}
		switch {
		case slices.Contains(catalogue, token):
			picked[token] = true
		case groups[token] != nil:
			for _, name := range groups[token] {
				picked[name] = true
			}
		default:
			return nil, nil, false
		}
	}
	for _, name := range catalogue {
		if picked[name] {
			names = append(names, name)
		}
	}
	return names, tokens, true
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

// eventQuery is an Events request, parsed: the store's filter, the window,
// the page size, whether the summary was asked for, and what to record.
type eventQuery struct {
	filter  store.AdminEventFilter
	window  eventWindow
	perPage int
	summary bool
	used    events.EventFilters
}

// adminEventQuery parses the Events list's query string at now. Anything
// it doesn't accept is false (400).
func adminEventQuery(r *http.Request, now time.Time) (eventQuery, bool) {
	var q eventQuery
	params, ok := input.Query(r, "name", "outcome", "account", "workspace", "email", "ip", "window", "from", "to", "summary", "before", "perPage")
	if !ok {
		return q, false
	}
	q.perPage, ok = input.Int(params["perPage"], defaultEventsPerPage, maxEventsPerPage)
	if !ok {
		return q, false
	}
	f := &q.filter
	used := events.EventFilters{
		Outcome: params["outcome"], Account: params["account"], Workspace: params["workspace"],
		Email: params["email"], IP: params["ip"], Before: params["before"],
	}
	f.Account, f.Workspace, f.Before, f.Failed = used.Account, used.Workspace, used.Before, used.Outcome == outcomeFailed
	q.summary = params["summary"] == summaryInclude
	used.Summary = q.summary
	valid := (used.Account == "" || input.PublicID("usr", used.Account)) &&
		(used.Workspace == "" || input.PublicID("wsp", used.Workspace)) &&
		(used.Before == "" || input.PublicID("evt", used.Before)) &&
		(used.Outcome == "" || f.Failed) &&
		(params["summary"] == "" || q.summary) &&
		// The summary comes with the newest page only.
		!(q.summary && used.Before != "")
	if params["name"] != "" {
		f.Names, used.Names, ok = eventNames(params["name"])
		valid = valid && ok
	} else {
		// Nothing picked: every event but the admin reads.
		f.Hide = events.AdminReadNames()
	}
	if used.Email != "" {
		f.Email = used.Email
		valid = valid && httpapi.ValidEmail(&f.Email)
		used.Email = f.Email
	}
	if used.IP != "" {
		f.IP, ok = ipFilter(used.IP)
		valid = valid && ok
		used.IP = f.IP
	}
	q.window, ok = windowQuery(params, now)
	valid = valid && ok
	f.From, f.To = q.window.From, q.window.listTo()
	used.Window, used.From, used.To = q.window.Name, q.window.From, q.window.To
	q.used = used
	return q, valid
}

// windowQuery reads window, or from and to (never both), resolving it at
// now. No window is the last 24 hours.
func windowQuery(params map[string]string, now time.Time) (eventWindow, bool) {
	name, rawFrom, rawTo := params["window"], params["from"], params["to"]
	if name != "" && (rawFrom != "" || rawTo != "") {
		return eventWindow{}, false
	}
	if rawFrom == "" && rawTo == "" {
		if name == "" {
			name = defaultEventWindow
		}
		return resolveWindow(now, name)
	}
	from, okFrom := timeFilter(rawFrom)
	to, okTo := timeFilter(rawTo)
	if !okFrom || !okTo || from != nil && to != nil && !from.Before(*to) {
		return eventWindow{}, false
	}
	return eventWindow{From: utc(from), To: utc(to)}, true
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (h Events) Index(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	q, ok := adminEventQuery(r, now)
	if !ok {
		httpapi.Fail(w, 400, "Check the filters.")
		return
	}
	page, err := h.Repo.IndexAdminEvents(r.Context(), q.filter, q.perPage)
	if errors.Is(err, store.ErrNoCursor) {
		httpapi.Fail(w, 400, "That page is no longer available. Start from the newest events.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	out := adminEventIndex{AdminEventPage: page, Window: q.window.echo(now)}
	if out.Refs, err = h.Repo.FindAdminEventRefs(r.Context(), q.filter.Account, q.filter.Workspace); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	if q.summary {
		summary, err := h.summarize(r.Context(), q, now)
		if err != nil {
			httpapi.InternalError(w, err)
			return
		}
		out.Summary = &summary
	}
	// One read recorded for the list and its charts together.
	e := events.AdminEventListed{Admin: httpapi.Admin(r).User.PublicID, Filters: q.used}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, out)
}

// adminEventIndex is an Events page: its rows and cursor, the window as
// resolved (null for all time), the names behind the account and
// workspace filters, and, when asked for, the summary.
type adminEventIndex struct {
	store.AdminEventPage
	Window  *windowEcho          `json:"window"`
	Refs    store.AdminEventRefs `json:"refs"`
	Summary *store.AdminSummary  `json:"summary,omitempty"`
}

// summarize computes the summary after the list, within its bounds: 7 days
// or less, a slot among the server's two, and a deadline. Past a bound it
// says why (a status) rather than failing, so the list always answers.
func (h Events) summarize(ctx context.Context, q eventQuery, now time.Time) (store.AdminSummary, error) {
	window, ok := q.window.summaryWindow(now)
	if !ok {
		return store.AdminSummary{Status: store.SummaryTooWide}, nil
	}
	release, ok := h.Summaries.Acquire(ctx)
	if !ok {
		return store.AdminSummary{Status: store.SummaryBusy}, ctx.Err()
	}
	defer release()
	bounded, cancel := context.WithTimeout(ctx, h.Summaries.Deadline)
	defer cancel()
	summary, err := h.Repo.SummarizeAdminEvents(bounded, q.filter, window)
	if err != nil && ctx.Err() == nil && bounded.Err() != nil {
		return store.AdminSummary{Status: store.SummaryTooSlow}, nil
	}
	return summary, err
}

func (h Events) Show(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("event")
	if !input.PublicID("evt", id) {
		httpapi.Fail(w, 404, "Event not found.")
		return
	}
	event, err := h.Repo.FindAdminEvent(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		httpapi.Fail(w, 404, "Event not found.")
		return
	}
	if err != nil {
		httpapi.InternalError(w, err)
		return
	}
	e := events.AdminEventViewed{Admin: httpapi.Admin(r).User.PublicID, Event: event.PublicID}
	if err := h.Repo.Record(r.Context(), e, store.EventScope{}); err != nil {
		httpapi.InternalError(w, err)
		return
	}
	httpapi.JSON(w, 200, event)
}

// adminCatalogue is what the filters offer: every event name and their
// groups, and every rate limit and listener name. It holds no user data, so reading it is the one admin read
// not recorded.
type adminCatalogue struct {
	Events      []string       `json:"events"`
	EventGroups []events.Group `json:"eventGroups"`
	Limits      []string       `json:"limits"`
	Listeners   []string       `json:"listeners"`
}

func (h Events) Catalogue(w http.ResponseWriter, r *http.Request) {
	limits := []string{mail.AccountExistsPerAccount.Name}
	for _, l := range httpapi.RateLimits {
		limits = append(limits, l.Name)
	}
	slices.Sort(limits)
	httpapi.JSON(w, 200, adminCatalogue{Events: events.Names(), EventGroups: events.Groups(), Limits: limits, Listeners: listeners(h.Server)})
}

// listeners names every registered listener, sorted.
func listeners(s *httpapi.Server) []string {
	names := []string{}
	if s.Repo.Listeners == nil {
		return names
	}
	for _, event := range events.Names() {
		names = append(names, s.Repo.Listeners.ListenersFor(event)...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}
