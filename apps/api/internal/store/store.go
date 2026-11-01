package store

import (
	"encoding/json"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"time"
)

// Store models keep the internal bigint ID out of JSON: responses identify
// records only by their public ID (usr_…, wsp_…, prj_…), which PostgreSQL
// generates as a column default.
type Store struct {
	DB *bun.DB
	// Listeners names the listeners of each event, so recording an event
	// queues a delivery to each (the server sets it to its events.Bus). Nil
	// queues none.
	Listeners interface{ ListenersFor(event string) []string }
}
type User struct {
	bun.BaseModel `bun:"table:users,alias:u"`
	ID            int64     `bun:",pk,autoincrement" json:"-"`
	PublicID      string    `bun:",nullzero" json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"createdAt"`
	// EmailVerifiedAt is when the account proved it owns its email (a code,
	// or a password reset link), or nil: always set for accounts made with
	// EMAIL_VERIFICATION on.
	EmailVerifiedAt *time.Time `bun:"email_verified_at" json:"emailVerifiedAt"`
	// MFAEnabledAt is when two-factor authentication was turned on, or nil
	// while it's off. Read only where a query selects it (mfaEnabledAt);
	// only ever sent in the user's own session responses.
	MFAEnabledAt *time.Time `bun:"mfa_enabled_at,scanonly" json:"mfaEnabledAt"`
	// The policy versions agreed to at sign-up (POLICIES=on) and when, or
	// all nil. Sent as one "policies" object (MarshalJSON).
	TermsOfServiceVersion *time.Time `bun:"terms_of_service_version" json:"-"`
	PrivacyPolicyVersion  *time.Time `bun:"privacy_policy_version" json:"-"`
	PoliciesAcceptedAt    *time.Time `bun:"policies_accepted_at" json:"-"`
	// Suspended says an admin suspended the account
	// (docs/api/admin.md#suspending-an-account). Read only by FindUser, for
	// sign-in's honest refusal after the password; never in a response.
	Suspended bool `bun:"suspended,scanonly" json:"-"`
	// LastSeenAt is when the account last used a session, to the hour
	// (FindSession), or nil before its first signed-in request. Read by the
	// monitoring pages; never in an app response.
	LastSeenAt *time.Time `bun:"last_seen_at" json:"-"`
}

// account carries the password hash so it never travels inside a User response.
type account struct {
	User         `bun:",extend"`
	PasswordHash string
}
type Session struct {
	User                 User
	CSRF                 string
	CreatedAt, ExpiresAt time.Time
}

// Workspace is a workspace as one member sees it: Role is that member's role,
// read from workspace_user, and CanLeave whether they may leave it (anyone but
// its only owner).
type Workspace struct {
	bun.BaseModel `bun:"table:workspaces,alias:w"`
	ID            int64     `bun:",pk,autoincrement" json:"-"`
	PublicID      string    `bun:",nullzero" json:"id"`
	Name          string    `json:"name"`
	Role          auth.Role `bun:",scanonly" json:"role"`
	CanLeave      bool      `bun:",scanonly" json:"canLeave"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}

// MarshalJSON adds what the member's role allows, computed from the role, so
// the web app reads the rules instead of keeping its own copy.
func (w Workspace) MarshalJSON() ([]byte, error) {
	type plain Workspace
	return json.Marshal(struct {
		plain
		Permissions     []auth.Permission `json:"permissions"`
		AssignableRoles []auth.Role       `json:"assignableRoles"`
	}{plain(w), w.Role.Permissions(), w.Role.AssignableRoles()})
}

type WorkspaceInput struct {
	Name string `json:"name"`
}

// membership links a user to a workspace with a role.
type membership struct {
	bun.BaseModel `bun:"table:workspace_user,alias:m"`
	ID            int64 `bun:",pk,autoincrement"`
	UserID        int64 `bun:",notnull"`
	WorkspaceID   int64 `bun:",notnull"`
	Role          auth.Role
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}
type Project struct {
	bun.BaseModel `bun:"table:projects,alias:p"`
	ID            int64     `bun:",pk,autoincrement" json:"-"`
	PublicID      string    `bun:",nullzero" json:"id"`
	WorkspaceID   int64     `bun:",notnull" json:"-"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	Color         string    `json:"color"`
	CreatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"createdAt"`
	UpdatedAt     time.Time `bun:",nullzero,notnull,default:current_timestamp" json:"updatedAt"`
}
type ProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Color       string `json:"color"`
}
