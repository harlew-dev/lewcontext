package mail

import (
	"context"
	"database/sql"
	"go/parser"
	"go/token"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// outbox is a driver that keeps what it's given.
type outbox struct{ sent []Message }

func (o *outbox) Deliver(_ context.Context, m Message) error {
	o.sent = append(o.sent, m)
	return nil
}

func newMailer(t *testing.T) (*Mailer, *outbox) {
	t.Helper()
	box := &outbox{}
	m, err := New(box, "lewcontext")
	if err != nil {
		t.Fatal(err)
	}
	return m, box
}

// Example data for every template, so each one is rendered at least once.
var examples = map[Template]any{
	ForgotPassword:      ForgotPasswordData{Name: "Ada", URL: "http://localhost:3000/auth/forgot-password/abc"},
	WorkspaceInvitation: WorkspaceInvitationData{Workspace: "Acme", Inviter: "Ada", InviterEmail: "ada@example.com", Role: "a member", URL: "http://localhost:3000/auth/invitations/abc", Home: "http://localhost:3000/"},
}

// Every email is a whole document from the shared layout, titled with its own
// subject.
func TestLayoutWrapsEveryTemplate(t *testing.T) {
	m, box := newMailer(t)
	for name, data := range examples {
		if err := m.Send(context.Background(), "ada@example.com", name, data); err != nil {
			t.Fatal(err)
		}
		html := box.sent[len(box.sent)-1].HTML
		if !strings.HasPrefix(html, "<!doctype html>") || !strings.Contains(html, "<title>"+subjects[name]+"</title>") || !strings.Contains(html, "max-width: 480px") {
			t.Errorf("%s isn't wrapped in the layout:\n%s", name, html)
		}
	}
}

func TestEveryTemplateRenders(t *testing.T) {
	m, box := newMailer(t)
	for name := range subjects {
		data, ok := examples[name]
		if !ok {
			t.Fatalf("no example data for %s", name)
		}
		if err := m.Send(context.Background(), "ada@example.com", name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if len(box.sent) != len(subjects) {
		t.Fatalf("sent %d of %d", len(box.sent), len(subjects))
	}
	for _, msg := range box.sent {
		if msg.Subject == "" || !strings.Contains(msg.HTML, "Ada") || !strings.Contains(msg.Text, "Ada") {
			t.Fatalf("%s rendered badly: %+v", msg.Template, msg)
		}
		// The plain-text part is text: the link as it is, and no markup.
		if !strings.Contains(msg.Text, "http://localhost:3000/auth/") || strings.Contains(msg.Text, "<") {
			t.Fatalf("%s plain text:\n%s", msg.Template, msg.Text)
		}
	}
}

// Both parts open with the app's name: the HTML card's wordmark above the
// content, escaped like any value, and the plain text's first line.
func TestAppNameOpensEveryEmail(t *testing.T) {
	box := &outbox{}
	m, err := New(box, "Acme & <Co>")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range examples {
		if err := m.Send(context.Background(), "ada@example.com", name, data); err != nil {
			t.Fatal(err)
		}
		msg := box.sent[len(box.sent)-1]
		wordmark := strings.Index(msg.HTML, ">Acme &amp; &lt;Co&gt;</td>")
		if wordmark < 0 || wordmark > strings.Index(msg.HTML, "Ada") || strings.Contains(msg.HTML, "<Co>") {
			t.Errorf("%s HTML doesn't open with the escaped name:\n%s", name, msg.HTML)
		}
		if !strings.HasPrefix(msg.Text, "Acme & <Co>\n\n") || strings.HasPrefix(msg.Text[len("Acme & <Co>\n\n"):], "\n") || !strings.HasSuffix(msg.Text, ".\n") || strings.HasSuffix(msg.Text, "\n\n") {
			t.Errorf("%s plain text:\n%q", name, msg.Text)
		}
	}
}

// Every email needs its plain-text template; New fails without one.
func TestMissingTextTemplate(t *testing.T) {
	if _, err := parseText(files, Template("missing"), "lewcontext"); err == nil {
		t.Fatal("parsed a template that doesn't exist")
	}
}

func TestUntrustedDataIsEscaped(t *testing.T) {
	m, box := newMailer(t)
	names := map[string]string{
		"<script>alert('XSS')</script>":  "&lt;script&gt;alert(&#39;XSS&#39;)&lt;/script&gt;",
		"<h1>Hack</h1>":                  "&lt;h1&gt;Hack&lt;/h1&gt;",
		`"><img src=x onerror=alert(1)>`: "&#34;&gt;&lt;img src=x onerror=alert(1)&gt;",
	}
	for name, escaped := range names {
		data := ForgotPasswordData{Name: name, URL: "https://example.com/reset"}
		if err := m.Send(context.Background(), "ada@example.com", ForgotPassword, data); err != nil {
			t.Fatal(err)
		}
		html := box.sent[len(box.sent)-1].HTML
		if !strings.Contains(html, escaped) {
			t.Errorf("name %q not escaped as %q", name, escaped)
		}
		// The layout's logo is the only image.
		for tag, layout := range map[string]int{"<script": 0, "<h1": 0, "<img": 1} {
			if strings.Count(html, tag) != layout {
				t.Errorf("name %q added %s to the email", name, tag)
			}
		}
	}
}

// A link that isn't http(s) is replaced in href, so data can't make a
// javascript: link.
func TestUnsafeURLIsFiltered(t *testing.T) {
	m, box := newMailer(t)
	data := ForgotPasswordData{Name: "Ada", URL: "javascript:alert(1)"}
	if err := m.Send(context.Background(), "ada@example.com", ForgotPassword, data); err != nil {
		t.Fatal(err)
	}
	html := box.sent[0].HTML
	if !strings.Contains(html, `href="#ZgotmplZ"`) || strings.Contains(html, `href="javascript:`) {
		t.Fatalf("javascript: URL reached href:\n%s", html)
	}
}

func TestSendRefusesBadInput(t *testing.T) {
	m, box := newMailer(t)
	ctx := context.Background()
	data := examples[ForgotPassword]
	for _, to := range []string{"", "not an address", "Ada <ada@example.com>", "ada@example.com\r\nBcc: eve@example.com", "ada@example.com\nX: y"} {
		if err := m.Send(ctx, to, ForgotPassword, data); err == nil {
			t.Errorf("recipient %q accepted", to)
		}
	}
	if err := m.Send(ctx, "ada@example.com", Template("missing"), data); err == nil {
		t.Error("unknown template accepted")
	}
	// The wrong data struct fails to render, and nothing is sent.
	if err := m.Send(ctx, "ada@example.com", ForgotPassword, struct{ Name string }{"Ada"}); err == nil {
		t.Error("wrong data rendered")
	}
	if len(box.sent) != 0 {
		t.Fatalf("delivered %d messages", len(box.sent))
	}
}

// html/template is what escapes data; text/template would not. Only text.go,
// which renders the plain-text parts, may use it, and never for HTML.
func TestNoTextTemplate(t *testing.T) {
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range f.Imports {
			if path, _ := strconv.Unquote(spec.Path.Value); path == "text/template" && source != "text.go" {
				t.Errorf("%s imports text/template; only text.go may", source)
			}
		}
	}
	text, err := os.ReadFile("text.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(text), ".html") {
		t.Error("text.go mentions .html; it renders only .txt templates")
	}
}

func TestFileDriverIsPrivateAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail", "mail.log")
	m, err := New(&FileDriver{Path: path}, "lewcontext")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"First", "Second"} {
		if err := m.Send(context.Background(), "ada@example.com", ForgotPassword, ForgotPasswordData{Name: name, URL: "https://example.com/r"}); err != nil {
			t.Fatal(err)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mailbox mode %v", info.Mode().Perm())
	}
	content, _ := os.ReadFile(path)
	text := string(content)
	if strings.Count(text, "Subject: Reset your password") != 2 || strings.Index(text, "Hi First") > strings.Index(text, "Hi Second") {
		t.Fatalf("mailbox:\n%s", text)
	}
}

// resets is an in-memory ResetStore.
type resets struct {
	users    map[string]store.User
	issued   map[int64]string // user → token digest
	recorded []events.Event
}

func (r *resets) IssuePasswordReset(_ context.Context, email, hash string, _ time.Time) (store.User, error) {
	u, ok := r.users[email]
	if !ok {
		return u, sql.ErrNoRows
	}
	r.issued[u.ID] = hash
	return u, nil
}

func (r *resets) Record(_ context.Context, e events.Event, _ store.EventScope) error {
	r.recorded = append(r.recorded, e)
	return nil
}

func TestPasswordResetListener(t *testing.T) {
	m, box := newMailer(t)
	ada := store.User{ID: 7, PublicID: "usr_00000000000007", Name: "<b>Ada</b>", Email: "ada@example.com"}
	s := &resets{users: map[string]store.User{ada.Email: ada}, issued: map[int64]string{}}
	listen := PasswordReset(s, m, "http://localhost:3000")
	ctx := context.Background()
	now := events.Delivered{EventID: "evt_00000000000001", OccurredAt: time.Now(), Attempt: 1}

	// Unknown email: nothing issued, sent or recorded.
	if err := listen(ctx, events.PasswordResetRequested{Email: "nobody@example.com"}, now); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != 0 || len(s.issued) != 0 || len(s.recorded) != 0 {
		t.Fatal("unknown email got a link")
	}

	if err := listen(ctx, events.PasswordResetRequested{Email: ada.Email}, now); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != 1 || box.sent[0].To != ada.Email {
		t.Fatalf("sent %+v", box.sent)
	}
	html := box.sent[0].HTML
	prefix := `href="http://localhost:3000/auth/forgot-password/`
	start := strings.Index(html, prefix)
	if start < 0 {
		t.Fatalf("no link in:\n%s", html)
	}
	token := html[start+len(prefix) : start+len(prefix)+43]
	if s.issued[ada.ID] != auth.Digest(token) || strings.Contains(html, "<b>Ada") {
		t.Fatal("stored digest doesn't match the link, or the name wasn't escaped")
	}
	sent, ok := s.recorded[0].(events.PasswordResetSent)
	if len(s.recorded) != 1 || !ok || sent.User != ada.PublicID || sent.Request != now.EventID {
		t.Fatalf("recorded %+v", s.recorded)
	}

	// A rerun issues a new link, replacing the first.
	if err := listen(ctx, events.PasswordResetRequested{Email: ada.Email}, now); err != nil {
		t.Fatal(err)
	}
	if s.issued[ada.ID] == auth.Digest(token) {
		t.Fatal("rerun reused the token")
	}

	// A request older than a link's lifetime is dropped.
	stale := events.Delivered{EventID: "evt_00000000000002", OccurredAt: time.Now().Add(-ResetLinkLifetime - time.Minute)}
	if err := listen(ctx, events.PasswordResetRequested{Email: ada.Email}, stale); err != nil || len(box.sent) != 2 {
		t.Fatalf("stale request sent: %v, %d", err, len(box.sent))
	}
}

func TestMissingTemplateStopsStartup(t *testing.T) {
	subjects["not-a-file"] = "Missing"
	defer delete(subjects, "not-a-file")
	if _, err := New(&outbox{}, "lewcontext"); err == nil || !strings.Contains(err.Error(), "not-a-file.html") {
		t.Fatalf("New: %v", err)
	}
}

// invitations is an in-memory InvitationStore.
type invitations struct {
	live     map[string]store.InvitationMail // public ID → invitation
	issued   map[string]string               // public ID → token digest
	recorded []events.Event
}

func (i *invitations) IssueInvitationToken(_ context.Context, publicID, hash string) (store.InvitationMail, error) {
	inv, ok := i.live[publicID]
	if !ok {
		return inv, sql.ErrNoRows
	}
	i.issued[publicID] = hash
	return inv, nil
}

func (i *invitations) Record(_ context.Context, e events.Event, _ store.EventScope) error {
	i.recorded = append(i.recorded, e)
	return nil
}

func TestInvitationListener(t *testing.T) {
	m, box := newMailer(t)
	inv := store.InvitationMail{
		Invitation: "inv_00000000000001", Workspace: "wsp_00000000000001", WorkspaceID: 3,
		WorkspaceName: "<h1>Acme</h1>", Email: "bob@example.com", Role: auth.RoleAdmin,
		SenderName: "<script>alert(1)</script>", SenderEmail: "ada@example.com",
	}
	s := &invitations{live: map[string]store.InvitationMail{inv.Invitation: inv}, issued: map[string]string{}}
	listen := SendInvitation(s, m, "http://localhost:3000")
	ctx := context.Background()
	now := events.Delivered{EventID: "evt_00000000000001", OccurredAt: time.Now(), Attempt: 1}

	// Revoked, accepted or expired meanwhile: nothing sent or recorded.
	if err := listen(ctx, events.WorkspaceInvitationCreated{Invitation: "inv_00000000000002"}, now); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != 0 || len(s.recorded) != 0 {
		t.Fatal("a gone invitation was sent")
	}

	if err := listen(ctx, events.WorkspaceInvitationCreated{Invitation: inv.Invitation}, now); err != nil {
		t.Fatal(err)
	}
	if len(box.sent) != 1 || box.sent[0].To != inv.Email || box.sent[0].Subject != "You’ve been invited to join a workspace" {
		t.Fatalf("sent %+v", box.sent)
	}
	html := box.sent[0].HTML
	prefix := `href="http://localhost:3000/auth/invitations/`
	start := strings.Index(html, prefix)
	if start < 0 {
		t.Fatalf("no link in:\n%s", html)
	}
	token := html[start+len(prefix) : start+len(prefix)+43]
	if s.issued[inv.Invitation] != auth.Digest(token) {
		t.Fatal("stored digest doesn't match the link")
	}
	// Someone new to the app can find out what it is: a second button, and a
	// line in the plain text, both to the homepage.
	if !strings.Contains(html, `href="http://localhost:3000/"`) || !strings.Contains(html, ">Learn about lewcontext</a") {
		t.Errorf("no homepage button in:\n%s", html)
	}
	if !strings.Contains(box.sent[0].Text, "New to lewcontext? Learn what it is: http://localhost:3000/") {
		t.Errorf("no homepage line in:\n%s", box.sent[0].Text)
	}
	for _, tag := range []string{"<h1>", "<script>"} {
		if strings.Contains(html, tag) {
			t.Errorf("%s reached the email", tag)
		}
	}
	if !strings.Contains(html, "&lt;h1&gt;Acme&lt;/h1&gt;") || !strings.Contains(html, "as an admin") || !strings.Contains(html, "(ada@example.com)") {
		t.Fatalf("email:\n%s", html)
	}
	sent, ok := s.recorded[0].(events.WorkspaceInvitationSent)
	if len(s.recorded) != 1 || !ok || sent.Invitation != inv.Invitation || sent.Workspace != inv.Workspace || sent.Request != now.EventID {
		t.Fatalf("recorded %+v", s.recorded)
	}

	// A rerun issues a new link, replacing the first.
	if err := listen(ctx, events.WorkspaceInvitationCreated{Invitation: inv.Invitation}, now); err != nil {
		t.Fatal(err)
	}
	if s.issued[inv.Invitation] == auth.Digest(token) {
		t.Fatal("rerun reused the token")
	}

	// An event older than an invitation's lifetime is dropped.
	stale := events.Delivered{EventID: "evt_00000000000002", OccurredAt: time.Now().Add(-store.InvitationLifetime - time.Minute)}
	if err := listen(ctx, events.WorkspaceInvitationCreated{Invitation: inv.Invitation}, stale); err != nil || len(box.sent) != 2 {
		t.Fatalf("stale event sent: %v, %d", err, len(box.sent))
	}

	// A sender whose account is gone isn't named.
	inv.SenderName, inv.SenderEmail = "", ""
	s.live[inv.Invitation] = inv
	if err := listen(ctx, events.WorkspaceInvitationCreated{Invitation: inv.Invitation}, now); err != nil {
		t.Fatal(err)
	}
	if html := box.sent[2].HTML; !strings.Contains(html, "You were invited to join as an admin") || strings.Contains(html, "()") {
		t.Fatalf("email without a sender:\n%s", html)
	}
}
