package events

import "time"

// Results of a sign-in at the admin door. Every refusal answers the same
// 401; the result says why, for the operator.
const (
	AdminLoginMFARequired      = "mfa_required"
	AdminLoginMFASetupRequired = "mfa_setup_required"
	AdminLoginWrongPassword    = "wrong_password"
	AdminLoginUnknownAccount   = "unknown_account"
	AdminLoginNotAdmin         = "not_admin"
	// AdminLoginMFAOff: an admin without MFA, outside the setup window.
	AdminLoginMFAOff = "mfa_off"
)

// MFAStepAdminLogin is auth.mfa.checked's step for an admin sign-in's code.
const MFAStepAdminLogin = "admin_login"

// AdminGranted: the operator made an account an admin (admin grant).
// MFASetupUntil is when the setup window at the door closes, or nil when MFA
// is already on.
type AdminGranted struct {
	User          string     `json:"user"`
	MFASetupUntil *time.Time `json:"mfaSetupUntil"`
}

func (AdminGranted) EventName() string { return "admin.granted" }

// AdminRevoked: the operator took admin away (admin revoke), signing out
// the user's admin sessions.
type AdminRevoked struct {
	User                 string `json:"user"`
	AdminSessionsRevoked int    `json:"adminSessionsRevoked"`
}

func (AdminRevoked) EventName() string { return "admin.revoked" }

// AdminLoginAttempted: a sign-in at the admin door reached the password
// check. Email is as typed; User is empty when no account uses it.
type AdminLoginAttempted struct {
	Email  string `json:"email"`
	User   string `json:"user,omitempty"`
	Result string `json:"result"`
}

func (AdminLoginAttempted) EventName() string { return "admin.login_attempted" }

// AdminLoggedIn: an admin session started, after the second factor.
type AdminLoggedIn struct {
	User string `json:"user"`
}

func (AdminLoggedIn) EventName() string { return "admin.logged_in" }

// AdminLoggedOut: an admin signed out of the admin dashboard.
type AdminLoggedOut struct {
	User string `json:"user"`
}

func (AdminLoggedOut) EventName() string { return "admin.logged_out" }

// AdminUserListed: an admin loaded a page of the users list, with its
// search, sort and page number.
type AdminUserListed struct {
	Admin string `json:"admin"`
	Q     string `json:"q"`
	Sort  string `json:"sort"`
	Page  int    `json:"page"`
}

func (AdminUserListed) EventName() string { return "admin.user.listed" }

// AdminUserViewed: an admin opened one user's page.
type AdminUserViewed struct {
	Admin string `json:"admin"`
	User  string `json:"user"`
}

func (AdminUserViewed) EventName() string { return "admin.user.viewed" }

// AdminWorkspaceListed: an admin loaded a page of the workspaces list.
type AdminWorkspaceListed struct {
	Admin string `json:"admin"`
	Q     string `json:"q"`
	Sort  string `json:"sort"`
	Page  int    `json:"page"`
}

func (AdminWorkspaceListed) EventName() string { return "admin.workspace.listed" }

// AdminWorkspaceViewed: an admin opened one workspace's page.
type AdminWorkspaceViewed struct {
	Admin     string `json:"admin"`
	Workspace string `json:"workspace"`
}

func (AdminWorkspaceViewed) EventName() string { return "admin.workspace.viewed" }

// AdminEventListed: an admin loaded a page of the Events list, with the
// filters used (so "who searched for this email" is answerable).
type AdminEventListed struct {
	Admin   string       `json:"admin"`
	Filters EventFilters `json:"filters"`
}

func (AdminEventListed) EventName() string { return "admin.event.listed" }

// EventFilters are the Events list's filters as the admin sent them; empty
// ones are left out. IP is the address or range searched for, not the
// admin's own, which is in the events.ip column like any request's.
type EventFilters struct {
	Name       string     `json:"name,omitempty"`
	Account    string     `json:"account,omitempty"`
	Workspace  string     `json:"workspace,omitempty"`
	Email      string     `json:"email,omitempty"`
	IP         string     `json:"ip,omitempty"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
	AdminReads bool       `json:"adminReads,omitempty"`
	Before     string     `json:"before,omitempty"`
}

// AdminEventViewed: an admin opened one event's detail.
type AdminEventViewed struct {
	Admin string `json:"admin"`
	Event string `json:"event"`
}

func (AdminEventViewed) EventName() string { return "admin.event.viewed" }

// AdminMailViewed: an admin loaded the Mail summary for Window (24h or 7d).
type AdminMailViewed struct {
	Admin  string `json:"admin"`
	Window string `json:"window"`
}

func (AdminMailViewed) EventName() string { return "admin.mail.viewed" }

// AdminDeliveryListed: an admin loaded a page of the deliveries list, with
// the filters used.
type AdminDeliveryListed struct {
	Admin   string          `json:"admin"`
	Filters DeliveryFilters `json:"filters"`
}

func (AdminDeliveryListed) EventName() string { return "admin.delivery.listed" }

// DeliveryFilters are the deliveries list's filters; empty ones are left out.
type DeliveryFilters struct {
	Status   string     `json:"status,omitempty"`
	Listener string     `json:"listener,omitempty"`
	From     *time.Time `json:"from,omitempty"`
	To       *time.Time `json:"to,omitempty"`
	Before   string     `json:"before,omitempty"`
}

// AdminProjectListed: an admin loaded a page of a workspace's projects, with
// its search, status, sort and page number.
type AdminProjectListed struct {
	Admin     string `json:"admin"`
	Workspace string `json:"workspace"`
	Q         string `json:"q"`
	Status    string `json:"status"`
	Sort      string `json:"sort"`
	Page      int    `json:"page"`
}

func (AdminProjectListed) EventName() string { return "admin.project.listed" }
