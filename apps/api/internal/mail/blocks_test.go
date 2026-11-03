package mail

import (
	"context"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	netmail "net/mail"
	"regexp"
	"strings"
	"testing"
	"time"
)

var blockLink = regexp.MustCompile(`http://localhost:3000/auth/email-blocks/([A-Za-z0-9_-]{43})`)

// A blockable email ends both parts with its own block link, sends it as
// List-Unsubscribe, and stores only the link's digest, for the address it
// went to.
func TestBlockableEmailCarriesABlockLink(t *testing.T) {
	m, box, blocks := blockingMailer(t)
	if err := m.Send(context.Background(), "Bob@Example.com", WorkspaceInvitation, examples[WorkspaceInvitation]); err != nil {
		t.Fatal(err)
	}
	msg := box.sent[0]
	match := blockLink.FindStringSubmatch(msg.Unsubscribe)
	if match == nil || msg.Unsubscribe != match[0] {
		t.Fatalf("Unsubscribe %q", msg.Unsubscribe)
	}
	if len(blocks.links) != 1 || blocks.links[auth.Digest(match[1])] != "bob@example.com" {
		t.Fatalf("issued %v for token %s", blocks.links, match[1])
	}
	// The link survives html/template as a real URL, not #ZgotmplZ.
	if !strings.Contains(msg.HTML, `Don’t want email from lewcontext? <a href="`+msg.Unsubscribe+`"`) || !strings.Contains(msg.HTML, ">Block this address</a>.") {
		t.Errorf("HTML footer:\n%s", msg.HTML)
	}
	if !strings.HasSuffix(msg.Text, "\n\nDon’t want email from lewcontext? Block this address: "+msg.Unsubscribe+"\n") {
		t.Errorf("plain-text footer:\n%q", msg.Text)
	}

	// Each email gets a link of its own.
	if err := m.Send(context.Background(), "bob@example.com", WorkspaceInvitation, examples[WorkspaceInvitation]); err != nil {
		t.Fatal(err)
	}
	if len(blocks.links) != 2 || box.sent[1].Unsubscribe == msg.Unsubscribe {
		t.Fatal("the second email reused the first's link")
	}
}

// Every other email has no footer, no List-Unsubscribe and issues no link.
func TestOtherEmailsCarryNoBlockLink(t *testing.T) {
	m, box, blocks := blockingMailer(t)
	for name, data := range examples {
		if blockable[name] {
			continue
		}
		if err := m.Send(context.Background(), "ada@example.com", name, data); err != nil {
			t.Fatal(err)
		}
		msg := box.sent[len(box.sent)-1]
		if msg.Unsubscribe != "" || strings.Contains(msg.HTML, "Block this address") || strings.Contains(msg.Text, "Block this address") {
			t.Errorf("%s carries a block link", name)
		}
	}
	if len(blocks.links) != 0 {
		t.Fatalf("issued %d links", len(blocks.links))
	}
}

// A blocked address gets nothing, whatever the template: ErrBlocked, and no
// link issued. So does every address while the block list can't be read, or
// a link can't be stored.
func TestBlockedAddressGetsNothing(t *testing.T) {
	m, box, blocks := blockingMailer(t)
	blocks.block("bob@example.com")
	for name, data := range examples {
		if err := m.Send(context.Background(), "Bob@example.com", name, data); !errors.Is(err, ErrBlocked) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(box.sent) != 0 || len(blocks.links) != 0 {
		t.Fatalf("sent %d, issued %d", len(box.sent), len(blocks.links))
	}

	blocks.err = errors.New("database down")
	for _, name := range []Template{ForgotPassword, WorkspaceInvitation} {
		if err := m.Send(context.Background(), "ada@example.com", name, examples[name]); err == nil || errors.Is(err, ErrBlocked) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(box.sent) != 0 {
		t.Fatalf("sent %d without the block list", len(box.sent))
	}
}

func TestNewNeedsTheBlockList(t *testing.T) {
	if _, err := New(Options{Driver: &outbox{}, App: "lewcontext", Origin: "http://localhost:3000"}); err == nil {
		t.Fatal("New made a mailer that can't check the block list")
	}
}

// The listeners treat a blocked address as done: nothing sent, nothing
// recorded, no retry. An invitation's link was issued (which set sent_at),
// so it looks sent to whoever invited.
func TestListenersSkipBlockedAddresses(t *testing.T) {
	m, box, blocks := blockingMailer(t)
	blocks.block("bob@example.com")
	ctx := context.Background()
	now := events.Delivered{EventID: "evt_00000000000001", OccurredAt: time.Now(), Attempt: 1}

	inv := store.InvitationMail{Invitation: "inv_00000000000001", Workspace: "wsp_00000000000001", WorkspaceName: "Acme", Email: "bob@example.com", Role: auth.RoleMember}
	invites := &invitations{live: map[string]store.InvitationMail{inv.Invitation: inv}, issued: map[string]string{}}
	if err := SendInvitation(invites, m, "http://localhost:3000")(ctx, events.WorkspaceInvitationCreated{Invitation: inv.Invitation}, now); err != nil {
		t.Fatal(err)
	}
	if len(invites.issued) != 1 || len(invites.recorded) != 0 {
		t.Fatalf("issued %v, recorded %v", invites.issued, invites.recorded)
	}

	signUps := newSignUps()
	signUps.requests[now.EventID] = "bob@example.com"
	if err := Registration(signUps, m, testKeys, "http://localhost:3000", true)(ctx, events.RegistrationRequested{Email: "bob@example.com"}, now); err != nil {
		t.Fatal(err)
	}
	// The sign-up still has a code, as a taken email's does: one nobody is
	// sent, so every code fails it alike.
	if signUps.codes[now.EventID] == "" || len(signUps.recorded) != 0 {
		t.Fatalf("code %q, recorded %v", signUps.codes[now.EventID], signUps.recorded)
	}
	if len(box.sent) != 0 {
		t.Fatalf("sent %d to a blocked address", len(box.sent))
	}
}

// The SMTP driver sends a blockable email's link as List-Unsubscribe, in
// its URL form only: no List-Unsubscribe-Post, whose one click would block
// without the page's confirmation.
func TestSMTPListUnsubscribe(t *testing.T) {
	s := newFakeSMTP(t, nil)
	m, _ := smtpMailer(t, s, "none", false, "", "")
	if err := m.Send(context.Background(), "bob@example.com", WorkspaceInvitation, examples[WorkspaceInvitation]); err != nil {
		t.Fatal(err)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(s.received().data))
	if err != nil {
		t.Fatal(err)
	}
	header := msg.Header.Get("List-Unsubscribe")
	if !regexp.MustCompile(`^<http://localhost:3000/auth/email-blocks/[A-Za-z0-9_-]{43}>$`).MatchString(header) || msg.Header.Get("List-Unsubscribe-Post") != "" {
		t.Fatalf("List-Unsubscribe %q, Post %q", header, msg.Header.Get("List-Unsubscribe-Post"))
	}

	if err := sendReset(m, "Ada"); err != nil {
		t.Fatal(err)
	}
	msg, err = netmail.ReadMessage(strings.NewReader(s.received().data))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("List-Unsubscribe") != "" {
		t.Fatalf("a reset link has List-Unsubscribe %q", msg.Header.Get("List-Unsubscribe"))
	}
}
