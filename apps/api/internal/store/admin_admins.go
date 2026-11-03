package store

import (
	"context"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// The Admins page (docs/api/admin.md): who holds admin, with what role and
// MFA, when they last signed in to the dashboard, and their live admin
// sessions. An access review on one page.

// maxAdminAccounts bounds the Admins page. Admins are granted one by one
// from the command line, so this is far above any real count.
const maxAdminAccounts = 500

// AdminAccount is an admin as the Admins page lists them. LastSignedInAt is
// their newest admin.logged_in, kept as long as events are (90 days by
// default); Sessions are their live admin sessions.
type AdminAccount struct {
	ID             int64                 `bun:"id" json:"-"`
	User           AdminPerson           `bun:"-" json:"user"`
	Role           auth.AdminRole        `bun:"role" json:"role"`
	MFAEnabled     bool                  `bun:"mfa_enabled" json:"mfaEnabled"`
	MFASetupUntil  *time.Time            `bun:"mfa_setup_until" json:"mfaSetupUntil"`
	GrantedAt      time.Time             `bun:"granted_at" json:"grantedAt"`
	LastSignedInAt *time.Time            `bun:"last_signed_in_at" json:"lastSignedInAt"`
	Sessions       []AdminAccountSession `bun:"-" json:"sessions"`
}

// AdminAccountSession is one live admin session: where it signed in from,
// whether it's the one asking (Current), and whether it was made under the
// key being retired (PreviousKey). Never its token or CSRF token.
type AdminAccountSession struct {
	UserID      int64     `bun:"user_id" json:"-"`
	CreatedAt   time.Time `bun:"created_at" json:"createdAt"`
	LastSeenAt  time.Time `bun:"last_seen_at" json:"lastSeenAt"`
	IP          string    `bun:"ip" json:"ip"`
	UserAgent   string    `bun:"user_agent" json:"userAgent"`
	Current     bool      `bun:"current" json:"current"`
	PreviousKey bool      `bun:"previous_key" json:"previousKey"`
}

// AdminKeys are the digests a live admin session may have been made under:
// the current ADMIN_ROUTE_KEY's, and the previous one's ("" when unset).
type AdminKeys struct {
	Current, Previous string
}

// hashes are the digests that are set.
func (k AdminKeys) hashes() []string {
	if k.Previous == "" {
		return []string{k.Current}
	}
	return []string{k.Current, k.Previous}
}

// IndexAdminAdmins lists every admin, oldest grant first, with their live
// admin sessions (made under one of keys, newest first). current is the
// digest of the asking session's token, which only marks it Current. Two
// queries: the admins, then their sessions (at most MaxAdminSessions each).
func (s *Store) IndexAdminAdmins(ctx context.Context, current string, keys AdminKeys) ([]AdminAccount, error) {
	lastSignIn := s.DB.NewSelect().Model((*eventRecord)(nil)).Column("e.created_at").
		Where("e.actor_id = adm.user_id AND e.name = ?", events.AdminLoggedIn{}.EventName()).
		OrderExpr("e.created_at DESC").Limit(1)
	rows, err := s.DB.NewSelect().Model((*adminRecord)(nil)).
		Join("JOIN users AS u ON u.id = adm.user_id").
		ColumnExpr("u.id, u.public_id, u.name, u.email").
		ColumnExpr("(?) IS NOT NULL AS mfa_enabled", mfaEnabledAt(s.DB)).
		ColumnExpr("adm.role, adm.mfa_setup_until, adm.created_at AS granted_at").
		ColumnExpr("(?) AS last_signed_in_at", lastSignIn).
		OrderExpr("adm.created_at, adm.user_id").
		Limit(maxAdminAccounts).
		Rows(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	admins := []AdminAccount{}
	byID := map[int64]int{}
	for rows.Next() {
		var a AdminAccount
		if err := rows.Scan(&a.ID, &a.User.PublicID, &a.User.Name, &a.User.Email, &a.MFAEnabled, &a.Role, &a.MFASetupUntil, &a.GrantedAt, &a.LastSignedInAt); err != nil {
			return nil, err
		}
		a.Sessions = []AdminAccountSession{}
		byID[a.ID] = len(admins)
		admins = append(admins, a)
	}
	if err := rows.Err(); err != nil || len(admins) == 0 {
		return admins, err
	}
	sessions, err := s.liveAdminSessions(ctx, admins, current, keys)
	for _, session := range sessions {
		i := byID[session.UserID]
		admins[i].Sessions = append(admins[i].Sessions, session)
	}
	return admins, err
}

// liveAdminSessions are the admins' sessions that still work: not expired
// or idle, and made under one of keys.
func (s *Store) liveAdminSessions(ctx context.Context, admins []AdminAccount, current string, keys AdminKeys) ([]AdminAccountSession, error) {
	ids := make([]int64, len(admins))
	for i, a := range admins {
		ids[i] = a.ID
	}
	var sessions []AdminAccountSession
	err := s.DB.NewSelect().Model((*adminSessionRecord)(nil)).
		ColumnExpr("adms.user_id, adms.created_at, adms.last_seen_at, host(adms.ip) AS ip, adms.user_agent").
		ColumnExpr("adms.token_hash = ? AS current", current).
		ColumnExpr("adms.key_hash = ? AS previous_key", keys.Previous).
		Where("adms.user_id IN (?) AND adms.key_hash IN (?)", bun.In(ids), bun.In(keys.hashes())).
		Where("adms.expires_at > now() AND adms.last_seen_at > now() - make_interval(secs => ?)", adminIdleSeconds).
		OrderExpr("adms.user_id, adms.created_at DESC").
		Scan(ctx, &sessions)
	return sessions, err
}
