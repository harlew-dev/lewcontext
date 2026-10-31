// Package mail renders emails from HTML templates and hands them to a driver
// that delivers them. The only driver so far is FileDriver, a development
// mailbox; there is no email delivery yet.
//
// Templates are html/template files embedded in the binary: each email's file
// defines its "title" and "content", which templates/layout.html wraps in the
// shared document. html/template
// escapes every value for where it appears (text, attribute, URL), so data
// such as an account's name can't add markup or links. Data structs hold
// plain strings: never convert a value to template.HTML, template.URL or the
// other "already safe" types, which switch that escaping off.
package mail

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/mail"
	"strings"
)

//go:embed templates/*.html
var files embed.FS

// Template names an email: templates/<name>.html.
type Template string

const (
	ForgotPassword      Template = "forgot-password"
	WorkspaceInvitation Template = "workspace-invitation"
)

// Subjects are fixed text kept here rather than in the HTML file: a subject is
// a mail header, which must hold no user data and isn't HTML-escaped.
var subjects = map[Template]string{
	ForgotPassword:      "Reset your password",
	WorkspaceInvitation: "You’ve been invited to join a workspace",
}

// ForgotPasswordData fills templates/forgot-password.html.
type ForgotPasswordData struct {
	Name string // the account's name, as the user typed it
	URL  string // the reset link, built from APP_ORIGIN
}

// WorkspaceInvitationData fills templates/workspace-invitation.html. Every
// field but URL is user data.
type WorkspaceInvitationData struct {
	Workspace    string // the workspace's name
	Inviter      string // who sent it; empty once their account is gone
	InviterEmail string
	Role         string // with its article: "a member", "an admin"
	URL          string // the invitation link, built from APP_ORIGIN
}

// Message is a rendered email, ready for a driver.
type Message struct {
	To, Subject string
	Template    Template
	HTML        string
}

// Driver delivers rendered messages: FileDriver now, SMTP or a provider later.
type Driver interface {
	Deliver(ctx context.Context, m Message) error
}

type Mailer struct {
	templates map[Template]*template.Template
	driver    Driver
}

// New parses every template with the layout, so a broken or missing one
// stops the server from starting rather than failing when an email is sent.
// Each email is its own set, since each defines "title" and "content".
func New(driver Driver) (*Mailer, error) {
	templates := map[Template]*template.Template{}
	for name := range subjects {
		t, err := template.ParseFS(files, "templates/layout.html", "templates/"+fileName(name))
		if err != nil {
			return nil, fmt.Errorf("mail template %s is missing or broken: %w", fileName(name), err)
		}
		templates[name] = t.Option("missingkey=error")
	}
	return &Mailer{templates: templates, driver: driver}, nil
}

// Send renders template t with data (its *Data struct) and delivers it to
// the address to. Nothing is delivered if rendering fails.
func (m *Mailer) Send(ctx context.Context, to string, t Template, data any) error {
	if !validRecipient(to) {
		return errors.New("mail: invalid recipient address")
	}
	subject, ok := subjects[t]
	if !ok {
		return fmt.Errorf("mail: unknown template %q", t)
	}
	var body bytes.Buffer
	if err := m.templates[t].ExecuteTemplate(&body, "layout.html", data); err != nil {
		return fmt.Errorf("render mail %s: %w", t, err)
	}
	return m.driver.Deliver(ctx, Message{To: to, Subject: subject, Template: t, HTML: body.String()})
}

// validRecipient accepts a bare address (no display name) without line
// breaks, so it can't add headers to a message.
func validRecipient(to string) bool {
	if strings.ContainsAny(to, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(to)
	return err == nil && address.Address == to
}

func fileName(t Template) string {
	return string(t) + ".html"
}
