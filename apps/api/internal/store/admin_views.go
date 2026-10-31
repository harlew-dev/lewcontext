package store

import (
	"context"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"time"
)

// What the admin dashboard shows: every user and every workspace. These
// queries are deliberately not scoped to a membership, since they exist to
// see across workspaces, so they're only reachable behind requireAdminKey
// and requireAdminSession (httpapi/admin.go). They return no secrets.

// Default sorts: the newest first. The web app mirrors the keys in
// src/types/admin.ts.
const (
	DefaultAdminUserSort      = "-created"
	DefaultAdminWorkspaceSort = "-created"
)

// adminUserSorts and adminWorkspaceSorts are the sort allowlists; every
// order ends in the primary key.
var adminUserSorts = map[string]string{
	"created":  "u.created_at ASC, u.id ASC",
	"-created": "u.created_at DESC, u.id DESC",
	"name":     "lower(u.name) ASC, u.id ASC",
	"-name":    "lower(u.name) DESC, u.id DESC",
	"email":    "u.email ASC, u.id ASC",
	"-email":   "u.email DESC, u.id DESC",
}
var adminWorkspaceSorts = map[string]string{
	"created":  "w.created_at ASC, w.id ASC",
	"-created": "w.created_at DESC, w.id DESC",
	"name":     "lower(w.name) ASC, w.id ASC",
	"-name":    "lower(w.name) DESC, w.id DESC",
}

// AdminUserSortable and AdminWorkspaceSortable report whether key is an
// allowed sort.
func AdminUserSortable(key string) bool {
	_, ok := adminUserSorts[key]
	return ok
}
func AdminWorkspaceSortable(key string) bool {
	_, ok := adminWorkspaceSorts[key]
	return ok
}

// AdminFilter narrows an admin list: Search matches names (and users'
// emails); an empty Sort is the default.
type AdminFilter struct {
	Search string
	Sort   string
}

// AdminUser is an account as the admin dashboard lists it.
type AdminUser struct {
	ID        int64     `bun:"id" json:"-"`
	PublicID  string    `bun:"public_id" json:"id"`
	Name      string    `bun:"name" json:"name"`
	Email     string    `bun:"email" json:"email"`
	CreatedAt time.Time `bun:"created_at" json:"createdAt"`
	// EmailVerifiedAt is nil for an account that never verified its email.
	EmailVerifiedAt *time.Time `bun:"email_verified_at" json:"emailVerifiedAt"`
	MFAEnabled      bool       `bun:"mfa_enabled" json:"mfaEnabled"`
	Admin           bool       `bun:"admin" json:"admin"`
	WorkspaceCount  int        `bun:"workspace_count" json:"workspaceCount"`
	LastSignedInAt  *time.Time `bun:"last_signed_in_at" json:"lastSignedInAt"`
}

// AdminMembership is one of a user's workspaces, with their role in it.
type AdminMembership struct {
	Workspace WorkspaceSummary `json:"workspace"`
	Role      auth.Role        `json:"role"`
	AddedAt   time.Time        `json:"addedAt"`
}

// AdminUserDetail is one user's page: the list's fields, their memberships
// (at most 100, newest first) and how many app sessions they have live.
type AdminUserDetail struct {
	AdminUser
	Memberships  []AdminMembership `json:"memberships"`
	SessionCount int               `json:"sessionCount"`
}

// AdminPerson names an account inside another record (a workspace's owners).
type AdminPerson struct {
	PublicID string `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

// AdminWorkspace is a workspace as the admin dashboard lists it, with up to
// three of its owners, oldest first.
type AdminWorkspace struct {
	ID           int64         `bun:"id" json:"-"`
	PublicID     string        `bun:"public_id" json:"id"`
	Name         string        `bun:"name" json:"name"`
	CreatedAt    time.Time     `bun:"created_at" json:"createdAt"`
	MemberCount  int           `bun:"member_count" json:"memberCount"`
	ProjectCount int           `bun:"project_count" json:"projectCount"`
	Owners       []AdminPerson `bun:"owners,type:jsonb" json:"owners"`
}

// AdminWorkspaceDetail is one workspace's page: the list's fields and its
// members, by role then name.
type AdminWorkspaceDetail struct {
	AdminWorkspace
	Members []Member `json:"members"`
}

// maxAdminMemberships bounds the memberships a user's page lists.
const maxAdminMemberships = 100

// maxAdminOwners is how many owners each workspace in the list names.
const maxAdminOwners = 3

// adminUsers selects users with what the admin dashboard shows of them.
func adminUsers(db bun.IDB) *bun.SelectQuery {
	mfaOn := db.NewSelect().Model((*mfaTOTP)(nil)).ColumnExpr("1").Where("t.user_id = u.id AND t.confirmed_at IS NOT NULL")
	admin := db.NewSelect().Model((*adminRecord)(nil)).ColumnExpr("1").Where("adm.user_id = u.id")
	workspaces := db.NewSelect().Model((*membership)(nil)).ColumnExpr("count(*)").Where("m.user_id = u.id")
	lastSignIn := db.NewSelect().Model((*eventRecord)(nil)).Column("e.created_at").
		Where("e.user_id = u.id AND e.name = ?", events.LoggedIn{}.EventName()).
		OrderExpr("e.created_at DESC").Limit(1)
	return db.NewSelect().Model((*User)(nil)).
		ColumnExpr("u.id, u.public_id, u.name, u.email, u.created_at, u.email_verified_at").
		ColumnExpr("EXISTS (?) AS mfa_enabled", mfaOn).
		ColumnExpr("EXISTS (?) AS admin", admin).
		ColumnExpr("(?) AS workspace_count", workspaces).
		ColumnExpr("(?) AS last_signed_in_at", lastSignIn)
}

// IndexAdminUsers returns one page of every account that matches f.
func (s *Store) IndexAdminUsers(ctx context.Context, f AdminFilter, req PageRequest) (Page[AdminUser], error) {
	order, ok := adminUserSorts[f.Sort]
	if !ok {
		order = adminUserSorts[DefaultAdminUserSort]
	}
	q := adminUsers(s.DB).OrderExpr(order)
	if f.Search != "" {
		pattern := containsPattern(f.Search)
		q = q.Where(`(u.name ILIKE ? ESCAPE '\' OR u.email ILIKE ? ESCAPE '\')`, pattern, pattern)
	}
	return Paginate[AdminUser](ctx, q, req)
}

// FindAdminUser returns the account with publicID, with its memberships and
// live session count, or sql.ErrNoRows.
func (s *Store) FindAdminUser(ctx context.Context, publicID string) (AdminUserDetail, error) {
	var d AdminUserDetail
	if err := adminUsers(s.DB).Where("u.public_id = ?", publicID).Scan(ctx, &d.AdminUser); err != nil {
		return d, err
	}
	var rows []struct {
		PublicID string    `bun:"public_id"`
		Name     string    `bun:"name"`
		Role     auth.Role `bun:"role"`
		AddedAt  time.Time `bun:"added_at"`
	}
	err := s.DB.NewSelect().Model((*membership)(nil)).
		ColumnExpr("w.public_id, w.name, m.role, m.created_at AS added_at").
		Join("JOIN workspaces AS w ON w.id = m.workspace_id").
		Where("m.user_id = ?", d.ID).
		OrderExpr("m.created_at DESC, m.id DESC").
		Limit(maxAdminMemberships).
		Scan(ctx, &rows)
	if err != nil {
		return d, err
	}
	d.Memberships = make([]AdminMembership, len(rows))
	for i, row := range rows {
		d.Memberships[i] = AdminMembership{Workspace: WorkspaceSummary{PublicID: row.PublicID, Name: row.Name}, Role: row.Role, AddedAt: row.AddedAt}
	}
	sessions, err := s.DB.NewSelect().Model((*sessionRecord)(nil)).
		Where("s.user_id = ? AND s.expires_at > now() AND s.last_seen_at > now() - interval '30 minutes'", d.ID).
		Count(ctx)
	d.SessionCount = int(sessions)
	return d, err
}

// adminWorkspaces selects workspaces with their counts and first owners.
func adminWorkspaces(db bun.IDB) *bun.SelectQuery {
	members := db.NewSelect().Model((*membership)(nil)).ColumnExpr("count(*)").Where("m.workspace_id = w.id")
	projects := db.NewSelect().Model((*Project)(nil)).ColumnExpr("count(*)").Where("p.workspace_id = w.id")
	firstOwners := db.NewSelect().Model((*membership)(nil)).
		ColumnExpr("u.public_id, u.name, u.email").
		Join("JOIN users AS u ON u.id = m.user_id").
		Where("m.workspace_id = w.id AND m.role = ?", auth.RoleOwner).
		OrderExpr("m.created_at, m.id").
		Limit(maxAdminOwners)
	owners := db.NewSelect().TableExpr("(?) AS o", firstOwners).
		ColumnExpr("coalesce(jsonb_agg(jsonb_build_object('id', o.public_id, 'name', o.name, 'email', o.email)), '[]'::jsonb)")
	return db.NewSelect().Model((*Workspace)(nil)).
		ColumnExpr("w.id, w.public_id, w.name, w.created_at").
		ColumnExpr("(?) AS member_count", members).
		ColumnExpr("(?) AS project_count", projects).
		ColumnExpr("(?) AS owners", owners)
}

// IndexAdminWorkspaces returns one page of every workspace that matches f.
func (s *Store) IndexAdminWorkspaces(ctx context.Context, f AdminFilter, req PageRequest) (Page[AdminWorkspace], error) {
	order, ok := adminWorkspaceSorts[f.Sort]
	if !ok {
		order = adminWorkspaceSorts[DefaultAdminWorkspaceSort]
	}
	q := adminWorkspaces(s.DB).OrderExpr(order)
	if f.Search != "" {
		q = q.Where(`w.name ILIKE ? ESCAPE '\'`, containsPattern(f.Search))
	}
	return Paginate[AdminWorkspace](ctx, q, req)
}

// FindAdminWorkspace returns the workspace with publicID and its members, or
// sql.ErrNoRows.
func (s *Store) FindAdminWorkspace(ctx context.Context, publicID string) (AdminWorkspaceDetail, error) {
	var d AdminWorkspaceDetail
	if err := adminWorkspaces(s.DB).Where("w.public_id = ?", publicID).Scan(ctx, &d.AdminWorkspace); err != nil {
		return d, err
	}
	d.Members = []Member{}
	err := membersOf(s.DB, d.ID).
		OrderExpr(byRoleThenName, pgdialect.Array(roleNames())).
		Limit(MaxWorkspaceMembers).
		Scan(ctx, &d.Members)
	return d, err
}

// IndexAdminProjects returns one page of the projects in the workspace with
// publicID, filtered and sorted as the app's list is, or sql.ErrNoRows when
// there is no such workspace.
func (s *Store) IndexAdminProjects(ctx context.Context, publicID string, f ProjectFilter, req PageRequest) (Page[Project], error) {
	var workspaceID int64
	err := s.DB.NewSelect().Model((*Workspace)(nil)).Column("w.id").Where("w.public_id = ?", publicID).Scan(ctx, &workspaceID)
	if err != nil {
		return Page[Project]{}, err
	}
	return s.IndexProjects(ctx, workspaceID, f, req)
}
