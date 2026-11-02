package account_test

import (
	"github.com/uptrace/bun"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/store"
	"testing"
)

// server is a Server on a migrated database of its own; router.New(s) is
// its handler.
func server(t *testing.T) (*httpapi.Server, *bun.DB) {
	t.Helper()
	db := apitest.Database(t)
	return apitest.Must(httpapi.New(&store.Store{DB: db}, apitest.Config()))(t), db
}
