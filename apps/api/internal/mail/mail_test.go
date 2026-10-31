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
	m, err := New(box)
	if err != nil {
		t.Fatal(err)
	}
	return m, box
}

// Example data for every template, so each one is rendered at least once.
var examples = map[Template]any{
	ForgotPassword: ForgotPasswordData{Name: "Ada", URL: "http://localhost:3000/auth/forgot-password/abc"},
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
		if msg.Subject == "" || !strings.Contains(msg.HTML, "Ada") {
			t.Fatalf("%s rendered badly: %+v", msg.Template, msg)
		}
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
		for _, tag := range []string{"<script", "<h1", "<img"} {
			if strings.Contains(html, tag) {
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

// html/template is what escapes data; text/template would not.
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
			if path, _ := strconv.Unquote(spec.Path.Value); path == "text/template" {
				t.Errorf("%s imports text/template", source)
			}
		}
	}
}

func TestFileDriverIsPrivateAndAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail", "mail.log")
	m, err := New(&FileDriver{Path: path})
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
	if _, err := New(&outbox{}); err == nil || !strings.Contains(err.Error(), "not-a-file.html") {
		t.Fatalf("New: %v", err)
	}
}
