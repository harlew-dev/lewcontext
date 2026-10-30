package events

// Results of a login attempt.
const (
	LoginSucceeded      = "succeeded"
	LoginWrongPassword  = "wrong_password"
	LoginUnknownAccount = "unknown_account"
)

// Steps of a password change that check the current password.
const (
	PasswordStepVerify = "verify"
	PasswordStepUpdate = "update"
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
// "ip" or "account"; User is set when a signed-in user hit it.
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

// PasswordChecked: a password change checked the current password.
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
