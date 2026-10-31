package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// Admins are user accounts the operator granted admin from the command line
// (cmd/admin), and their admin sessions, which are separate from the app's
// (docs/api/admin.md).

var (
	// ErrNotAdmin is an account that isn't an admin (any more).
	ErrNotAdmin = errors.New("not an admin")
	// ErrAdminNeedsMFA refuses turning MFA off for an admin.
	ErrAdminNeedsMFA = errors.New("admins must keep two-factor authentication on")
)

// MaxAdminSessions is how many admin sessions one admin may have at once.
const MaxAdminSessions = 2

// Challenge purposes of the admin door: an admin's sign-in waiting for a
// code, and an admin without MFA setting it up, inside the setup window.
const (
	ChallengeAdmin      = "admin"
	ChallengeAdminSetup = "admin_setup"
)

type adminRecord struct {
	bun.BaseModel `bun:"table:admins,alias:adm"`
	UserID        int64 `bun:",pk"`
	MFASetupUntil *time.Time
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}
type adminSessionRecord struct {
	bun.BaseModel `bun:"table:admin_sessions,alias:adms"`
	TokenHash     string    `bun:",pk"`
	UserID        int64     `bun:",notnull"`
	KeyHash       string    `bun:",notnull"`
	CSRFToken     string    `bun:"csrf_token,notnull"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	LastSeenAt    time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

// Admin is an admin account. MFASetupUntil is when their setup window at
// the door closes, or nil.
type Admin struct {
	User          User
	MFASetupUntil *time.Time
	CreatedAt     time.Time
}

// SetupOpen reports whether the admin may set up MFA at the door now.
func (a Admin) SetupOpen() bool {
	return a.User.MFAEnabledAt == nil && a.MFASetupUntil != nil && time.Now().Before(*a.MFASetupUntil)
}

// AdminSession is a live admin session and its admin.
type AdminSession struct {
	User                 User
	CSRF                 string
	CreatedAt, ExpiresAt time.Time
}

// adminsWith selects admins with their accounts (MFAEnabledAt included).
func adminsWith(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().Model((*adminRecord)(nil)).
		ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at").
		ColumnExpr("(?)", mfaEnabledAt(db)).
		ColumnExpr("adm.mfa_setup_until, adm.created_at").
		Join("JOIN users AS u ON u.id = adm.user_id")
}

func scanAdmin(ctx context.Context, q *bun.SelectQuery) (Admin, error) {
	var a Admin
	err := q.Scan(ctx, &a.User.ID, &a.User.PublicID, &a.User.Name, &a.User.Email, &a.User.CreatedAt, &a.User.MFAEnabledAt, &a.MFASetupUntil, &a.CreatedAt)
	return a, err
}

// FindAdmin returns the admin with the user's internal ID, or ErrNotAdmin.
func (s *Store) FindAdmin(ctx context.Context, userID int64) (Admin, error) {
	a, err := scanAdmin(ctx, adminsWith(s.DB).Where("adm.user_id = ?", userID))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotAdmin
	}
	return a, err
}

// IndexAdmins lists every admin, oldest grant first, for admin list.
func (s *Store) IndexAdmins(ctx context.Context) ([]Admin, error) {
	rows, err := adminsWith(s.DB).OrderExpr("adm.created_at, adm.user_id").Rows(ctx)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	admins := []Admin{}
	for rows.Next() {
		var a Admin
		if err := rows.Scan(&a.User.ID, &a.User.PublicID, &a.User.Name, &a.User.Email, &a.User.CreatedAt, &a.User.MFAEnabledAt, &a.MFASetupUntil, &a.CreatedAt); err != nil {
			return nil, err
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}

// GrantAdmin makes the account with email an admin, or grants it again,
// and records admin.granted with no actor (the operator's CLI). Without
// MFA, it opens a new 24-hour setup window at the door. sql.ErrNoRows for an
// unknown email.
func (s *Store) GrantAdmin(ctx context.Context, email string) (Admin, error) {
	var a Admin
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var u User
		err := tx.NewSelect().Model(&u).ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at").ColumnExpr("(?) AS mfa_enabled_at", mfaEnabledAt(tx)).
			Where("u.email = ?", email).For("UPDATE OF u").Scan(ctx)
		if err != nil {
			return err
		}
		row := adminRecord{UserID: u.ID}
		if u.MFAEnabledAt == nil {
			until := time.Now().Add(auth.AdminSetupWindow)
			row.MFASetupUntil = &until
		}
		_, err = tx.NewInsert().Model(&row).
			On("CONFLICT (user_id) DO UPDATE").
			Set("mfa_setup_until = EXCLUDED.mfa_setup_until").
			Returning("created_at").
			Exec(ctx)
		if err != nil {
			return err
		}
		a = Admin{User: u, MFASetupUntil: row.MFASetupUntil, CreatedAt: row.CreatedAt}
		e := events.AdminGranted{User: u.PublicID, MFASetupUntil: row.MFASetupUntil}
		return s.record(ctx, tx, e, EventScope{UserID: u.ID})
	})
	return a, err
}

// RevokeAdmin takes admin away from the account with email, deleting its
// admin sessions, and records admin.revoked with no actor. sql.ErrNoRows for
// an unknown email, ErrNotAdmin for an account that isn't one.
func (s *Store) RevokeAdmin(ctx context.Context, email string) (User, int, error) {
	var u User
	var revoked int
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := tx.NewSelect().Model(&u).Where("u.email = ?", email).For("UPDATE").Scan(ctx); err != nil {
			return err
		}
		var err error
		if revoked, err = deleteAdminSessions(ctx, tx, u.ID); err != nil {
			return err
		}
		result, err := tx.NewDelete().Model((*adminRecord)(nil)).Where("user_id = ?", u.ID).Exec(ctx)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			if err == nil {
				err = ErrNotAdmin
			}
			return err
		}
		return s.record(ctx, tx, events.AdminRevoked{User: u.PublicID, AdminSessionsRevoked: revoked}, EventScope{UserID: u.ID})
	})
	return u, revoked, err
}

// FindAdminChallenge returns the user of a live admin door challenge and
// its purpose (ChallengeAdmin or ChallengeAdminSetup), only while the user
// is still an admin. ErrNoChallenge otherwise.
func (s *Store) FindAdminChallenge(ctx context.Context, hash string) (Admin, string, error) {
	var purpose string
	q := adminsWith(s.DB).
		Join("JOIN mfa_challenges AS c ON c.user_id = adm.user_id").
		ColumnExpr("c.purpose").
		Where("c.token_hash = ? AND c.purpose IN (?, ?) AND c.expires_at > now()", hash, ChallengeAdmin, ChallengeAdminSetup)
	var a Admin
	err := q.Scan(ctx, &a.User.ID, &a.User.PublicID, &a.User.Name, &a.User.Email, &a.User.CreatedAt, &a.User.MFAEnabledAt, &a.MFASetupUntil, &a.CreatedAt, &purpose)
	if errors.Is(err, sql.ErrNoRows) {
		return a, "", ErrNoChallenge
	}
	return a, purpose, err
}

// DeleteMFAChallenge ends a sign-in challenge.
func (s *Store) DeleteMFAChallenge(ctx context.Context, hash string) error {
	_, err := s.DB.NewDelete().Model((*mfaChallenge)(nil)).Where("token_hash = ?", hash).Exec(ctx)
	return err
}

// CreateAdminSession starts an admin session under the key whose digest is
// keyHash, keeping at most MaxAdminSessions per admin (the oldest go), and
// records e with the admin as actor. ErrNotAdmin if they stopped being one.
func (s *Store) CreateAdminSession(ctx context.Context, userID int64, hash, keyHash, csrf string, expiry time.Time, e events.Event) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// The admin row's lock serialises sign-ins, so the cap holds.
		var locked int64
		err := tx.NewSelect().Model((*adminRecord)(nil)).Column("user_id").Where("user_id = ?", userID).For("UPDATE").Scan(ctx, &locked)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotAdmin
		}
		if err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*adminSessionRecord)(nil)).
			Where("user_id = ? AND (expires_at <= now() OR last_seen_at <= now() - make_interval(secs => ?))", userID, adminIdleSeconds).
			Exec(ctx); err != nil {
			return err
		}
		newest := tx.NewSelect().Model((*adminSessionRecord)(nil)).Column("token_hash").
			Where("user_id = ?", userID).
			Order("created_at DESC").
			Offset(MaxAdminSessions - 1)
		if _, err := tx.NewDelete().Model((*adminSessionRecord)(nil)).Where("token_hash IN (?)", newest).Exec(ctx); err != nil {
			return err
		}
		session := adminSessionRecord{TokenHash: hash, UserID: userID, KeyHash: keyHash, CSRFToken: csrf, ExpiresAt: expiry}
		if _, err := tx.NewInsert().Model(&session).Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, e, EventScope{ActorID: userID})
	})
}

// adminIdleSeconds is auth.AdminSessionIdle, for make_interval.
var adminIdleSeconds = int(auth.AdminSessionIdle.Seconds())

// FindAdminSession refreshes the idle time of the admin session hash created
// under the key whose digest is keyHash, and returns it with its admin.
// sql.ErrNoRows when it's unknown, expired, idle, made under another key, or
// its user is no longer an admin.
func (s *Store) FindAdminSession(ctx context.Context, hash, keyHash string) (AdminSession, error) {
	active := s.DB.NewUpdate().Model((*adminSessionRecord)(nil)).
		Set("last_seen_at = now()").
		Where("token_hash = ? AND key_hash = ? AND expires_at > now() AND last_seen_at > now() - make_interval(secs => ?)", hash, keyHash, adminIdleSeconds).
		Returning("user_id, csrf_token, created_at, expires_at")
	var session AdminSession
	err := s.DB.NewSelect().
		With("active", active).
		TableExpr("active AS a").
		Join("JOIN admins AS adm ON adm.user_id = a.user_id").
		Join("JOIN users AS u ON u.id = a.user_id").
		ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at, a.csrf_token, a.created_at, a.expires_at").
		ColumnExpr("(?)", mfaEnabledAt(s.DB)).
		Scan(ctx, &session.User.ID, &session.User.PublicID, &session.User.Name, &session.User.Email, &session.User.CreatedAt, &session.CSRF, &session.CreatedAt, &session.ExpiresAt, &session.User.MFAEnabledAt)
	return session, err
}

// DeleteAdminSession signs an admin session out and records
// admin.logged_out.
func (s *Store) DeleteAdminSession(ctx context.Context, u User, hash string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewDelete().Model((*adminSessionRecord)(nil)).Where("token_hash = ?", hash).Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, events.AdminLoggedOut{User: u.PublicID}, EventScope{ActorID: u.ID})
	})
}

// deleteAdminSessions deletes every admin session of the user's and counts
// them: losing admin, a new password and an MFA reset end them all.
func deleteAdminSessions(ctx context.Context, db bun.IDB, userID int64) (int, error) {
	result, err := db.NewDelete().Model((*adminSessionRecord)(nil)).Where("user_id = ?", userID).Exec(ctx)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	return int(n), err
}

// isAdmin reports whether the user is an admin, inside tx.
func isAdmin(ctx context.Context, db bun.IDB, userID int64) (bool, error) {
	return db.NewSelect().Model((*adminRecord)(nil)).Where("user_id = ?", userID).Exists(ctx)
}
