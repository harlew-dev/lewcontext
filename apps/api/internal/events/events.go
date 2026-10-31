// Package events defines what the API records as having happened: sign-ins,
// password changes, workspaces, their members and projects created, changed
// or deleted, invitations sent and accepted, accounts deleted, and rate
// limits reached.
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
	WorkspaceCreated{}, WorkspaceRenamed{}, WorkspaceDeleted{},
	WorkspaceMemberUpdated{}, WorkspaceMemberRemoved{}, WorkspaceMemberLeft{},
	WorkspaceInvitationCreated{}, WorkspaceInvitationSent{}, WorkspaceInvitationRevoked{}, WorkspaceInvitationAccepted{},
	ProjectCreated{}, ProjectUpdated{}, ProjectDeleted{},
	RateLimitReached{},
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

// meta is who is acting in a request and with what browser, carried in the
// request context so store methods can record it without extra arguments.
type meta struct {
	actorID   int64
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

// UserAgent is the request's User-Agent, or "" outside a request.
func UserAgent(ctx context.Context) string { return current(ctx).userAgent }
