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
	IP     string `json:"ip"`
	User   string `json:"user,omitempty"`
	Result string `json:"result"`
}

func (AdminLoginAttempted) EventName() string { return "admin.login_attempted" }

// AdminLoggedIn: an admin session started, after the second factor.
type AdminLoggedIn struct {
	User string `json:"user"`
	IP   string `json:"ip"`
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
