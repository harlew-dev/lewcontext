package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// InvitationLifetime is how long an invitation, and its emailed link, works.
const InvitationLifetime = 7 * 24 * time.Hour

// MaxPendingInvitations is how many live invitations a workspace may have.
const MaxPendingInvitations = 50

var (
	// ErrAlreadyMember refuses inviting someone already in the workspace.
	ErrAlreadyMember = errors.New("already a member")
	// ErrAlreadyInvited refuses a second live invitation to the same email.
	ErrAlreadyInvited = errors.New("already invited")
	// ErrTooManyInvitations refuses a workspace's 51st live invitation.
	ErrTooManyInvitations = errors.New("too many pending invitations")
	// ErrInvitationInvalid means an invitation link is unknown, expired,
	// revoked, replaced by a newer email, or already accepted.
	ErrInvitationInvalid = errors.New("invitation link is invalid")
	// ErrWrongInvitee refuses accepting an invitation sent to another email.
	ErrWrongInvitee = errors.New("invitation is for another email")
)

// invitationRecord is a row of workspace_invitations. Only the digest of the
// emailed link's token is stored, and only once the listener has sent it.
type invitationRecord struct {
	bun.BaseModel `bun:"table:workspace_invitations,alias:i"`
	ID            int64     `bun:",pk,autoincrement"`
	PublicID      string    `bun:",nullzero"`
	WorkspaceID   int64     `bun:",notnull"`
	Email         string    `bun:",notnull"`
	Role          auth.Role `bun:",notnull"`
	InvitedBy     int64     `bun:",nullzero"`
	TokenHash     sql.NullString
	SentAt        sql.NullTime
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	ExpiresAt     time.Time `bun:",notnull"`
}

// Sender is who sent an invitation, by public ID and name.
type Sender struct {
	PublicID string `json:"id"`
	Name     string `json:"name"`
}

// Invitation is a workspace's invitation, as owners and admins see it.
// SentAt is null until the email has gone; an expired one is listed until
// Cleanup deletes it.
type Invitation struct {
	PublicID   string         `bun:"public_id" json:"id"`
	Email      string         `json:"email"`
	Role       auth.Role      `json:"role"`
	SenderID   sql.NullString `bun:"sender_id" json:"-"`
	SenderName sql.NullString `bun:"sender_name" json:"-"`
	SentAt     *time.Time     `bun:"sent_at" json:"sentAt"`
	CreatedAt  time.Time      `json:"createdAt"`
	ExpiresAt  time.Time      `json:"expiresAt"`
}

// MarshalJSON adds invitedBy: the sender, or null once their account is gone.
func (i Invitation) MarshalJSON() ([]byte, error) {
	type plain Invitation
	var sender *Sender
	if i.SenderID.Valid {
		sender = &Sender{PublicID: i.SenderID.String, Name: i.SenderName.String}
	}
	return json.Marshal(struct {
		plain
		InvitedBy *Sender `json:"invitedBy"`
	}{plain(i), sender})
}

// invitationsOf selects the workspace's invitations with their senders,
// scoped to the workspace first.
func invitationsOf(db bun.IDB, workspaceID int64) *bun.SelectQuery {
	return db.NewSelect().Model((*invitationRecord)(nil)).
		ColumnExpr("i.public_id, i.email, i.role, i.sent_at, i.created_at, i.expires_at").
		ColumnExpr("sender.public_id AS sender_id, sender.name AS sender_name").
		Join("LEFT JOIN users AS sender ON sender.id = i.invited_by").
		Where("i.workspace_id = ?", workspaceID)
}

// IndexInvitations returns one page of the workspace's invitations, newest
// first, expired ones included.
func (s *Store) IndexInvitations(ctx context.Context, workspaceID int64, req PageRequest) (Page[Invitation], error) {
	q := invitationsOf(s.DB, workspaceID).OrderExpr("i.created_at DESC, i.id DESC")
	return Paginate[Invitation](ctx, q, req)
}

// InvitationInput is who to invite, and as what. Callers validate the email
// and that the role isn't owner.
type InvitationInput struct {
	Email string
	Role  auth.Role
}

// CreateInvitation invites in.Email to the workspace on behalf of actorID and
// records workspace.invitation.created, which queues the email. Under the
// workspace lock it reads the actor's role again (ErrNotAssignable unless it
// may give in.Role), then refuses an existing member (ErrAlreadyMember), a
// live invitation to the same email (ErrAlreadyInvited), the 51st live one
// (ErrTooManyInvitations) and the member quota (QuotaError). An expired
// invitation to the same email is replaced. The answer is the same whether
// or not the email has an account.
func (s *Store) CreateInvitation(ctx context.Context, w Workspace, actorID int64, in InvitationInput) (Invitation, error) {
	inv := invitationRecord{WorkspaceID: w.ID, Email: in.Email, Role: in.Role, InvitedBy: actorID, ExpiresAt: time.Now().Add(InvitationLifetime)}
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockWorkspace(ctx, tx, w.ID); err != nil {
			return err
		}
		actor, err := memberRole(ctx, tx, w.ID, actorID)
		if err != nil {
			return err
		}
		if !actor.Can(auth.WorkspaceInvitationStore) || !actor.Manages(in.Role) {
			return ErrNotAssignable
		}
		if err := refuseDuplicateInvitation(ctx, tx, w.ID, in.Email); err != nil {
			return err
		}
		if err := checkPendingInvitations(ctx, tx, w.ID); err != nil {
			return err
		}
		if err := checkWorkspaceMembers(ctx, tx, w.ID, 0); err != nil {
			return err
		}
		if _, err := tx.NewInsert().Model(&inv).Returning("id, public_id").Exec(ctx); err != nil {
			return err
		}
		e := events.WorkspaceInvitationCreated{Invitation: inv.PublicID, Workspace: w.PublicID, Email: inv.Email, Role: string(inv.Role)}
		return s.record(ctx, tx, e, EventScope{WorkspaceID: w.ID})
	})
	if err != nil {
		return Invitation{}, err
	}
	var created Invitation
	err = invitationsOf(s.DB, w.ID).Where("i.id = ?", inv.ID).Scan(ctx, &created)
	return created, err
}

// refuseDuplicateInvitation refuses inviting a member, or an email with a
// live invitation, and deletes an expired invitation to the email so a new
// one can replace it. Call it under lockWorkspace.
func refuseDuplicateInvitation(ctx context.Context, tx bun.Tx, workspaceID int64, email string) error {
	member, err := tx.NewSelect().Model((*membership)(nil)).
		Join("JOIN users AS u ON u.id = m.user_id").
		Where("m.workspace_id = ? AND u.email = ?", workspaceID, email).
		Exists(ctx)
	if err != nil {
		return err
	}
	if member {
		return ErrAlreadyMember
	}
	live, err := tx.NewSelect().Model((*invitationRecord)(nil)).
		Where("i.workspace_id = ? AND i.email = ? AND i.expires_at > now()", workspaceID, email).
		Exists(ctx)
	if err != nil {
		return err
	}
	if live {
		return ErrAlreadyInvited
	}
	_, err = tx.NewDelete().Model((*invitationRecord)(nil)).
		Where("i.workspace_id = ? AND i.email = ?", workspaceID, email).
		Exec(ctx)
	return err
}

// checkPendingInvitations refuses a workspace's MaxPendingInvitations+1th
// live invitation. Call it under lockWorkspace.
func checkPendingInvitations(ctx context.Context, tx bun.Tx, workspaceID int64) error {
	pending, err := tx.NewSelect().Model((*invitationRecord)(nil)).
		Where("i.workspace_id = ? AND i.expires_at > now()", workspaceID).
		Count(ctx)
	if err != nil {
		return err
	}
	if pending >= MaxPendingInvitations {
		return ErrTooManyInvitations
	}
	return nil
}

// InvitationMail is what an invitation's email needs.
type InvitationMail struct {
	Invitation, Workspace string // public IDs
	WorkspaceID           int64
	WorkspaceName         string
	Email                 string
	Role                  auth.Role
	// SenderName and SenderEmail are empty once the sender's account is gone.
	SenderName, SenderEmail string
}

// IssueInvitationToken makes hash the live link of the invitation publicID,
// replacing any earlier link, sets sent_at, and returns what the email needs.
// It returns sql.ErrNoRows when the invitation was revoked, accepted or has
// expired, so the listener sends nothing.
func (s *Store) IssueInvitationToken(ctx context.Context, publicID, hash string) (InvitationMail, error) {
	var mail InvitationMail
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var inv invitationRecord
		err := tx.NewUpdate().Model(&inv).
			Set("token_hash = ?", hash).
			Set("sent_at = now()").
			Where("public_id = ? AND expires_at > now()", publicID).
			Returning("*").
			Scan(ctx)
		if err != nil {
			return err
		}
		mail = InvitationMail{Invitation: inv.PublicID, WorkspaceID: inv.WorkspaceID, Email: inv.Email, Role: inv.Role}
		var senderName, senderEmail sql.NullString
		err = tx.NewSelect().Model((*invitationRecord)(nil)).
			ColumnExpr("w.public_id, w.name, sender.name, sender.email").
			Join("JOIN workspaces AS w ON w.id = i.workspace_id").
			Join("LEFT JOIN users AS sender ON sender.id = i.invited_by").
			Where("i.id = ?", inv.ID).
			Scan(ctx, &mail.Workspace, &mail.WorkspaceName, &senderName, &senderEmail)
		mail.SenderName, mail.SenderEmail = senderName.String, senderEmail.String
		return err
	})
	return mail, err
}

// InvitationPreview is what an invitation link shows before it's accepted.
type InvitationPreview struct {
	Workspace struct {
		Name string `json:"name"`
	} `json:"workspace"`
	InvitedBy *struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"invitedBy"`
	Email     string    `json:"email"`
	Role      auth.Role `json:"role"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// CheckInvitation returns the live invitation whose link is hash, or
// ErrInvitationInvalid.
func (s *Store) CheckInvitation(ctx context.Context, hash string) (InvitationPreview, error) {
	var p InvitationPreview
	var senderName, senderEmail sql.NullString
	err := s.DB.NewSelect().Model((*invitationRecord)(nil)).
		ColumnExpr("w.name, sender.name, sender.email, i.email, i.role, i.expires_at").
		Join("JOIN workspaces AS w ON w.id = i.workspace_id").
		Join("LEFT JOIN users AS sender ON sender.id = i.invited_by").
		Where("i.token_hash = ? AND i.expires_at > now()", hash).
		Scan(ctx, &p.Workspace.Name, &senderName, &senderEmail, &p.Email, &p.Role, &p.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrInvitationInvalid
	}
	if senderName.Valid {
		p.InvitedBy = &struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		}{senderName.String, senderEmail.String}
	}
	return p, err
}

// AcceptInvitation adds u to the workspace of the live invitation whose link
// is hash, with its role (a member already keeps theirs), deletes the
// invitation and records workspace.invitation.accepted. It takes the
// workspace lock, then the invitation's row, so a second accept, a revoke or
// a membership change waits and then finds things as they are. It returns
// ErrInvitationInvalid for a dead link, ErrWrongInvitee when the invitation
// is for another email (it's left as it was), and a QuotaError when the
// workspace is over its member quota.
func (s *Store) AcceptInvitation(ctx context.Context, hash string, u User) (Workspace, error) {
	var w Workspace
	err := s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var workspaceID int64
		err := tx.NewSelect().Model((*invitationRecord)(nil)).Column("i.workspace_id").
			Where("i.token_hash = ? AND i.expires_at > now()", hash).
			Scan(ctx, &workspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvitationInvalid
		}
		if err != nil {
			return err
		}
		if err := lockWorkspace(ctx, tx, workspaceID); err != nil {
			return err
		}
		var inv invitationRecord
		err = tx.NewSelect().Model(&inv).
			Where("i.workspace_id = ? AND i.token_hash = ? AND i.expires_at > now()", workspaceID, hash).
			For("UPDATE").
			Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvitationInvalid
		}
		if err != nil {
			return err
		}
		if inv.Email != u.Email {
			return ErrWrongInvitee
		}
		if err := checkWorkspaceMembers(ctx, tx, workspaceID, inv.ID); err != nil {
			return err
		}
		m := membership{UserID: u.ID, WorkspaceID: workspaceID, Role: inv.Role}
		if _, err := tx.NewInsert().Model(&m).On("CONFLICT (workspace_id, user_id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
		role, err := memberRole(ctx, tx, workspaceID, u.ID)
		if err != nil {
			return err
		}
		if _, err := tx.NewDelete().Model((*invitationRecord)(nil)).Where("i.id = ?", inv.ID).Exec(ctx); err != nil {
			return err
		}
		if err := tx.NewSelect().Model(&w).Column("w.public_id").Where("w.id = ?", workspaceID).Scan(ctx); err != nil {
			return err
		}
		e := events.WorkspaceInvitationAccepted{Invitation: inv.PublicID, Workspace: w.PublicID, Role: string(role)}
		return s.record(ctx, tx, e, EventScope{ActorID: u.ID, UserID: u.ID, WorkspaceID: workspaceID})
	})
	if err != nil {
		return Workspace{}, err
	}
	return s.FindWorkspace(ctx, u.ID, w.PublicID)
}

// RevokeInvitation deletes the workspace's invitation publicID, so its link
// stops working, and records workspace.invitation.revoked. actor must be able
// to give the invitation's role (ErrNotAssignable), as for removing a member.
// It locks the workspace, then the invitation, in the order accepting does:
// the other way round, the two deadlock. sql.ErrNoRows: no such invitation
// in this workspace.
func (s *Store) RevokeInvitation(ctx context.Context, w Workspace, actor auth.Role, publicID string) error {
	return s.DB.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if err := lockWorkspace(ctx, tx, w.ID); err != nil {
			return err
		}
		var inv invitationRecord
		err := tx.NewSelect().Model(&inv).
			Where("i.public_id = ? AND i.workspace_id = ?", publicID, w.ID).
			For("UPDATE").
			Scan(ctx)
		if err != nil {
			return err
		}
		if !actor.Manages(inv.Role) {
			return ErrNotAssignable
		}
		if _, err := tx.NewDelete().Model((*invitationRecord)(nil)).Where("i.id = ?", inv.ID).Exec(ctx); err != nil {
			return err
		}
		e := events.WorkspaceInvitationRevoked{Invitation: inv.PublicID, Workspace: w.PublicID, Email: inv.Email}
		return s.record(ctx, tx, e, EventScope{WorkspaceID: w.ID})
	})
}
