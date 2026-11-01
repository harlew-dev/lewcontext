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
	Role          auth.AdminRole `bun:",notnull"`
	// AlertEmail is which alerts the admin is emailed (AlertEmailAll,
	// AlertEmailCritical, the default, or AlertEmailNone).
	AlertEmail string    `bun:",nullzero,notnull,default:'critical'"`
	CreatedAt  time.Time `bun:",nullzero,notnull,default:current_timestamp"`
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
	IP            string    `bun:"ip,type:inet,notnull"`
	UserAgent     string    `bun:",notnull"`
	MovedAt       *time.Time
	// SteppedUpUntil ends the step-up window a fresh code opened
	// (StepUpAdminSession); nil when it never did.
	SteppedUpUntil *time.Time
	// StepUpFailures counts its wrong step-up codes; the fifth ends it.
	StepUpFailures int `bun:",notnull"`
}

// Admin is an admin account. MFASetupUntil is when their setup window at
// the door closes, or nil.
type Admin struct {
	User          User
	MFASetupUntil *time.Time
	Role          auth.AdminRole
	CreatedAt     time.Time
}

// SetupOpen reports whether the admin may set up MFA at the door now.
func (a Admin) SetupOpen() bool {
	return a.User.MFAEnabledAt == nil && a.MFASetupUntil != nil && time.Now().Before(*a.MFASetupUntil)
}

// AdminSession is a live admin session and its admin. KeyHash is the digest
// of the key it was made under, for the caller to check against the key the
// request came with; IP is where it signed in, and Moved whether it has
// been used from another network since (admin.session.moved). SteppedUp is
// whether its step-up window is open, by the database's clock as its expiry
// is (httpapi.RequireAdminStepUp).
type AdminSession struct {
	User                 User
	Role                 auth.AdminRole
	CSRF                 string
	KeyHash              string
	IP                   string
	Moved                bool
	SteppedUp            bool
	CreatedAt, ExpiresAt time.Time
}

// NewAdminSession is an admin session to start: the token's and the key's
// digests, its CSRF token and end, and where it signed in from (the client
// IP and its User-Agent, already cut to 256 characters).
type NewAdminSession struct {
	UserID                   int64
	TokenHash, KeyHash, CSRF string
	ExpiresAt                time.Time
	IP, UserAgent            string
}

// scanDest is where a row of adminsWith goes, in its order, then extra.
func (a *Admin) scanDest(extra ...any) []any {
	return append([]any{&a.User.ID, &a.User.PublicID, &a.User.Name, &a.User.Email, &a.User.CreatedAt, &a.User.MFAEnabledAt, &a.MFASetupUntil, &a.Role, &a.CreatedAt}, extra...)
}

// adminsWith selects admins with their accounts (MFAEnabledAt included),
// in Admin.scanDest's order.
func adminsWith(db bun.IDB) *bun.SelectQuery {
	return db.NewSelect().Model((*adminRecord)(nil)).
		ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at").
		ColumnExpr("(?)", mfaEnabledAt(db)).
		ColumnExpr("adm.mfa_setup_until, adm.role, adm.created_at").
		Join("JOIN users AS u ON u.id = adm.user_id")
}

func scanAdmin(ctx context.Context, q *bun.SelectQuery) (Admin, error) {
	var a Admin
	err := q.Scan(ctx, a.scanDest()...)
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
		if err := rows.Scan(a.scanDest()...); err != nil {
			return nil, err
		}
		admins = append(admins, a)
	}
	return admins, rows.Err()
}

// GrantAdmin makes the account with email an admin with role (a viewer when
// role is ""), or grants it again. A new grant, or one again with the same
// role (or none), records admin.granted with no actor (the operator's CLI)
// and, without MFA, opens a new 24-hour setup window at the door. Granting
// an existing admin another role only changes the role, recording
// admin.updated; from is then the role it had ("" otherwise).
// sql.ErrNoRows for an unknown email, ErrSuspended for a suspended account.
func (s *Store) GrantAdmin(ctx context.Context, email string, role auth.AdminRole) (a Admin, from auth.AdminRole, err error) {
	err = s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var u User
		err := tx.NewSelect().Model(&u).ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at").ColumnExpr("(?) AS mfa_enabled_at", mfaEnabledAt(tx)).
			Where("u.email = ?", email).For("UPDATE OF u").Scan(ctx)
		if err != nil {
			return err
		}
		// A suspended account is never an admin: unsuspend it first.
		if err := checkNotSuspended(ctx, tx, u.ID); err != nil {
			return err
		}
		var existing adminRecord
		err = tx.NewSelect().Model(&existing).Where("user_id = ?", u.ID).For("UPDATE").Scan(ctx)
		known := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if known && role != "" && role != existing.Role {
			a, from = Admin{User: u, MFASetupUntil: existing.MFASetupUntil, Role: role, CreatedAt: existing.CreatedAt}, existing.Role
			return s.changeAdminRole(ctx, tx, u, existing.Role, role)
		}
		if role == "" {
			role = auth.AdminViewer
			if known {
				role = existing.Role
			}
		}
		a, err = s.grantAdmin(ctx, tx, u, role)
		return err
	})
	return a, from, err
}

// grantAdmin inserts (or refreshes) the admin row of u with role, opening
// the setup window without MFA, and records admin.granted.
func (s *Store) grantAdmin(ctx context.Context, tx bun.Tx, u User, role auth.AdminRole) (Admin, error) {
	row := adminRecord{UserID: u.ID, Role: role}
	if u.MFAEnabledAt == nil {
		until := time.Now().Add(auth.AdminSetupWindow)
		row.MFASetupUntil = &until
	}
	_, err := tx.NewInsert().Model(&row).
		On("CONFLICT (user_id) DO UPDATE").
		Set("mfa_setup_until = EXCLUDED.mfa_setup_until").
		Returning("created_at").
		Exec(ctx)
	if err != nil {
		return Admin{}, err
	}
	e := events.AdminGranted{User: u.PublicID, Role: string(role), MFASetupUntil: row.MFASetupUntil}
	return Admin{User: u, MFASetupUntil: row.MFASetupUntil, Role: role, CreatedAt: row.CreatedAt}, s.record(ctx, tx, e, EventScope{UserID: u.ID})
}

// changeAdminRole gives the admin u role instead of from and records
// admin.updated. The setup window is left as it was. The admin's sessions
// carry on with the new role from their next request, but their step-up
// windows close: an action under the new role needs a fresh code.
func (s *Store) changeAdminRole(ctx context.Context, tx bun.Tx, u User, from, role auth.AdminRole) error {
	if _, err := tx.NewUpdate().Model((*adminRecord)(nil)).Set("role = ?", role).Where("user_id = ?", u.ID).Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.NewUpdate().Model((*adminSessionRecord)(nil)).Set("stepped_up_until = NULL").Where("user_id = ?", u.ID).Exec(ctx); err != nil {
		return err
	}
	changes := events.Changes{}
	changes.Add("role", string(from), string(role))
	return s.record(ctx, tx, events.AdminUpdated{User: u.PublicID, Changes: changes}, EventScope{UserID: u.ID})
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
	err := q.Scan(ctx, a.scanDest(&purpose)...)
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

// CreateAdminSession starts the admin session n, keeping at most
// MaxAdminSessions per admin (the oldest go), and records e with the admin
// as actor. ErrNotAdmin if they stopped being one, ErrSuspended for a
// suspended account (never an admin; the backstop).
func (s *Store) CreateAdminSession(ctx context.Context, n NewAdminSession, e events.Event) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		// The admin row's lock serialises sign-ins, so the cap holds.
		var locked int64
		err := tx.NewSelect().Model((*adminRecord)(nil)).Column("user_id").Where("user_id = ?", n.UserID).For("UPDATE").Scan(ctx, &locked)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotAdmin
		}
		if err != nil {
			return err
		}
		if err := checkNotSuspended(ctx, tx, n.UserID); err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*adminSessionRecord)(nil)).
			Where("user_id = ? AND (expires_at <= now() OR last_seen_at <= now() - make_interval(secs => ?))", n.UserID, adminIdleSeconds).
			Exec(ctx); err != nil {
			return err
		}
		newest := tx.NewSelect().Model((*adminSessionRecord)(nil)).Column("token_hash").
			Where("user_id = ?", n.UserID).
			Order("created_at DESC").
			Offset(MaxAdminSessions - 1)
		if _, err := tx.NewDelete().Model((*adminSessionRecord)(nil)).Where("token_hash IN (?)", newest).Exec(ctx); err != nil {
			return err
		}
		session := adminSessionRecord{TokenHash: n.TokenHash, UserID: n.UserID, KeyHash: n.KeyHash, CSRFToken: n.CSRF, ExpiresAt: n.ExpiresAt, IP: n.IP, UserAgent: n.UserAgent}
		if _, err := tx.NewInsert().Model(&session).Exec(ctx); err != nil {
			return err
		}
		return s.record(ctx, tx, e, EventScope{ActorID: n.UserID})
	})
}

// adminIdleSeconds is auth.AdminSessionIdle, for make_interval.
var adminIdleSeconds = int(auth.AdminSessionIdle.Seconds())

// FindAdminSession refreshes the idle time of the admin session hash made
// under the key whose digest is keyHash (the one the request came with),
// and returns it with its admin and that digest. sql.ErrNoRows when it's
// unknown, expired, idle, made under another key, or its user is no longer
// an admin (or is suspended: never an admin, but this is the backstop). A
// session made under another key is never touched, so a
// request with the wrong key can't keep it from idling out.
func (s *Store) FindAdminSession(ctx context.Context, hash, keyHash string) (AdminSession, error) {
	active := s.DB.NewUpdate().Model((*adminSessionRecord)(nil)).
		Set("last_seen_at = now()").
		Where("token_hash = ? AND key_hash = ? AND expires_at > now() AND last_seen_at > now() - make_interval(secs => ?)", hash, keyHash, adminIdleSeconds).
		Where("NOT EXISTS (?)", suspendedUser(s.DB, "adms.user_id")).
		Returning("user_id, csrf_token, key_hash, host(ip) AS ip, moved_at IS NOT NULL AS moved, coalesce(stepped_up_until > now(), false) AS stepped_up, created_at, expires_at")
	var session AdminSession
	err := s.DB.NewSelect().
		With("active", active).
		TableExpr("active AS a").
		Join("JOIN admins AS adm ON adm.user_id = a.user_id").
		Join("JOIN users AS u ON u.id = a.user_id").
		ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at, adm.role, a.csrf_token, a.key_hash, a.ip, a.moved, a.stepped_up, a.created_at, a.expires_at").
		ColumnExpr("(?)", mfaEnabledAt(s.DB)).
		Scan(ctx, &session.User.ID, &session.User.PublicID, &session.User.Name, &session.User.Email, &session.User.CreatedAt, &session.Role, &session.CSRF, &session.KeyHash, &session.IP, &session.Moved, &session.SteppedUp, &session.CreatedAt, &session.ExpiresAt, &session.User.MFAEnabledAt)
	return session, err
}

// CheckAdminSession reports whether hash is a live admin session made under
// the key whose digest is keyHash, without refreshing it: telling a session
// made under the other key (401) from none at all.
func (s *Store) CheckAdminSession(ctx context.Context, hash, keyHash string) (bool, error) {
	return s.DB.NewSelect().Model((*adminSessionRecord)(nil)).
		Join("JOIN admins AS adm ON adm.user_id = adms.user_id").
		Where("adms.token_hash = ? AND adms.key_hash = ? AND adms.expires_at > now() AND adms.last_seen_at > now() - make_interval(secs => ?)", hash, keyHash, adminIdleSeconds).
		Exists(ctx)
}

// FlagAdminSessionMoved marks the admin session hash as used from another
// network than it signed in from, and records e (admin.session.moved) with
// the admin as actor, only the first time: a session is flagged once.
func (s *Store) FlagAdminSessionMoved(ctx context.Context, hash string, admin User, e events.AdminSessionMoved) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewUpdate().Model((*adminSessionRecord)(nil)).
			Set("moved_at = now()").
			Where("token_hash = ? AND moved_at IS NULL", hash).
			Exec(ctx)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n == 0 {
			return err
		}
		return s.record(ctx, tx, e, EventScope{ActorID: admin.ID})
	})
}

// adminStepUpSeconds is auth.AdminStepUpWindow, for make_interval.
var adminStepUpSeconds = int(auth.AdminStepUpWindow.Seconds())

// MaxStepUpFailures is how many wrong step-up codes end an admin session,
// as MaxChallengeAttempts end a sign-in's challenge: a stolen admin cookie
// gets one burst of guesses (which also spend the account's
// mfa:per-account), never a way to keep the real admin locked out.
const MaxStepUpFailures = 5

// StepUpResult is how a step-up went: OK (the window is open), or a wrong
// code, Ended when it was the session's MaxStepUpFailures-th and the
// session is gone.
type StepUpResult struct {
	OK    bool
	Ended bool
	FactorUsed
}

// StepUpAdminSession opens the step-up window of the live admin session
// hash, of admin's, for auth.AdminStepUpWindow from now (the database's
// clock), once the second factor f checks out. The code is checked and used
// up exactly as at the door's second step (useConfirmedFactor, under the
// authenticator's row lock): an authenticator code only for a step after
// the last one used, a recovery code once. A wrong code (or none: MFA off)
// counts against the session; the MaxStepUpFailures-th deletes it and
// records admin.session.ended. A right code doesn't reset the count, so a
// session gets MaxStepUpFailures wrong codes in its whole life.
// sql.ErrNoRows when the session ended meanwhile (nothing is used up).
func (s *Store) StepUpAdminSession(ctx context.Context, hash string, admin User, f SecondFactor) (StepUpResult, error) {
	var result StepUpResult
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result = StepUpResult{}
		factor, err := useConfirmedFactor(ctx, tx, admin.ID, f)
		if errors.Is(err, ErrWrongCode) || errors.Is(err, ErrMFANotEnabled) {
			result.Ended, err = s.failStepUp(ctx, tx, hash, admin)
			return err
		}
		if err != nil {
			return err
		}
		res, err := tx.NewUpdate().Model((*adminSessionRecord)(nil)).
			Set("stepped_up_until = now() + make_interval(secs => ?)", adminStepUpSeconds).
			Where("token_hash = ? AND user_id = ?", hash, admin.ID).
			Where(liveAdminSession, adminIdleSeconds).
			Exec(ctx)
		if err != nil {
			return err
		}
		if err := oneRow(res); err != nil {
			return err
		}
		result.OK, result.FactorUsed = true, *factor
		return nil
	})
	return result, err
}

// liveAdminSession is the condition for an admin session that hasn't ended:
// not expired, nor idle (its argument is adminIdleSeconds).
const liveAdminSession = "expires_at > now() AND last_seen_at > now() - make_interval(secs => ?)"

// failStepUp counts a wrong step-up code against the live session hash,
// and at MaxStepUpFailures deletes it and records admin.session.ended,
// reporting whether it ended. sql.ErrNoRows when it already had.
func (s *Store) failStepUp(ctx context.Context, tx bun.Tx, hash string, admin User) (bool, error) {
	var failures int
	err := tx.NewUpdate().Model((*adminSessionRecord)(nil)).
		Set("step_up_failures = step_up_failures + 1").
		Where("token_hash = ? AND user_id = ?", hash, admin.ID).
		Where(liveAdminSession, adminIdleSeconds).
		Returning("step_up_failures").
		Scan(ctx, &failures)
	if err != nil || failures < MaxStepUpFailures {
		return false, err
	}
	if _, err := tx.NewDelete().Model((*adminSessionRecord)(nil)).Where("token_hash = ?", hash).Exec(ctx); err != nil {
		return false, err
	}
	e := events.AdminSessionEnded{Admin: admin.PublicID, Reason: events.AdminSessionEndedStepUpFailures}
	return true, s.record(ctx, tx, e, EventScope{ActorID: admin.ID})
}

// oneRow is sql.ErrNoRows unless res changed exactly one row.
func oneRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		err = sql.ErrNoRows
	}
	return err
}

// DeleteOtherAdminSessions signs out every admin session of the admin's but
// hash (this one), and records admin.sessions_revoked with how many.
func (s *Store) DeleteOtherAdminSessions(ctx context.Context, admin User, hash string) (int, error) {
	var count int
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		result, err := tx.NewDelete().Model((*adminSessionRecord)(nil)).Where("user_id = ? AND token_hash <> ?", admin.ID, hash).Exec(ctx)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		count = int(n)
		return s.record(ctx, tx, events.AdminSessionsRevoked{Admin: admin.PublicID, Count: count}, EventScope{ActorID: admin.ID})
	})
	return count, err
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
