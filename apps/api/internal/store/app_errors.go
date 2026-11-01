package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/observe"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// appErrorRecord is one grouped error (app_errors): a row per fingerprint,
// written only by ReportErrors (docs/api/observability.md#grouped-errors).
type appErrorRecord struct {
	bun.BaseModel `bun:"table:app_errors,alias:ae"`
	ID            int64 `bun:",pk,autoincrement"`
	Fingerprint   string
	Source        string
	Route         string
	Message       string
	Stack         string
	Count         int64
	FirstSeenAt   time.Time
	LastSeenAt    time.Time
	LastRequestID string
	LastVersion   string
	ResolvedAt    time.Time `bun:",nullzero"`
}

// ReportErrors upserts the error collector's groups (observe.Collector), in
// one statement with a row per fingerprint: a new group is inserted; a
// known one adds the count, widens its first and last times, takes the
// latest occurrence's request, release and stack, and is open again (a
// resolved group that recurs is a regression). The message is the same for
// a fingerprint, so it stays.
//
// A group that breaks one of the table's rules (validAppError) is never
// sent. If the statement still fails on the data (SQLSTATE classes 21, 22,
// 23 and 42), each group is sent on its own and those that fail on their
// own data are dropped: one bad group must not stop every later flush. The
// answer is then an *observe.FlushError naming the groups to retry (those
// that failed otherwise) and how many were dropped.
func (s *Store) ReportErrors(ctx context.Context, groups []observe.ErrorGroup) error {
	valid := make([]observe.ErrorGroup, 0, len(groups))
	for _, g := range groups {
		if validAppError(g) {
			valid = append(valid, g)
		}
	}
	dropped := len(groups) - len(valid)
	err := s.upsertErrors(ctx, valid)
	if err != nil && dataError(err) {
		var retry []observe.ErrorGroup
		var last error
		for _, g := range valid {
			switch one := s.upsertErrors(ctx, []observe.ErrorGroup{g}); {
			case one == nil:
			case dataError(one):
				dropped++
			default:
				retry, last = append(retry, g), one
			}
		}
		return &observe.FlushError{Retry: retry, Dropped: dropped, Err: last}
	}
	if err != nil {
		return err
	}
	if dropped > 0 {
		return &observe.FlushError{Dropped: dropped}
	}
	return nil
}

func (s *Store) upsertErrors(ctx context.Context, groups []observe.ErrorGroup) error {
	if len(groups) == 0 {
		return nil
	}
	rows := make([]appErrorRecord, len(groups))
	for i, g := range groups {
		rows[i] = appErrorRecord{
			Fingerprint: g.Fingerprint, Source: g.Source, Route: g.Route,
			Message: g.Message, Stack: g.Stack, Count: g.Count,
			FirstSeenAt: g.FirstSeen, LastSeenAt: g.LastSeen,
			LastRequestID: g.LastRequestID, LastVersion: g.LastVersion,
		}
	}
	latest := "EXCLUDED.last_seen_at >= ae.last_seen_at"
	_, err := s.DB.NewInsert().Model(&rows).
		ExcludeColumn("id", "resolved_at").
		On("CONFLICT (fingerprint) DO UPDATE").
		Set("count = ae.count + EXCLUDED.count").
		Set("first_seen_at = LEAST(ae.first_seen_at, EXCLUDED.first_seen_at)").
		Set("last_seen_at = GREATEST(ae.last_seen_at, EXCLUDED.last_seen_at)").
		Set("last_request_id = CASE WHEN " + latest + " THEN EXCLUDED.last_request_id ELSE ae.last_request_id END").
		Set("last_version = CASE WHEN " + latest + " THEN EXCLUDED.last_version ELSE ae.last_version END").
		Set("stack = CASE WHEN " + latest + " AND EXCLUDED.stack <> '' THEN EXCLUDED.stack ELSE ae.stack END").
		Set("resolved_at = NULL").
		Exec(ctx)
	return err
}

// dataError reports whether PostgreSQL refused a statement for what it
// held (cardinality, data exception, integrity constraint, or a statement
// a value broke), rather than because it couldn't run it.
func dataError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || len(pgErr.Code) < 2 {
		return false
	}
	switch pgErr.Code[:2] {
	case "21", "22", "23", "42":
		return true
	}
	return false
}

// Bounds of app_errors' columns (020_create_app_errors), checked before a
// group is sent.
var (
	fingerprintShape = regexp.MustCompile(`^[0-9a-f]{64}$`)
	requestIDShape   = regexp.MustCompile(`^req_[0-9A-Za-z]{14}$`)
	versionShape     = regexp.MustCompile(`^[A-Za-z0-9._-]{0,64}$`)
	errorSources     = map[string]bool{
		observe.SourceRequest: true, observe.SourcePanic: true, observe.SourceWorker: true,
		observe.SourceBackground: true, observe.SourceClient: true,
	}
)

// validAppError reports whether g fits app_errors' CHECKs, and holds no
// control or format character (a NUL can't be stored at all), apart from
// the line breaks and tabs that join a stack's lines.
func validAppError(g observe.ErrorGroup) bool {
	n := utf8.RuneCountInString(g.Message)
	return fingerprintShape.MatchString(g.Fingerprint) && errorSources[g.Source] &&
		utf8.RuneCountInString(g.Route) <= 200 && plain(g.Route, "") &&
		n >= 1 && n <= observe.MaxMessage && plain(g.Message, "") &&
		utf8.RuneCountInString(g.Stack) <= observe.MaxStack && plain(g.Stack, "\n\t") &&
		g.Count > 0 && !g.FirstSeen.IsZero() && !g.LastSeen.IsZero() &&
		(g.LastRequestID == "" || requestIDShape.MatchString(g.LastRequestID)) &&
		versionShape.MatchString(g.LastVersion)
}

// plain reports whether s is valid UTF-8 without control or format
// characters other than those in allowed.
func plain(s, allowed string) bool {
	return utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool {
		return (unicode.IsControl(r) || unicode.Is(unicode.Cf, r)) && !strings.ContainsRune(allowed, r)
	})
}
