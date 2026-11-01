package store

import (
	"context"
	"time"
)

// The Configuration page's queries (docs/api/monitoring.md#configuration):
// the servers that have run lately with their configuration's summaries,
// and the admin sessions and admins its warnings count.

// maxServers is how many servers the Configuration page lists: a server's
// row goes a day after it was last heard from, so a crash loop could leave
// many.
const maxServers = 50

// IndexServerInstances lists the servers heard from in the last day (the
// rollup deletes older rows), most recently heard from first, with their
// configuration's summary as each wrote it.
func (s *Store) IndexServerInstances(ctx context.Context) ([]ServerInstance, error) {
	out := []ServerInstance{}
	err := s.DB.NewSelect().Model(&out).
		OrderExpr("si.last_seen_at DESC, si.id").
		Limit(maxServers).
		Scan(ctx)
	return out, err
}

// CountAdminSessionsUnder counts the live admin sessions made under the
// admin key whose digest is keyHash: those still on ADMIN_ROUTE_KEY_PREVIOUS
// end when it's removed.
func (s *Store) CountAdminSessionsUnder(ctx context.Context, keyHash string) (int64, error) {
	if keyHash == "" {
		return 0, nil
	}
	n, err := s.DB.NewSelect().Model((*adminSessionRecord)(nil)).
		Where("adms.key_hash = ?", keyHash).
		Where("adms.expires_at > now() AND adms.last_seen_at > now() - make_interval(secs => ?)", adminIdleSeconds).
		Count(ctx)
	return int64(n), err
}

// CountAdminsWithMFA counts the admins who can sign in: MFA on.
func (s *Store) CountAdminsWithMFA(ctx context.Context) (int64, error) {
	n, err := s.DB.NewSelect().Model((*adminRecord)(nil)).
		Join("JOIN users AS u ON u.id = adm.user_id").
		Where("(?) IS NOT NULL", mfaEnabledAt(s.DB)).
		Count(ctx)
	return int64(n), err
}

// ServerLiveSince is when a server must have been heard from to count as
// live at now.
func ServerLiveSince(now time.Time) time.Time { return now.Add(-ServerLiveFor) }
