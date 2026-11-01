package store

import (
	"lewcontext/api/internal/database/dbtest"
	"strings"
	"testing"
)

// Erasure runs inside the account deletion's transaction, under the
// request's timeout, so each of its updates finds the events holding the
// address through its partial expression index, never by reading every
// admin read in the trail.
func TestErasureUsesItsIndexes(t *testing.T) {
	s, _ := queueStore(t, nil)
	pseudonym := func(email string) string { return "hmac:" + email }
	want := []string{"events_email_created_idx", "events_search_email_idx", "events_search_q_idx"}
	queries := redactions(s.DB, "Ada@Example.test", pseudonym)
	if len(queries) != len(want) {
		t.Fatalf("%d redactions", len(queries))
	}
	for i, q := range queries {
		if plan := dbtest.Explain(t, s.DB, q); !strings.Contains(plan, want[i]) {
			t.Errorf("redaction %d doesn't use %s:\n%s", i, want[i], plan)
		}
	}
}
