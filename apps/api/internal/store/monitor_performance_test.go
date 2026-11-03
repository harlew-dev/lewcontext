package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// A failed read of pg_stat_statements is never the Performance page's
// failure: a revoked grant (42501) or an older version's missing column
// (42703) answers unavailable with the reason, while the part's own end
// (its budget, the request gone) is still returned, so the part shows as
// not loaded.
func TestStatementsFailed(t *testing.T) {
	ctx := context.Background()
	for _, code := range []string{"42501", "42703", "XX000"} {
		out, err := statementsFailed(ctx, StatementStats{Statements: []Statement{}}, &pgconn.PgError{Code: code, Message: "permission denied"})
		if err != nil || out.Available || out.Unavailable != StatementsUnreadable {
			t.Fatalf("%s: %+v %v", code, out, err)
		}
	}
	if _, err := statementsFailed(ctx, StatementStats{}, errors.New("connection reset")); err != nil {
		t.Fatalf("another error: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := statementsFailed(canceled, StatementStats{}, context.Canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("the part's own end: %v", err)
	}
}
