package events

// Results of a login attempt.
const (
	LoginSucceeded      = "succeeded"
	LoginWrongPassword  = "wrong_password"
	LoginUnknownAccount = "unknown_account"
)

// Steps that check the current password: the two of a password change, and
// deleting a workspace or the account.
const (
	PasswordStepVerify           = "verify"
	PasswordStepUpdate           = "update"
	PasswordStepDestroyWorkspace = "destroy_workspace"
	PasswordStepDestroyAccount   = "destroy_account"
)

// Registered: an account was created.
type Registered struct {
	User  string `json:"user"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

func (Registered) EventName() string { return "auth.registered" }

// LoginAttempted: a login reached the password check. Email is as typed
// (trimmed, lowercased, and a valid address); User is empty when no account
// uses it. IP is the connection's address; forwarded headers aren't trusted.
type LoginAttempted struct {
	Email  string `json:"email"`
	IP     string `json:"ip"`
	User   string `json:"user,omitempty"`
	Result string `json:"result"`
}

func (LoginAttempted) EventName() string { return "auth.login_attempted" }

// LoggedIn: a login started a session.
type LoggedIn struct {
	User string `json:"user"`
}

func (LoggedIn) EventName() string { return "auth.logged_in" }

// RateLimited: a rate limit refused its first request in a window. Scope is
// the limit's name, e.g. "auth:per-ip" or "password-reset:per-email"
// (httpapi/limits.go); User is set when a signed-in user hit it.
type RateLimited struct {
	Scope string `json:"scope"`
	User  string `json:"user,omitempty"`
}

func (RateLimited) EventName() string { return "auth.rate_limited" }

// LoggedOut: a session was signed out.
type LoggedOut struct {
	User string `json:"user"`
}

func (LoggedOut) EventName() string { return "auth.logged_out" }

// OtherSessionsRevoked: a user signed out their other sessions.
type OtherSessionsRevoked struct {
	User  string `json:"user"`
	Count int    `json:"count"`
}

func (OtherSessionsRevoked) EventName() string { return "auth.other_sessions_revoked" }

// PasswordChecked: the current password was checked, by a password change or
// before deleting a workspace or the account (Step).
type PasswordChecked struct {
	User string `json:"user"`
	OK   bool   `json:"ok"`
	Step string `json:"step"`
}

func (PasswordChecked) EventName() string { return "auth.password_checked" }

// PasswordChanged: a user set a new password, which signed out their other
// sessions.
type PasswordChanged struct {
	User                 string `json:"user"`
	OtherSessionsRevoked int    `json:"otherSessionsRevoked"`
}

func (PasswordChanged) EventName() string { return "auth.password_changed" }

// AccountDeleted: a user deleted their account. It holds no name or email,
// since the account's personal data is what's being erased, and has no user
// or actor: both would be the deleted account.
type AccountDeleted struct {
	User string `json:"user"`
}

func (AccountDeleted) EventName() string { return "auth.account_deleted" }

// ProfileUpdated: a user changed their account's name.
type ProfileUpdated struct {
	User    string  `json:"user"`
	Changes Changes `json:"changes"`
}

func (ProfileUpdated) EventName() string { return "auth.profile_updated" }

// PasswordResetRequested: someone asked for a password reset link. Email is
// as typed (trimmed, lowercased, and a valid address), whether or not an
// account uses it: the request doesn't look the account up, so its response
// can't tell. IP is the connection's address.
type PasswordResetRequested struct {
	Email string `json:"email"`
	IP    string `json:"ip"`
}

func (PasswordResetRequested) EventName() string { return "auth.password_reset.requested" }

// PasswordResetSent: a reset link was sent to an account's email, for the
// request event Request (evt_…).
type PasswordResetSent struct {
	User    string `json:"user"`
	Email   string `json:"email"`
	Request string `json:"request"`
}

func (PasswordResetSent) EventName() string { return "auth.password_reset.sent" }

// PasswordResetCompleted: a reset link set a new password, which signed out
// every session of the account's.
type PasswordResetCompleted struct {
	User                 string `json:"user"`
	OtherSessionsRevoked int    `json:"otherSessionsRevoked"`
}

func (PasswordResetCompleted) EventName() string { return "auth.password_reset.completed" }
