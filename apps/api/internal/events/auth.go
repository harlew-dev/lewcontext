package events

// Results of a login attempt.
const (
	LoginSucceeded      = "succeeded"
	LoginWrongPassword  = "wrong_password"
	LoginUnknownAccount = "unknown_account"
	// LoginSuspended: the right password (and code) of a suspended account,
	// refused with an honest 403. A wrong password is wrong_password, as
	// for any account, so a suspension can't be probed.
	LoginSuspended = "suspended"
)

// Steps that check the current password: the two of a password change,
// deleting a workspace or the account, and setting up, turning off or
// renewing the recovery codes of two-factor authentication.
const (
	PasswordStepVerify           = "verify"
	PasswordStepUpdate           = "update"
	PasswordStepDestroyWorkspace = "destroy_workspace"
	PasswordStepDestroyAccount   = "destroy_account"
	PasswordStepMFASetup         = "mfa_setup"
	PasswordStepMFADisable       = "mfa_disable"
	PasswordStepRecoveryCodes    = "recovery_codes"
)

// How a new account's email was verified (Registered.VerifiedBy), and how an
// existing account's was (EmailVerified.Via).
const (
	VerifiedByCode          = "code"
	VerifiedByNone          = "none" // EMAIL_VERIFICATION=off
	VerifiedByPasswordReset = "password_reset"
)

// Registered: an account was created. VerifiedBy is code when its email was
// verified first, none with verification off.
type Registered struct {
	User       string `json:"user"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	VerifiedBy string `json:"verifiedBy"`
}

func (Registered) EventName() string { return "auth.registered" }

// LoginAttempted: a login reached the password check. Email is as typed
// (trimmed, lowercased, and a valid address); User is empty when no account
// uses it.
type LoginAttempted struct {
	Email  string `json:"email"`
	User   string `json:"user,omitempty"`
	Result string `json:"result"`
}

func (LoginAttempted) EventName() string { return "auth.login_attempted" }

// LoggedIn: a login started a session.
type LoggedIn struct {
	User string `json:"user"`
}

func (LoggedIn) EventName() string { return "auth.logged_in" }

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
// can't tell.
type PasswordResetRequested struct {
	Email string `json:"email"`
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

// RegistrationRequested: someone signed up, or asked for a new code, with
// verification on. Email is as typed (trimmed, lowercased, and a valid
// address), whether or not an account uses it: the request doesn't look the
// account up, so its response can't tell. The mail.registration listener
// does, and sends a code or "you already have an account".
type RegistrationRequested struct {
	Email string `json:"email"`
}

func (RegistrationRequested) EventName() string { return "auth.registration.requested" }

// RegistrationSent: the listener emailed a sign-up's address for the request
// event Request (evt_…): a code, or, when ExistingAccount, "you already have
// an account" (User is that account).
type RegistrationSent struct {
	Email           string `json:"email"`
	Request         string `json:"request"`
	ExistingAccount bool   `json:"existingAccount"`
	User            string `json:"user,omitempty"`
}

func (RegistrationSent) EventName() string { return "auth.registration.sent" }

// RegistrationChecked: a code was checked against a sign-up waiting for one.
// Attempt counts this sign-up's tries, from 1.
type RegistrationChecked struct {
	Email   string `json:"email"`
	OK      bool   `json:"ok"`
	Attempt int    `json:"attempt"`
}

func (RegistrationChecked) EventName() string { return "auth.registration.checked" }

// EmailVerificationRequested: a signed-in account asked for a code to verify
// its email.
type EmailVerificationRequested struct {
	User string `json:"user"`
}

func (EmailVerificationRequested) EventName() string { return "auth.email_verification.requested" }

// EmailVerificationSent: the code was emailed, for the request event Request.
type EmailVerificationSent struct {
	User    string `json:"user"`
	Request string `json:"request"`
}

func (EmailVerificationSent) EventName() string { return "auth.email_verification.sent" }

// EmailVerified: an account's email was verified, by a code (Via code) or by
// completing a password reset, whose link went only to that inbox.
type EmailVerified struct {
	User string `json:"user"`
	Via  string `json:"via"`
}

func (EmailVerified) EventName() string { return "auth.email_verified" }

// Who suspended or unsuspended an account (Suspended.By, Unsuspended.By):
// an admin from the dashboard, or the operator's command line.
const (
	SuspendedByAdmin = "admin"
	SuspendedByCLI   = "cli"
)

// Suspended: an account was suspended (docs/api/admin.md#suspending-an-account):
// every session of it signed out (SessionsRevoked) and, when asked, every
// live invitation it sent revoked (InvitationsRevoked; each is also
// workspace.invitation.revoked). From the dashboard, admin.user.suspended
// is recorded with it, holding the reason; from the command line (By cli)
// the reason is here.
type Suspended struct {
	User               string `json:"user"`
	By                 string `json:"by"`
	SessionsRevoked    int    `json:"sessionsRevoked"`
	InvitationsRevoked int    `json:"invitationsRevoked"`
	Reason             string `json:"reason,omitempty"`
}

func (Suspended) EventName() string { return "auth.suspended" }

// Unsuspended: a suspended account may sign in again. From the dashboard,
// admin.user.unsuspended is recorded with it, holding the reason; from the
// command line the reason is here.
type Unsuspended struct {
	User   string `json:"user"`
	By     string `json:"by"`
	Reason string `json:"reason,omitempty"`
}

func (Unsuspended) EventName() string { return "auth.unsuspended" }

// SessionsRevokedAll: the operator signed every account out from the
// command line (admin sessions revoke-all), for a suspected leak of the
// session tables: Sessions app sessions and AdminSessions admin sessions.
type SessionsRevokedAll struct {
	Sessions      int `json:"sessions"`
	AdminSessions int `json:"adminSessions"`
}

func (SessionsRevokedAll) EventName() string { return "auth.sessions_revoked_all" }
