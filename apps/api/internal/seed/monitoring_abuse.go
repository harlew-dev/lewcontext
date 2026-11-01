package seed

import (
	"context"
	"fmt"
	"lewcontext/api/internal/factory"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
)

// The monitoring seeder's second part: what the IP addresses, Accounts,
// Workspaces and Usage pages look for. An invitation spammer, new today,
// with a link in its name and its workspace's, sending invitations nobody
// accepts and throwing workspaces away, from an address that made many
// accounts; a workspace near its project quota, its owner making projects
// by the hundred; ordinary accounts active today, this week and this
// month, some of them activated; and accounts not seen for two years. Fake
// accounts have unusable passwords; invitations are rows with no link, so
// no mail is ever sent (the seeder writes events directly, queuing no
// deliveries).

// The seeded addresses of part 2, from the documentation ranges.
const (
	SeedClusterIP = "198.51.100.77"
	seedSpamIP    = "203.0.113.120"
)

// abuse plants part 2's patterns, adding its events to planted.
type abuse struct {
	ctx context.Context
	tx  bun.Tx
	f   *gofakeit.Faker
	now time.Time
	add func(name string, ago time.Duration, ip string, payload map[string]any, scope ...func(*factory.Event))
}

// fakeAccount makes an account nobody can sign in to.
func (a abuse) fakeAccount(name string, created time.Time, lastSeen *time.Time) (factory.User, error) {
	return factory.Users.State(func(u *factory.User) {
		u.Name, u.PasswordHash, u.CreatedAt, u.LastSeenAt = name, "unusable:"+a.f.LetterN(32), created, lastSeen
	}).CreateOne(a.ctx, a.tx, a.f)
}

// seedAbuse plants every part 2 pattern, returning what it planted.
func seedAbuse(a abuse) (string, error) {
	spammer, err := a.spammer()
	if err != nil {
		return "", err
	}
	full, err := a.nearlyFull()
	if err != nil {
		return "", err
	}
	if err := a.usage(); err != nil {
		return "", err
	}
	return fmt.Sprintf("an invitation spammer (%s) from %s, sign-ups clustered on %s, %q near its project quota, accounts active and inactive",
		spammer.Name, seedSpamIP, SeedClusterIP, full.Name), nil
}

// spammer plants an account made today from an address that made eight
// more, its name and its workspace's holding links, inviting 25 addresses
// nobody accepts, owning 17 workspaces, and making then deleting four
// more within the hour.
func (a abuse) spammer() (factory.User, error) {
	ago := func(d time.Duration) time.Time { return a.now.Add(-d) }
	u, err := a.fakeAccount("Visit http://cheap-pills.example", ago(5*time.Hour), nil)
	if err != nil {
		return u, err
	}
	of := func(e *factory.Event) { e.ActorID, e.UserID = u.ID, u.ID }
	a.add("auth.registered", 5*time.Hour, SeedClusterIP, map[string]any{"user": u.PublicID, "name": u.Name, "email": u.Email}, of)
	for i := range 8 {
		a.add("auth.registered", time.Duration(6+i)*time.Hour, SeedClusterIP, map[string]any{"user": fakeID(a.f, "usr"), "email": fmt.Sprintf("farm%02d@example.test", i)})
	}
	spam, err := factory.WorkspaceFor(a.ctx, a.tx, a.f, u.ID, factory.Named("Free money at www.cheap-pills.example"))
	if err != nil {
		return u, err
	}
	for i := range 16 {
		if _, err := factory.WorkspaceFor(a.ctx, a.tx, a.f, u.ID, factory.Named(fmt.Sprintf("Promo %02d", i+1))); err != nil {
			return u, err
		}
	}
	by := func(e *factory.Event) { e.ActorID, e.WorkspaceID = u.ID, spam.ID }
	a.add("workspace.created", 4*time.Hour, seedSpamIP, map[string]any{"workspace": spam.PublicID, "name": spam.Name}, by)
	// 30 invitations to 25 addresses in the last 4 hours, as live rows
	// with no link (45 of the 50 a workspace may hold), none accepted.
	if _, err := factory.InvitationsTo(spam.ID).State(func(i *factory.WorkspaceInvitation) { i.InvitedBy = u.ID }).Create(a.ctx, a.tx, a.f, 45); err != nil {
		return u, err
	}
	for i := range 30 {
		a.add("workspace.invitation.created", time.Duration(4*60-i*7)*time.Minute, seedSpamIP, map[string]any{
			"invitation": fakeID(a.f, "inv"), "workspace": spam.PublicID, "email": fmt.Sprintf("target%02d@example.test", i%25), "role": "member",
		}, by)
	}
	// Four workspaces made and deleted within the hour, two of them
	// inviting first: with the one kept, five made today.
	for i := range 4 {
		id := fakeID(a.f, "wsp")
		start := time.Duration(4-i)*time.Hour + 30*time.Minute
		a.add("workspace.created", start, seedSpamIP, map[string]any{"workspace": id, "name": "Prize draw"}, func(e *factory.Event) { e.ActorID = u.ID })
		if i < 2 {
			a.add("workspace.invitation.created", start-10*time.Minute, seedSpamIP, map[string]any{"invitation": fakeID(a.f, "inv"), "workspace": id, "email": fmt.Sprintf("prize%d@example.test", i), "role": "member"}, func(e *factory.Event) { e.ActorID = u.ID })
		}
		a.add("workspace.deleted", start-40*time.Minute, seedSpamIP, map[string]any{"workspace": id, "name": "Prize draw", "projects": 0, "members": 1}, func(e *factory.Event) { e.ActorID = u.ID })
	}
	return u, nil
}

// nearlyFull plants a workspace holding 850 of its 1,000 projects, its
// owner having made 210 of them today.
func (a abuse) nearlyFull() (factory.Workspace, error) {
	seen := a.now.Add(-20 * time.Minute)
	owner, err := a.fakeAccount("Morgan Archer", a.now.AddDate(0, -8, 0), &seen)
	if err != nil {
		return factory.Workspace{}, err
	}
	w, err := factory.WorkspaceFor(a.ctx, a.tx, a.f, owner.ID, factory.Named("Northwind Archive"))
	if err != nil {
		return w, err
	}
	if _, err := factory.ProjectsIn(w.ID).Create(a.ctx, a.tx, a.f, 850); err != nil {
		return w, err
	}
	for i := range 210 {
		a.add("project.created", time.Duration(i*6)*time.Minute, "192.0.2.80", map[string]any{
			"project": fakeID(a.f, "prj"), "workspace": w.PublicID, "name": "Import", "status": "Planned", "color": "blue",
		}, func(e *factory.Event) { e.ActorID, e.WorkspaceID = owner.ID, w.ID })
	}
	return w, nil
}

// usage plants ordinary accounts: active today, this week and this month
// (one with MFA), sign-ups in the last two weeks that made or joined a
// workspace and invited someone, feature use over the day, and accounts
// not seen for two years.
func (a abuse) usage() error {
	for i, ago := range []time.Duration{time.Hour, 3 * time.Hour, 2 * 24 * time.Hour, 5 * 24 * time.Hour, 12 * 24 * time.Hour, 25 * 24 * time.Hour} {
		seen := a.now.Add(-ago)
		u, err := a.fakeAccount(a.f.Name(), a.now.AddDate(0, -3, 0), &seen)
		if err != nil {
			return err
		}
		if i == 0 {
			// Two-factor on (a secret no one holds, for an account no one
			// can sign in to), so the week's share with MFA isn't zero.
			if _, err := factory.MFATOTPs.State(factory.TOTPFor(u.ID)).CreateOne(a.ctx, a.tx, a.f); err != nil {
				return err
			}
			a.add("auth.mfa.enabled", 50*time.Minute, "192.0.2.31", map[string]any{"user": u.PublicID}, func(e *factory.Event) { e.ActorID, e.UserID = u.ID, u.ID })
		}
	}
	// Sign-ups 2 to 13 days ago; every other one made a workspace within
	// hours, and those over a week old invited someone.
	for i := range 8 {
		registered := time.Duration(2+i*3/2) * 24 * time.Hour
		u, err := a.fakeAccount(a.f.Name(), a.now.Add(-registered), nil)
		if err != nil {
			return err
		}
		of := func(e *factory.Event) { e.ActorID, e.UserID = u.ID, u.ID }
		a.add("auth.registered", registered, a.docIP(), map[string]any{"user": u.PublicID, "name": u.Name, "email": u.Email}, of)
		if i%2 == 0 {
			id := fakeID(a.f, "wsp")
			a.add("workspace.created", registered-3*time.Hour, a.docIP(), map[string]any{"workspace": id, "name": "First"}, func(e *factory.Event) { e.ActorID = u.ID })
			if registered > 7*24*time.Hour {
				a.add("workspace.invitation.created", registered-2*24*time.Hour, a.docIP(), map[string]any{"invitation": fakeID(a.f, "inv"), "workspace": id, "email": "colleague@example.test", "role": "member"}, func(e *factory.Event) { e.ActorID = u.ID })
			}
		}
	}
	for i := range 40 {
		a.add("project.updated", time.Duration(i*35)*time.Minute, a.docIP(), map[string]any{"project": fakeID(a.f, "prj"), "workspace": fakeID(a.f, "wsp"), "changes": map[string]any{}})
	}
	// Not seen for two years, or never since made three years ago.
	lastSeen := a.now.AddDate(-2, -3, 0)
	if _, err := a.fakeAccount("Quinn Dormant", a.now.AddDate(-4, 0, 0), &lastSeen); err != nil {
		return err
	}
	_, err := a.fakeAccount("Riley Neverback", a.now.AddDate(-3, 0, 0), nil)
	return err
}

// docIP is an address from 192.0.2.0/24.
func (a abuse) docIP() string { return fmt.Sprintf("192.0.2.%d", a.f.IntRange(1, 254)) }
