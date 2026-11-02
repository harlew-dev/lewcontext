package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// "Download my data" (docs/compliance.md#download-my-data): everything the
// app keeps about an account that the account may see, as one JSON
// document. Each section is a type of its own, holding only the fields
// listed, so nothing is exported by accident: never a password hash, an
// MFA secret or recovery code, a token or its digest, a CSRF token or an
// internal ID, and nobody else's data beyond what the account itself
// typed (the addresses it invited, its own searches as an admin aren't
// exported at all).

// MaxExportedEvents is the most events an export holds, the newest first;
// past it, EventsTruncated says so.
const MaxExportedEvents = 10_000

// AccountExport is the document.
type AccountExport struct {
	ExportedAt         time.Time                `json:"exportedAt"`
	Account            ExportedAccount          `json:"account"`
	Sessions           []ExportedSession        `json:"sessions"`
	Memberships        []ExportedMembership     `json:"memberships"`
	OwnedWorkspaces    []ExportedOwnedWorkspace `json:"ownedWorkspaces"`
	Events             []ExportedEvent          `json:"events"`
	EventsTruncated    bool                     `json:"eventsTruncated"`
	PendingEmailChange *ExportedEmailChange     `json:"pendingEmailChange"`
	FormerEmails       []ExportedFormerEmail    `json:"formerEmails"`
}

// ExportedEmailChange deliberately omits session, code and request digests.
type ExportedEmailChange struct {
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ExportedFormerEmail is the account's retained erasure history, not an alias.
type ExportedFormerEmail struct {
	Email      string    `json:"email"`
	ReleasedAt time.Time `json:"releasedAt"`
}

// ExportedAccount is the account's profile.
type ExportedAccount struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Email           string     `json:"email"`
	CreatedAt       time.Time  `json:"createdAt"`
	EmailVerifiedAt *time.Time `json:"emailVerifiedAt"`
	LastSeenAt      *time.Time `json:"lastSeenAt"`
	// MFAEnabledAt says two-factor authentication is on, and since when;
	// never its secret or recovery codes.
	MFAEnabledAt *time.Time `json:"mfaEnabledAt"`
}

// ExportedSession is a live sign-in: when, and until when. Never its token.
type ExportedSession struct {
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

// ExportedMembership is a workspace the account belongs to, with its role.
type ExportedMembership struct {
	Workspace string    `json:"workspace"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	JoinedAt  time.Time `json:"joinedAt"`
}

// ExportedOwnedWorkspace is a workspace the account owns: its projects,
// and the live invitations the account sent from it.
type ExportedOwnedWorkspace struct {
	Workspace   string               `json:"workspace"`
	Name        string               `json:"name"`
	CreatedAt   time.Time            `json:"createdAt"`
	Projects    []ExportedProject    `json:"projects"`
	Invitations []ExportedInvitation `json:"invitations"`
}

// ExportedProject is a project in an owned workspace.
type ExportedProject struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Color       string    `json:"color"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ExportedInvitation is a live invitation the account sent: the address
// it typed, the role and when it ends. Never its link.
type ExportedInvitation struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// ExportedEvent is an event about the account, or one it caused, as
// stored (within the events' retention), apart from an operator's note: a
// suspension's reason from the command line (exportedPayload). IP and UserAgent are the
// account's own request's: shown only for events it caused, never for
// one someone else caused about it (a member's change of its role, a
// sign-in attempt on its email).
type ExportedEvent struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	CreatedAt time.Time       `json:"createdAt"`
	Payload   json.RawMessage `json:"payload"`
	IP        string          `json:"ip,omitempty"`
	UserAgent string          `json:"userAgent,omitempty"`
}

// FindAccountExport gathers the export of the account userID, read in one
// repeatable-read transaction so its sections agree.
func (s *Store) FindAccountExport(ctx context.Context, userID int64) (AccountExport, error) {
	out := AccountExport{ExportedAt: time.Now().UTC()}
	err := s.DB.RunInTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true}, func(ctx context.Context, tx bun.Tx) error {
		var err error
		if out.Account, err = exportAccount(ctx, tx, userID); err != nil {
			return err
		}
		if out.Sessions, err = exportSessions(ctx, tx, userID); err != nil {
			return err
		}
		if out.Memberships, err = exportMemberships(ctx, tx, userID); err != nil {
			return err
		}
		if out.OwnedWorkspaces, err = exportOwnedWorkspaces(ctx, tx, userID); err != nil {
			return err
		}
		var pending ExportedEmailChange
		err = tx.NewSelect().Model((*emailChange)(nil)).Column("email", "expires_at").Where("user_id = ? AND expires_at > now()", userID).Scan(ctx, &pending)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == nil {
			out.PendingEmailChange = &pending
		}
		out.FormerEmails = []ExportedFormerEmail{}
		if err := tx.NewSelect().Model((*userEmailHistory)(nil)).Column("email", "released_at").Where("user_id = ?", userID).Order("released_at").Scan(ctx, &out.FormerEmails); err != nil {
			return err
		}
		out.Events, out.EventsTruncated, err = exportEvents(ctx, tx, userID)
		return err
	})
	return out, err
}

func exportAccount(ctx context.Context, tx bun.Tx, userID int64) (ExportedAccount, error) {
	var a ExportedAccount
	err := tx.NewSelect().Model((*User)(nil)).
		Column("u.public_id", "u.name", "u.email", "u.created_at", "u.email_verified_at", "u.last_seen_at").
		ColumnExpr("(?)", mfaEnabledAt(tx)).
		Where("u.id = ?", userID).
		Scan(ctx, &a.ID, &a.Name, &a.Email, &a.CreatedAt, &a.EmailVerifiedAt, &a.LastSeenAt, &a.MFAEnabledAt)
	return a, err
}

func exportSessions(ctx context.Context, tx bun.Tx, userID int64) ([]ExportedSession, error) {
	sessions := []ExportedSession{}
	err := tx.NewSelect().Model((*sessionRecord)(nil)).
		ColumnExpr("s.created_at, s.last_seen_at, s.expires_at").
		Where("s.user_id = ? AND s.expires_at > now()", userID).
		OrderExpr("s.created_at DESC").
		Scan(ctx, &sessions)
	return sessions, err
}

func exportMemberships(ctx context.Context, tx bun.Tx, userID int64) ([]ExportedMembership, error) {
	memberships := []ExportedMembership{}
	err := tx.NewSelect().Model((*membership)(nil)).
		ColumnExpr("w.public_id AS workspace, w.name, m.role, m.created_at AS joined_at").
		Join("JOIN workspaces AS w ON w.id = m.workspace_id").
		Where("m.user_id = ?", userID).
		OrderExpr("m.created_at, w.id").
		Scan(ctx, &memberships)
	return memberships, err
}

// ownedWorkspace is an owned workspace's row, with its internal ID for the
// queries that follow (never exported).
type ownedWorkspace struct {
	ID int64
	ExportedOwnedWorkspace
}

func exportOwnedWorkspaces(ctx context.Context, tx bun.Tx, userID int64) ([]ExportedOwnedWorkspace, error) {
	var rows []ownedWorkspace
	err := tx.NewSelect().Model((*Workspace)(nil)).
		ColumnExpr("w.id, w.public_id AS workspace, w.name, w.created_at").
		Join("JOIN workspace_user AS m ON m.workspace_id = w.id").
		Where("m.user_id = ? AND m.role = ?", userID, auth.RoleOwner).
		OrderExpr("w.id").
		Scan(ctx, &rows)
	if err != nil {
		return nil, err
	}
	owned := make([]ExportedOwnedWorkspace, 0, len(rows))
	for _, row := range rows {
		w := row.ExportedOwnedWorkspace
		w.Projects = []ExportedProject{}
		err := tx.NewSelect().Model((*Project)(nil)).
			ColumnExpr("p.public_id AS id, p.name, p.description, p.status, p.color, p.created_at, p.updated_at").
			Where("p.workspace_id = ?", row.ID).
			OrderExpr("p.id").
			Scan(ctx, &w.Projects)
		if err != nil {
			return nil, err
		}
		w.Invitations = []ExportedInvitation{}
		err = tx.NewSelect().Model((*invitationRecord)(nil)).
			ColumnExpr("i.public_id AS id, i.email, i.role, i.created_at, i.expires_at").
			Where("i.workspace_id = ? AND i.invited_by = ? AND i.expires_at > now()", row.ID, userID).
			OrderExpr("i.id").
			Scan(ctx, &w.Invitations)
		if err != nil {
			return nil, err
		}
		owned = append(owned, w)
	}
	return owned, nil
}

// exportedPayload is an event's payload as the export shows it: as stored,
// apart from the reason an operator typed when suspending or unsuspending
// the account from the command line (auth.suspended's and
// auth.unsuspended's reason, by cli), which is the operator's note, not
// the account's data; who did it (by) stays. From the dashboard, the
// reason is in the admin.* event, never exported.
var exportedPayload = bun.SafeQuery("CASE WHEN e.name IN (?) THEN e.payload - 'reason' ELSE e.payload END",
	bun.In([]string{events.Suspended{}.EventName(), events.Unsuspended{}.EventName()}))

// exportEvents are the events about the account (user_id), and those it
// caused (actor_id) apart from its admin dashboard's: an admin's reads and
// actions hold other people's data (a stranger's invitations it revoked,
// an address it unblocked, an account it suspended) and the dashboard's
// searches, and are the admin trail's, not the account's. Those are every
// event recorded in an admin request (admin_request, set from the context
// whatever the event's name, so a new action is covered) and every admin.*
// name. Newest first, at most MaxExportedEvents.
func exportEvents(ctx context.Context, tx bun.Tx, userID int64) ([]ExportedEvent, bool, error) {
	var rows []struct {
		ExportedEvent
		Own bool
	}
	err := tx.NewSelect().Model((*eventRecord)(nil)).
		ColumnExpr("e.public_id AS id, e.name, e.created_at").
		ColumnExpr("? AS payload", exportedPayload).
		ColumnExpr("coalesce(?, '') AS ip, e.user_agent", eventIPText).
		ColumnExpr("e.actor_id IS NOT DISTINCT FROM ? AS own", userID).
		Where("e.user_id = ? OR (e.actor_id = ? AND NOT e.admin_request AND split_part(e.name, '.', 1) <> 'admin')", userID, userID).
		Where("e.name <> ?", events.EmailChangeSent{}.EventName()).
		OrderExpr(newestFirst).
		Limit(MaxExportedEvents+1).
		Scan(ctx, &rows)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > MaxExportedEvents
	if truncated {
		rows = rows[:MaxExportedEvents]
	}
	out := make([]ExportedEvent, len(rows))
	for i, row := range rows {
		out[i] = row.ExportedEvent
		if !row.Own {
			out[i].IP, out[i].UserAgent = "", ""
		}
	}
	return out, truncated, nil
}
