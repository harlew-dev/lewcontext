// Package mail renders emails from templates and hands them to a driver that
// delivers them: SMTPDriver sends them through an SMTP server, FileDriver
// appends them to a development mailbox.
//
// Every email has an HTML part and a plain-text part. The HTML templates are
// html/template files embedded in the binary: each email's file defines its
// "title" and "content", which templates/layout.html wraps in the shared
// document. The plain-text templates (templates/<name>.txt) define their
// "content" for templates/layout.txt, and are rendered in text.go, the only
// file that may use text/template. Both layouts open with the app's name,
// which templates read with {{ app }}, and blockable emails end with a block
// link, {{ blockURL }}. html/template
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
	"lewcontext/api/internal/auth"
	"net/mail"
	"strings"
)

//go:embed templates/*.html templates/*.txt templates/logo.png
var files embed.FS

// logoContentID is how the layout refers to the embedded logo: src="cid:logo".
const logoContentID = "logo"

// Template names an email: templates/<name>.html.
type Template string

const (
	ForgotPassword      Template = "forgot-password"
	WorkspaceInvitation Template = "workspace-invitation"
	VerifyEmail         Template = "verify-email"
	AccountExists       Template = "account-exists"
)

// Subjects are fixed text kept here rather than in the HTML file: a subject is
// a mail header, which must hold no user data and isn't HTML-escaped.
var subjects = map[Template]string{
	ForgotPassword:      "Reset your password",
	WorkspaceInvitation: "You’ve been invited to join a workspace",
	VerifyEmail:         "Confirm your email address",
	AccountExists:       "You already have an account",
}

// blockable are the templates whose emails carry a block link: a footer
// line and List-Unsubscribe, for addresses that may have never heard of us
// (plans/features/email-blocks.md). Mail that only reaches accounts, and
// sign-up codes, which someone just asked for, don't.
var blockable = map[Template]bool{
	WorkspaceInvitation: true,
}

// ErrBlocked means Send sent nothing because the address is on the block
// list. Listeners treat it as done: retrying can't change it.
var ErrBlocked = errors.New("mail: the address is blocked")

// Blocks is what Send needs from the block list: *store.Store.
type Blocks interface {
	CheckEmailBlocked(ctx context.Context, email string) (bool, error)
	IssueEmailBlockLink(ctx context.Context, email, hash string) error
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
	Home         string // the homepage, APP_ORIGIN + "/", for someone new to the app
}

// VerifyEmailData fills templates/verify-email.html: a sign-up's code, or an
// account's verifying the email it has. It holds no name: anyone can send a
// sign-up's code to any address, so none of its words may be theirs.
type VerifyEmailData struct {
	Code string // spaced for reading (auth.FormatEmailCode): "482 913"
}

// AccountExistsData fills templates/account-exists.html, sent instead of a
// code to a sign-up for an email that already has an account.
type AccountExistsData struct {
	Name     string // the account's own name, never the one just typed
	LoginURL string // the login page with its account-exists notice
	ResetURL string // the page that asks for a reset link
}

// Message is a rendered email, ready for a driver.
type Message struct {
	To, Subject string
	Template    Template
	HTML, Text  string
	// Unsubscribe is the block link of a blockable email, sent as
	// List-Unsubscribe; empty otherwise.
	Unsubscribe string
	// Inline are images the HTML shows with src="cid:<ContentID>", sent
	// inside the message (never fetched from a server).
	Inline []Inline
}

// Inline is an image embedded in a message.
type Inline struct {
	ContentID, Name string
	Data            []byte
}

// Driver delivers rendered messages: SMTPDriver, or FileDriver in development.
type Driver interface {
	Deliver(ctx context.Context, m Message) error
}

type Mailer struct {
	templates map[Template]*template.Template
	texts     map[Template]textTemplate
	logo      []byte
	driver    Driver
	origin    string
	blocks    Blocks
}

// Options are what New needs: the Driver that delivers, the product name
// (APP_NAME) the layouts open with, the APP_ORIGIN block links are built
// on, and the block list every Send checks.
type Options struct {
	Driver Driver
	App    string
	Origin string
	Blocks Blocks
}

// New parses every template, HTML and plain text, each with its layout, so a
// broken or missing one stops the server from starting rather than failing
// when an email is sent. Each email is its own set, since each defines
// "title" and "content". html/template escapes the app's name like any
// other value.
func New(o Options) (*Mailer, error) {
	if o.Driver == nil || o.Blocks == nil || o.Origin == "" {
		return nil, errors.New("mail: New needs a driver, an origin and the block list")
	}
	templates := map[Template]*template.Template{}
	texts := map[Template]textTemplate{}
	funcs := template.FuncMap{"app": func() string { return o.App }, "blockURL": noBlockURL}
	for name := range subjects {
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(files, "templates/layout.html", "templates/"+fileName(name))
		if err != nil {
			return nil, fmt.Errorf("mail template %s is missing or broken: %w", fileName(name), err)
		}
		templates[name] = t.Option("missingkey=error")
		if texts[name], err = parseText(files, name, o.App); err != nil {
			return nil, err
		}
	}
	logo, err := files.ReadFile("templates/logo.png")
	if err != nil {
		return nil, err
	}
	return &Mailer{templates: templates, texts: texts, logo: logo, driver: o.Driver, origin: o.Origin, blocks: o.Blocks}, nil
}

// noBlockURL is {{ blockURL }} in an email that isn't blockable: nothing,
// so the layouts leave the footer out.
func noBlockURL() string { return "" }

// Send renders template t with data (its *Data struct), both parts, and
// delivers it to the address to. Nothing is delivered if either part fails
// to render, and nothing to an address on the block list, whatever the
// template: ErrBlocked. A blockable email gets a new block link, stored
// (as its digest) only once both parts have rendered.
func (m *Mailer) Send(ctx context.Context, to string, t Template, data any) error {
	if !validRecipient(to) {
		return errors.New("mail: invalid recipient address")
	}
	subject, ok := subjects[t]
	if !ok {
		return fmt.Errorf("mail: unknown template %q", t)
	}
	address := strings.ToLower(to)
	blocked, err := m.blocks.CheckEmailBlocked(ctx, address)
	if err != nil {
		return err
	}
	if blocked {
		return ErrBlocked
	}
	var token, link string
	if blockable[t] {
		if token, err = auth.Token(); err != nil {
			return err
		}
		link = m.origin + "/auth/email-blocks/" + token
	}
	html, err := m.renderHTML(t, data, link)
	if err != nil {
		return fmt.Errorf("render mail %s: %w", t, err)
	}
	text, err := m.texts[t].render(data, link)
	if err != nil {
		return fmt.Errorf("render mail %s: %w", t, err)
	}
	if token != "" {
		if err := m.blocks.IssueEmailBlockLink(ctx, address, auth.Digest(token)); err != nil {
			return err
		}
	}
	logo := Inline{ContentID: logoContentID, Name: "logo.png", Data: m.logo}
	message := Message{To: to, Subject: subject, Template: t, HTML: html, Text: text, Unsubscribe: link, Inline: []Inline{logo}}
	return m.driver.Deliver(ctx, message)
}

// renderHTML renders t's HTML part, with link as {{ blockURL }}. It runs a
// clone with that function set, so concurrent sends don't share it and the
// parsed set is never executed (html/template can't clone one that was).
func (m *Mailer) renderHTML(t Template, data any, link string) (string, error) {
	clone, err := m.templates[t].Clone()
	if err != nil {
		return "", err
	}
	clone.Funcs(template.FuncMap{"blockURL": func() string { return link }})
	var body bytes.Buffer
	if err := clone.ExecuteTemplate(&body, "layout.html", data); err != nil {
		return "", err
	}
	return body.String(), nil
}

// done is a listener's answer to Send's error: an address on the block
// list is done with, not retried. The listener records no .sent event.
func done(err error) error {
	if errors.Is(err, ErrBlocked) {
		return nil
	}
	return err
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
