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

// auth.mfa.checked's steps at the admin dashboard: an admin sign-in's code,
// and a code that opens an admin session's step-up window
// (POST /api/admin/step-up).
const (
	MFAStepAdminLogin  = "admin_login"
	MFAStepAdminStepUp = "admin_step_up"
)

// AdminGranted: the operator made an account an admin, or granted it again
// (admin grant). Role is what it may do (viewer or operator); MFASetupUntil
// is when the setup window at the door closes, or nil when MFA is already
// on.
type AdminGranted struct {
	User          string     `json:"user"`
	Role          string     `json:"role"`
	MFASetupUntil *time.Time `json:"mfaSetupUntil"`
}

func (AdminGranted) EventName() string { return "admin.granted" }

// AdminUpdated: the operator changed an admin's role (admin grant -role on
// an existing admin), with changes.role.
type AdminUpdated struct {
	User    string  `json:"user"`
	Changes Changes `json:"changes"`
}

func (AdminUpdated) EventName() string { return "admin.updated" }

// Reasons an admin request with the right key was refused.
const (
	// AdminRefusedIP: the client's address isn't in ADMIN_ALLOWED_IPS.
	AdminRefusedIP = "ip_not_allowed"
)

// AdminAccessRefused: a request with the right admin key (or a valid assets
// pass) was answered as an unknown URL, for Reason. Recorded at most once
// per address (an IPv6 /64) per 15 minutes per server; the address is in
// events.ip. It means the key, or a pass, is held somewhere it shouldn't be.
type AdminAccessRefused struct {
	Reason string `json:"reason"`
}

func (AdminAccessRefused) EventName() string { return "admin.access_refused" }

// AdminSessionMoved: an admin session was used from another network than
// the one it signed in from (another IPv4 /24 or IPv6 /48), recorded once
// per session. From and To are the networks, not the addresses;
// SessionAge is how long ago it signed in, in seconds.
type AdminSessionMoved struct {
	Admin      string `json:"admin"`
	From       string `json:"from"`
	To         string `json:"to"`
	SessionAge int    `json:"sessionAge"`
}

func (AdminSessionMoved) EventName() string { return "admin.session.moved" }

// Why an admin session was ended by the server (AdminSessionEnded.Reason).
const (
	// AdminSessionEndedStepUpFailures: its fifth wrong step-up code.
	AdminSessionEndedStepUpFailures = "step_up_failures"
)

// AdminSessionEnded: the server ended an admin session, for Reason, as a
// sign-in's challenge ends after five wrong codes. Its admin must sign in
// again with the password and a code.
type AdminSessionEnded struct {
	Admin  string `json:"admin"`
	Reason string `json:"reason"`
}

func (AdminSessionEnded) EventName() string { return "admin.session.ended" }

// AdminSessionsRevoked: an admin signed out their other admin sessions.
type AdminSessionsRevoked struct {
	Admin string `json:"admin"`
	Count int    `json:"count"`
}

func (AdminSessionsRevoked) EventName() string { return "admin.sessions_revoked" }

// AdminAdminListed: an admin loaded the Admins page (every admin, their
// sessions and when they last signed in).
type AdminAdminListed struct {
	Admin string `json:"admin"`
}

func (AdminAdminListed) EventName() string { return "admin.admin.listed" }

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
// search, sort and page number, and whether only suspended accounts.
type AdminUserListed struct {
	Admin     string `json:"admin"`
	Q         string `json:"q"`
	Sort      string `json:"sort"`
	Page      int    `json:"page"`
	Suspended bool   `json:"suspended,omitempty"`
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
// ones are left out. Names are the name tokens as sent (names and group
// IDs); From and To the window as resolved (a relative Window's too).
// IP is the address or range searched for, not the admin's own, which is
// in the events.ip column like any request's. Summary is the charts'
// status when they were asked for with the list: ok, busy, too_slow or
// too_wide.
type EventFilters struct {
	Names     []string   `json:"names,omitempty"`
	Outcome   string     `json:"outcome,omitempty"`
	Account   string     `json:"account,omitempty"`
	Workspace string     `json:"workspace,omitempty"`
	Email     string     `json:"email,omitempty"`
	IP        string     `json:"ip,omitempty"`
	Window    string     `json:"window,omitempty"`
	From      *time.Time `json:"from,omitempty"`
	To        *time.Time `json:"to,omitempty"`
	Before    string     `json:"before,omitempty"`
	Summary   string     `json:"summary,omitempty"`
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

// AdminEmailBlockListed: an admin loaded a page of the Email blocks list
// (the addresses whose owners asked for no email), with its search, sort
// and page number.
type AdminEmailBlockListed struct {
	Admin string `json:"admin"`
	Q     string `json:"q"`
	Sort  string `json:"sort"`
	Page  int    `json:"page"`
}

func (AdminEmailBlockListed) EventName() string { return "admin.email_block.listed" }

// AdminEmailUnblocked: an admin took Email off the block list from the
// dashboard, behind a fresh code, for Reason (10 to 500 characters).
// email.unblocked (by: admin) is recorded with it, in the same transaction.
type AdminEmailUnblocked struct {
	Admin  string `json:"admin"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

func (AdminEmailUnblocked) EventName() string { return "admin.email.unblocked" }

// The dashboard's account-and-access actions (plans/features/admin/actions.md):
// each recorded once, with the admin, what they acted on and the reason
// they gave (10 to 500 characters), in the same transaction as the change,
// beside the store's own events for it (auth.mfa.disabled,
// workspace.invitation.revoked, …), which carry the admin as actor too.

// AdminUserSignedOut: an admin signed every session of an account out
// (Sign out everywhere).
type AdminUserSignedOut struct {
	Admin           string `json:"admin"`
	User            string `json:"user"`
	Reason          string `json:"reason"`
	SessionsRevoked int    `json:"sessionsRevoked"`
}

func (AdminUserSignedOut) EventName() string { return "admin.user.signed_out" }

// AdminUserMFAReset: an admin turned an account's two-factor
// authentication off for someone who lost it, having confirmed who was
// asking outside the app; auth.mfa.disabled (reason reset) is recorded
// with it.
type AdminUserMFAReset struct {
	Admin           string `json:"admin"`
	User            string `json:"user"`
	Reason          string `json:"reason"`
	SessionsRevoked int    `json:"sessionsRevoked"`
}

func (AdminUserMFAReset) EventName() string { return "admin.user.mfa_reset" }

// AdminUserSuspended: an admin suspended an account; auth.suspended (by
// admin) is recorded with it.
type AdminUserSuspended struct {
	Admin              string `json:"admin"`
	User               string `json:"user"`
	Reason             string `json:"reason"`
	SessionsRevoked    int    `json:"sessionsRevoked"`
	InvitationsRevoked int    `json:"invitationsRevoked"`
}

func (AdminUserSuspended) EventName() string { return "admin.user.suspended" }

// AdminUserUnsuspended: an admin lifted an account's suspension;
// auth.unsuspended (by admin) is recorded with it.
type AdminUserUnsuspended struct {
	Admin  string `json:"admin"`
	User   string `json:"user"`
	Reason string `json:"reason"`
}

func (AdminUserUnsuspended) EventName() string { return "admin.user.unsuspended" }

// AdminWorkspaceInvitationsRevoked: an admin revoked every live invitation
// of a workspace (Count of them), to stop an invitation-spam run;
// workspace.invitation.revoked is recorded for each.
type AdminWorkspaceInvitationsRevoked struct {
	Admin     string `json:"admin"`
	Workspace string `json:"workspace"`
	Reason    string `json:"reason"`
	Count     int    `json:"count"`
}

func (AdminWorkspaceInvitationsRevoked) EventName() string {
	return "admin.workspace.invitations_revoked"
}

// AdminDeliveryRetried: an admin queued failed deliveries again: one
// (Event and Listener), or every failed delivery of Listener since
// FailedSince (at most 1,000), Count of them.
type AdminDeliveryRetried struct {
	Admin       string     `json:"admin"`
	Reason      string     `json:"reason"`
	Listener    string     `json:"listener"`
	Event       string     `json:"event,omitempty"`
	FailedSince *time.Time `json:"failedSince,omitempty"`
	Count       int        `json:"count"`
}

func (AdminDeliveryRetried) EventName() string { return "admin.delivery.retried" }

// AdminIPBlocked: an admin blocked a client address or range (Network, as
// CIDR) until ExpiresAt; Block is its ipb_ ID.
type AdminIPBlocked struct {
	Admin     string    `json:"admin"`
	Block     string    `json:"block"`
	Network   string    `json:"network"`
	ExpiresAt time.Time `json:"expiresAt"`
	Reason    string    `json:"reason"`
}

func (AdminIPBlocked) EventName() string { return "admin.ip.blocked" }

// AdminIPUnblocked: an admin lifted a block before it expired.
type AdminIPUnblocked struct {
	Admin   string `json:"admin"`
	Block   string `json:"block"`
	Network string `json:"network"`
	Reason  string `json:"reason"`
}

func (AdminIPUnblocked) EventName() string { return "admin.ip.unblocked" }

// AdminIPBlockListed: an admin loaded the IP blocks page.
type AdminIPBlockListed struct {
	Admin string `json:"admin"`
}

func (AdminIPBlockListed) EventName() string { return "admin.ip_block.listed" }

// AdminAdminSignedOut: an admin signed another admin out, every admin and
// app session of theirs (a lost laptop, a session that moved). The one
// action on another admin: anything else stays on the command line.
type AdminAdminSignedOut struct {
	Admin                string `json:"admin"`
	User                 string `json:"user"`
	Reason               string `json:"reason"`
	AdminSessionsRevoked int    `json:"adminSessionsRevoked"`
	SessionsRevoked      int    `json:"sessionsRevoked"`
}

func (AdminAdminSignedOut) EventName() string { return "admin.admin.signed_out" }
