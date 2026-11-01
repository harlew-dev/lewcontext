package store

import (
	"context"
	"regexp"
	"time"

	"github.com/uptrace/bun"
)

// The Errors page's queries (docs/api/monitoring.md#errors): the grouped
// errors (app_errors) seen in a window, one group with its stack, and the
// release now running. app_errors is small by construction: at most 1,000
// groups between flushes, each a fingerprint, pruned 30 days after last
// seen.

// maxErrorGroups is how many groups the Errors page lists.
const maxErrorGroups = 50

// ReleaseShape is a release as the server and browsers report it
// (observe.Version: a VCS revision, -dirty for uncommitted changes, or
// dev). last_version is shown only in this shape: anything else in the
// column (it takes any [A-Za-z0-9._-]) isn't a release.
var ReleaseShape = regexp.MustCompile(`^([0-9a-f]{7,40}(-dirty)?|dev)$`)

// ErrorGroup is a grouped error as the Errors page lists it: never its
// fingerprint or stack (one group's page has the stack). A browser's group
// (source client) holds text a user could write: show it as text, never
// as a link.
type ErrorGroup struct {
	ID            string     `json:"id" bun:"public_id"`
	Source        string     `json:"source" bun:"source"`
	Route         string     `json:"route" bun:"route"`
	Message       string     `json:"message" bun:"message"`
	Count         int64      `json:"count" bun:"count"`
	FirstSeenAt   time.Time  `json:"firstSeenAt" bun:"first_seen_at"`
	LastSeenAt    time.Time  `json:"lastSeenAt" bun:"last_seen_at"`
	LastRequestID string     `json:"lastRequestId" bun:"last_request_id"`
	LastVersion   string     `json:"lastVersion" bun:"last_version"`
	ResolvedAt    *time.Time `json:"resolvedAt" bun:"resolved_at"`
	// New: first seen since the current release started (set by the
	// page, which knows the release; compared as times, never as text).
	New bool `json:"new" bun:"-"`
}

// ErrorGroupDetail is one group with its latest occurrence's stack
// (scrubbed: function names, files and lines).
type ErrorGroupDetail struct {
	ErrorGroup
	Stack string `json:"stack" bun:"stack"`
}

// errorGroupColumns are a group's columns as listed.
const errorGroupColumns = "ae.public_id, ae.source, ae.route, ae.message, ae.count, ae.first_seen_at, ae.last_seen_at, ae.last_request_id, ae.last_version, ae.resolved_at"

// releaseOnly keeps a group's last version only in ReleaseShape.
func (g *ErrorGroup) releaseOnly() {
	if !ReleaseShape.MatchString(g.LastVersion) {
		g.LastVersion = ""
	}
}

// IndexErrorGroups lists the groups last seen in [from, to), newest
// occurrence first (app_errors_last_seen_idx), at most maxErrorGroups.
func (s *Store) IndexErrorGroups(ctx context.Context, from, to time.Time) ([]ErrorGroup, error) {
	out := []ErrorGroup{}
	err := s.DB.NewSelect().Model((*appErrorRecord)(nil)).
		ColumnExpr(errorGroupColumns).
		Where("ae.last_seen_at >= ? AND ae.last_seen_at < ?", from, to).
		OrderExpr("ae.last_seen_at DESC, ae.id DESC").
		Limit(maxErrorGroups).
		Scan(ctx, &out)
	for i := range out {
		out[i].releaseOnly()
	}
	return out, err
}

// FindErrorGroup reads one group by its public ID, with its stack;
// sql.ErrNoRows when there's none.
func (s *Store) FindErrorGroup(ctx context.Context, publicID string) (ErrorGroupDetail, error) {
	var out ErrorGroupDetail
	err := s.DB.NewSelect().Model((*appErrorRecord)(nil)).
		ColumnExpr(errorGroupColumns+", ae.stack").
		Where("ae.public_id = ?", publicID).
		Scan(ctx, &out)
	out.releaseOnly()
	return out, err
}

// Release is the version the newest server started on, and when the
// first server started on it: errors first seen since are new in it.
type Release struct {
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
}

// FindCurrentRelease finds the newest server.started since since and the
// first one on its version since then (events_name_created_idx);
// sql.ErrNoRows when no server started since since.
func (s *Store) FindCurrentRelease(ctx context.Context, since time.Time) (Release, error) {
	var out Release
	started := func() *bun.SelectQuery {
		return s.DB.NewSelect().Model((*eventRecord)(nil)).
			Where("e.name = 'server.started'").
			Where("e.created_at >= ?", since)
	}
	err := started().ColumnExpr("e.payload->>'version'").
		OrderExpr("e.created_at DESC").Limit(1).
		Scan(ctx, &out.Version)
	if err != nil {
		return out, err
	}
	err = started().ColumnExpr("min(e.created_at)").
		Where("e.payload->>'version' = ?", out.Version).
		Scan(ctx, &out.StartedAt)
	if !ReleaseShape.MatchString(out.Version) {
		out.Version = ""
	}
	return out, err
}
