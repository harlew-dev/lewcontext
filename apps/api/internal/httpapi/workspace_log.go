package httpapi

import (
	"database/sql"
	"errors"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"time"
)

// A workspace's log of changes (docs/api/events.md#the-workspace-log): the
// list and one event, read through the owners' and admins' routes and, for
// one personal API key, through its owner's. It shows a projection of each
// event the log allows (events.WorkspaceLog), never the stored payload, an
// IP or a user agent, and reading it records nothing.

// Log page sizes.
const (
	defaultLogPerPage = 50
	maxLogPerPage     = 100
)

// The name filter's bounds: the log has under 20 names and 5 groups.
const (
	maxLogNameTokens = 32
	maxLogNameParam  = 1024
)

// Relative windows the log offers, by their query value. Events are kept at
// most a year, workspace changes 90 days, so "all" is what is kept.
const (
	logWindowDay     = "24h"
	logWindowWeek    = "7d"
	logWindowMonth   = "30d"
	logWindowAll     = "all"
	defaultLogWindow = logWindowWeek
)

// maxLogRange is the longest custom range, as the admin Events page's.
const maxLogRange = 366 * 24 * time.Hour

// Years a filter's time may name: PostgreSQL and JSON both take these.
const (
	minFilterYear = 1970
	maxFilterYear = 9999
)

// logNames expands the name filter: comma-separated tokens, each an event
// in the log or a group ID, to the union of their names. Anything else (an
// unknown, empty or repeated token, too many, too long) is refused, so the
// query is always name IN (…) on known names, never LIKE.
func logNames(param string) ([]string, bool) {
	if len(param) > maxLogNameParam {
		return nil, false
	}
	tokens := strings.Split(param, ",")
	if len(tokens) > maxLogNameTokens {
		return nil, false
	}
	groups := events.WorkspaceLogGroups()
	picked := map[string]bool{}
	for i, token := range tokens {
		if token == "" || slices.Contains(tokens[:i], token) {
			return nil, false
		}
		switch {
		case events.InWorkspaceLog(token):
			picked[token] = true
		case groups[token] != nil:
			for _, name := range groups[token] {
				picked[name] = true
			}
		default:
			return nil, false
		}
	}
	var names []string
	for _, name := range events.WorkspaceLogNames() {
		if picked[name] {
			names = append(names, name)
		}
	}
	return names, true
}

// logTime reads an RFC 3339 time in years 1970–9999; "" is no filter.
func logTime(raw string) (*time.Time, bool) {
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	year := t.UTC().Year()
	if err != nil || year < minFilterYear || year > maxFilterYear {
		return nil, false
	}
	t = t.UTC()
	return &t, true
}

// logWindow reads window, or from and to (never both), at now. No window is
// the last 7 days; "all" has no start.
func logWindow(params map[string]string, now time.Time) (from, to *time.Time, ok bool) {
	name, rawFrom, rawTo := params["window"], params["from"], params["to"]
	if name != "" && (rawFrom != "" || rawTo != "") {
		return nil, nil, false
	}
	if rawFrom != "" || rawTo != "" {
		from, okFrom := logTime(rawFrom)
		to, okTo := logTime(rawTo)
		if !okFrom || !okTo || from != nil && to != nil && (!from.Before(*to) || to.Sub(*from) > maxLogRange) {
			return nil, nil, false
		}
		return from, to, true
	}
	if name == "" {
		name = defaultLogWindow
	}
	length := map[string]time.Duration{
		logWindowDay: 24 * time.Hour, logWindowWeek: 7 * 24 * time.Hour, logWindowMonth: 30 * 24 * time.Hour,
	}[name]
	switch {
	case name == logWindowAll:
		return nil, nil, true
	case length > 0:
		start := now.Add(-length)
		return &start, nil, true
	}
	return nil, nil, false
}

// logQuery parses the log's query string at now; anything it doesn't
// accept is false (400).
func logQuery(r *http.Request, now time.Time, allowed []string) (store.WorkspaceEventFilter, int, bool) {
	var f store.WorkspaceEventFilter
	params, ok := input.Query(r, allowed...)
	if !ok {
		return f, 0, false
	}
	perPage, ok := input.Int(params["perPage"], defaultLogPerPage, maxLogPerPage)
	if !ok {
		return f, 0, false
	}
	f.Actor, f.Subject, f.Account, f.Project, f.Key, f.Before = params["actor"], params["subject"], params["account"], params["project"], params["key"], params["before"]
	f.Search = strings.TrimSpace(params["q"])
	valid := (f.Actor == "" || input.PublicID("usr", f.Actor)) &&
		(f.Subject == "" || input.PublicID("usr", f.Subject)) &&
		(f.Account == "" || input.PublicID("usr", f.Account)) &&
		(f.Key == "" || input.PublicID("apk", f.Key)) &&
		(f.Project == "" || input.PublicID("prj", f.Project)) &&
		(f.Before == "" || input.PublicID("evt", f.Before)) &&
		input.Search(f.Search)
	if params["name"] != "" {
		f.Names, ok = logNames(params["name"])
		valid = valid && ok
	}
	f.From, f.To, ok = logWindow(params, now)
	return f, perPage, valid && ok
}

// workspaceEventIndex is a page of the log, with the names behind its
// filters.
type workspaceEventIndex struct {
	store.WorkspaceEventPage
	Refs store.WorkspaceEventRefs `json:"refs"`
}

// logParams are the parameters of the full log; a key's own log takes fewer.
var (
	logParams    = []string{"name", "actor", "subject", "account", "project", "key", "q", "window", "from", "to", "before", "perPage"}
	keyLogParams = []string{"name", "q", "window", "from", "to", "before", "perPage"}
)

// IndexWorkspaceLog answers a page of workspaceID's log. A key forces the
// log to that key's own events and limits the parameters to keyLogParams.
func (s *Server) IndexWorkspaceLog(w http.ResponseWriter, r *http.Request, workspaceID int64, key string) {
	allowed := logParams
	if key != "" {
		allowed = keyLogParams
	}
	f, perPage, ok := logQuery(r, time.Now().UTC(), allowed)
	if !ok {
		Fail(w, 400, "Check the filters.")
		return
	}
	if key != "" {
		f.Key = key
	}
	page, err := s.Repo.IndexWorkspaceEvents(r.Context(), workspaceID, f, perPage)
	if errors.Is(err, store.ErrNoCursor) {
		Fail(w, 400, "That page is no longer available. Start from the newest changes.")
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	out := workspaceEventIndex{WorkspaceEventPage: page}
	if out.Refs, err = s.Repo.FindWorkspaceEventRefs(r.Context(), workspaceID, f); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, 200, out)
}

// ShowWorkspaceLogEvent answers one event of workspaceID's log with the
// others of its request.
func (s *Server) ShowWorkspaceLogEvent(w http.ResponseWriter, r *http.Request, workspaceID int64) {
	id := r.PathValue("event")
	if !input.PublicID("evt", id) {
		Fail(w, 404, "Change not found.")
		return
	}
	event, err := s.Repo.FindWorkspaceEvent(r.Context(), workspaceID, id)
	if errors.Is(err, sql.ErrNoRows) {
		Fail(w, 404, "Change not found.")
		return
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, 200, event)
}
