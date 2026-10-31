package workspace_test

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

// mailServer is server with reset links and invitations sent to a mailbox
// when deliver runs the worker once, as the server's worker would.
func mailServer(t *testing.T) (s *httpapi.Server, db *bun.DB, box *apitest.Mailbox, deliver func()) {
	t.Helper()
	s, db = server(t)
	box, deliver = apitest.Mail(t, s.Repo, s.Config.Origin)
	return s, db, box, deliver
}
