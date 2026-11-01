// Package events defines what the API records as having happened: sign-ins,
// password changes, workspaces, their members and projects created, changed
// or deleted, invitations sent and accepted, accounts deleted, rate limits
// reached, and servers started and stopped.
//
// Each event is a struct whose JSON is the stored payload. Payloads identify
// records by public ID only (usr_, wsp_, prj_) and never hold passwords,
// hashes, session tokens or CSRF tokens. The store writes an event in the
// same transaction as the change it describes (store.record), so a change
// that rolls back leaves no event.
package events

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Event is one thing that happened. EventName is "<resource>.<fact>" (docs/naming.md),
// e.g. "project.created" or "workspace.member.removed".
type Event interface {
	EventName() string
}

// Catalogue lists every event type, for tests and docs.
var Catalogue = []Event{
	Registered{}, LoginAttempted{}, LoggedIn{}, LoggedOut{},
	OtherSessionsRevoked{}, PasswordChecked{}, PasswordChanged{}, ProfileUpdated{},
	AccountDeleted{},
	PasswordResetRequested{}, PasswordResetSent{}, PasswordResetCompleted{},
	RegistrationRequested{}, RegistrationSent{}, RegistrationChecked{},
	EmailVerificationRequested{}, EmailVerificationSent{}, EmailVerified{},
	MFAEnabled{}, MFADisabled{}, MFAChecked{}, RecoveryCodesCreated{},
	WorkspaceCreated{}, WorkspaceRenamed{}, WorkspaceDeleted{},
	WorkspaceMemberUpdated{}, WorkspaceMemberRemoved{}, WorkspaceMemberLeft{},
	WorkspaceInvitationCreated{}, WorkspaceInvitationSent{}, WorkspaceInvitationRevoked{}, WorkspaceInvitationAccepted{},
	ProjectCreated{}, ProjectUpdated{}, ProjectDeleted{},
	RateLimitReached{},
	ServerStarted{}, ServerStopped{},
	EmailBlocked{}, EmailUnblocked{},
	AdminGranted{}, AdminUpdated{}, AdminRevoked{}, AdminLoginAttempted{}, AdminLoggedIn{}, AdminLoggedOut{},
	AdminAccessRefused{}, AdminSessionMoved{}, AdminSessionEnded{}, AdminSessionsRevoked{}, AdminAdminListed{},
	AdminUserListed{}, AdminUserViewed{}, AdminWorkspaceListed{}, AdminWorkspaceViewed{},
	AdminEventListed{}, AdminEventViewed{}, AdminMailViewed{}, AdminMonitoringViewed{}, AdminDeliveryListed{}, AdminProjectListed{},
	AdminEmailBlockListed{}, AdminEmailUnblocked{},
}

// Change is a field's value before and after an update.
type Change struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Changes maps field names to their change; fields that kept their value
// are left out.
type Changes map[string]Change

// Add records field's change if from and to differ.
func (c Changes) Add(field, from, to string) {
	if from != to {
		c[field] = Change{From: from, To: to}
	}
}

// maxUserAgent matches the events.user_agent column's CHECK.
const maxUserAgent = 256

// meta is who is acting in a request, from where and with what browser,
// carried in the request context so store methods can record it without
// extra arguments.
type meta struct {
	actorID   int64
	ip        string
	requestID string
	userAgent string
}
type metaKey struct{}

func current(ctx context.Context) meta {
	m, _ := ctx.Value(metaKey{}).(meta)
	return m
}

// WithActor records that the signed-in user actorID is acting.
func WithActor(ctx context.Context, actorID int64) context.Context {
	m := current(ctx)
	m.actorID = actorID
	return context.WithValue(ctx, metaKey{}, m)
}

// WithRequest records the request's client address and its ID
// (observe.Request), together, for the events.ip and events.request_id
// columns: httpapi.ClientAddress sets both once per request.
func WithRequest(ctx context.Context, ip, requestID string) context.Context {
	m := current(ctx)
	m.ip, m.requestID = ip, requestID
	return context.WithValue(ctx, metaKey{}, m)
}

// WithUserAgent records the request's User-Agent, made valid UTF-8 and cut
// to 256 characters.
func WithUserAgent(ctx context.Context, userAgent string) context.Context {
	userAgent = strings.ToValidUTF8(userAgent, "")
	if utf8.RuneCountInString(userAgent) > maxUserAgent {
		userAgent = string([]rune(userAgent)[:maxUserAgent])
	}
	m := current(ctx)
	m.userAgent = userAgent
	return context.WithValue(ctx, metaKey{}, m)
}

// Actor is the internal ID of the user acting, or 0 when nobody is signed in.
func Actor(ctx context.Context) int64 { return current(ctx).actorID }

// IP is the request's client address, or "" outside a request (the CLI and
// listeners).
func IP(ctx context.Context) string { return current(ctx).ip }

// RequestID is the ID of the request the event was recorded in (req_…, or
// a trusted proxy's), or "" outside a request.
func RequestID(ctx context.Context) string { return current(ctx).requestID }

// UserAgent is the request's User-Agent, or "" outside a request.
func UserAgent(ctx context.Context) string { return current(ctx).userAgent }
