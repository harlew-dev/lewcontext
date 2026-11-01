package seed

import (
	"fmt"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/observe"
	"time"
)

// The monitoring seeder's third part: what the Integrity and Errors pages
// look for. A delivery stuck for 25 minutes (critical: the worker looks
// stalled), for a listener nobody registered, so a running server's worker
// fails it at once instead of sending anything, which the "no listener"
// check then shows; a delivery that already failed that way; a project
// made twice within 3 seconds (a double submit) and a reset sent twice for
// one request; and three error groups: a server error new today, an older
// panic already resolved, and one a browser reported, whose text is a
// user's (a link-like phrase, scrubbed of its host, as the collector
// stores it). No metrics or server rows: a running server writes its own,
// and made-up traffic would be misleading.

// seedStalledListener is the seeded deliveries' listener: no server has
// one by that name, so nothing is ever sent for them.
const seedStalledListener = "seed.stalled"

// seedHealth plants part 3's patterns, returning what it planted.
func seedHealth(a abuse) (string, error) {
	if err := a.stuckDeliveries(); err != nil {
		return "", err
	}
	if err := a.duplicates(); err != nil {
		return "", err
	}
	if err := a.errorGroups(); err != nil {
		return "", err
	}
	return "a delivery stuck 25 minutes and one failed with no listener, a double-submitted project, a reset sent twice, three error groups (one reported by a browser)", nil
}

// stuckDeliveries queues a delivery 25 minutes overdue and one failed for
// want of a listener, each for an event of its own.
func (a abuse) stuckDeliveries() error {
	for i, state := range []func(*factory.EventDelivery){
		func(d *factory.EventDelivery) { d.NextAttemptAt = a.now.Add(-25 * time.Minute) },
		func(d *factory.EventDelivery) {
			d.Attempts, d.NextAttemptAt, d.FailedAt = 1, a.now.Add(-3*time.Hour), a.now.Add(-3*time.Hour)
			d.LastError = `no listener "…" for auth.password_reset.requested`
		},
	} {
		e, err := factory.Events.State(factory.EventOf("auth.password_reset.requested", map[string]any{
			"email": fmt.Sprintf("stuck%d@example.test", i), "seeded": true,
		})).State(func(e *factory.Event) { e.CreatedAt = a.now.Add(-time.Duration(25+i*155) * time.Minute) }).CreateOne(a.ctx, a.tx, a.f)
		if err != nil {
			return err
		}
		if _, err := factory.DeliveryOf(e.ID, seedStalledListener).State(state).CreateOne(a.ctx, a.tx, a.f); err != nil {
			return err
		}
	}
	return nil
}

// duplicates plants a project made twice within 3 seconds by one account,
// and a reset link sent twice for one request.
func (a abuse) duplicates() error {
	clicker, err := a.fakeAccount("Casey Doubleclick", a.now.AddDate(0, -2, 0), nil)
	if err != nil {
		return err
	}
	workspace := fakeID(a.f, "wsp")
	for i := range 2 {
		a.add("project.created", time.Duration(50*60-3*i)*time.Second, a.docIP(),
			map[string]any{"project": fakeID(a.f, "prj"), "workspace": workspace, "name": "Launch plan", "description": "", "status": "active", "color": "sky"},
			func(e *factory.Event) { e.ActorID = clicker.ID })
	}
	request, user := fakeID(a.f, "evt"), fakeID(a.f, "usr")
	for i := range 2 {
		a.add("auth.password_reset.sent", time.Duration(70+i)*time.Minute, "",
			map[string]any{"email": "twice@example.test", "user": user, "request": request})
	}
	return nil
}

// errorGroups plants three groups as the collector stores them (scrubbed).
func (a abuse) errorGroups() error {
	groups := []factory.AppError{
		{Source: observe.SourceRequest, Route: "POST /api/workspaces/{workspace}/projects",
			Message: `ERROR: duplicate key value violates unique constraint "projects_workspace_id_name_key" (SQLSTATE <n>)`,
			Stack:   "store.(*Store).CreateProject(…)\n\tprojects.go:<n>\nworkspace.Projects.Store(…)\n\tprojects.go:<n>",
			Count:   7, FirstSeenAt: a.now.Add(-40 * time.Minute), LastSeenAt: a.now.Add(-6 * time.Minute),
			LastRequestID: fakeID(a.f, "req"), LastVersion: "dev"},
		{Source: observe.SourcePanic, Route: "GET /api/workspaces/{workspace}/members",
			Message: "runtime error: index out of range [<n>] with length <n>",
			Stack:   "workspace.Members.Index(…)\n\tmembers.go:<n>",
			Count:   2, FirstSeenAt: a.now.Add(-5 * 24 * time.Hour), LastSeenAt: a.now.Add(-4 * 24 * time.Hour),
			LastRequestID: fakeID(a.f, "req"), LastVersion: "dev", ResolvedAt: a.now.Add(-3 * 24 * time.Hour)},
		{Source: observe.SourceClient, Route: "/app/:workspace/projects",
			Message: "Error: Claim your prize at <url> before midnight",
			Stack:   "at render (index-<token>.js:<n>:<n>)",
			Count:   3, FirstSeenAt: a.now.Add(-3 * time.Hour), LastSeenAt: a.now.Add(-2 * time.Hour)},
	}
	for _, g := range groups {
		// A fingerprint of its own, so seeding again adds groups rather
		// than colliding with these (or a real one).
		g.Fingerprint = observe.Fingerprint(g.Source, g.Route, g.Message+"\x00"+a.f.UUID())
		if _, err := factory.AppErrors.State(func(e *factory.AppError) { *e = g }).CreateOne(a.ctx, a.tx, a.f); err != nil {
			return err
		}
	}
	return nil
}
