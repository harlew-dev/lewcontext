package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/observe"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// appErrorRecord is one grouped error (app_errors): a row per fingerprint,
// written only by RecordErrors (docs/api/observability.md#grouped-errors).
type appErrorRecord struct {
	bun.BaseModel `bun:"table:app_errors,alias:ae"`
	ID            int64  `bun:",pk,autoincrement"`
	PublicID      string `bun:",nullzero"`
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

// RecordErrors upserts the error collector's groups (observe.Collector), in
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
func (s *Store) RecordErrors(ctx context.Context, groups []observe.ErrorGroup) error {
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
	// Stable lock order across collectors/servers, without mutating the caller's
	// buffer. Parent locks precede cycle locks in resolution and pruning too.
	ordered := append([]observe.ErrorGroup(nil), groups...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Fingerprint < ordered[j].Fingerprint })
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, g := range ordered {
			incoming := appErrorRecord{Fingerprint: g.Fingerprint, Source: g.Source, Route: g.Route, Message: g.Message, Stack: g.Stack, Count: g.Count, FirstSeenAt: g.FirstSeen, LastSeenAt: g.LastSeen, LastRequestID: g.LastRequestID, LastVersion: g.LastVersion}
			result, err := tx.NewInsert().Model(&incoming).On("CONFLICT (fingerprint) DO NOTHING").Exec(ctx)
			if err != nil {
				return err
			}
			inserted, err := result.RowsAffected()
			if err != nil {
				return err
			}
			var row appErrorRecord
			if err := tx.NewSelect().Model(&row).Where("ae.fingerprint = ?", g.Fingerprint).For("UPDATE").Scan(ctx); err != nil {
				return err
			}
			parent := IncidentParent{AppErrorID: row.ID}
			cycle, err := FindCurrentIncidentCycle(ctx, tx, parent, true)
			if errors.Is(err, sql.ErrNoRows) {
				cycle, err = CreateIncidentCycle(ctx, tx, parent, row.FirstSeenAt)
				if err == nil && !row.ResolvedAt.IsZero() {
					_, err = tx.NewUpdate().Model(&cycle).Set("resolved_at = ?", row.ResolvedAt).Set("resolution_source = 'unknown_legacy'").WherePK().Exec(ctx)
				}
			}
			if err != nil {
				return err
			}
			if inserted > 0 {
				continue
			}
			if !row.ResolvedAt.IsZero() && g.LastSeen.After(row.ResolvedAt) {
				if _, err := CreateIncidentCycle(ctx, tx, parent, g.LastSeen); err != nil {
					return err
				}
				row.ResolvedAt = time.Time{}
			}
			row.Count += g.Count
			if g.FirstSeen.Before(row.FirstSeenAt) {
				row.FirstSeenAt = g.FirstSeen
			}
			if !g.LastSeen.Before(row.LastSeenAt) {
				row.LastSeenAt = g.LastSeen
				row.LastRequestID = g.LastRequestID
				row.LastVersion = g.LastVersion
				if g.Stack != "" {
					row.Stack = g.Stack
				}
			}
			if _, err := tx.NewUpdate().Model(&row).Column("count", "first_seen_at", "last_seen_at", "last_request_id", "last_version", "stack", "resolved_at").WherePK().Exec(ctx); err != nil {
				return err
			}
		}
		return nil
	})
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
