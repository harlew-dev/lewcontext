package workspace_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/brianvoe/gofakeit/v7"
	"github.com/uptrace/bun"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/factory"
	"lewcontext/api/internal/httpapi/apitest"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/httpapi/router"
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/store"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var invitationLink = regexp.MustCompile(`href="http://localhost:3000/auth/invitations/([A-Za-z0-9_-]{43})"`)

// invitationTokenIn is the token in an invitation email's link.
func invitationTokenIn(t *testing.T, m mail.Message) string {
	t.Helper()
	match := invitationLink.FindStringSubmatch(m.HTML)
	if match == nil {
		t.Fatalf("no invitation link in:\n%s", m.HTML)
	}
	return match[1]
}

func invite(email string, role auth.Role) string {
	return fmt.Sprintf(`{"email":%q,"role":%q}`, email, role)
}

// memberWithSession adds a new user to the workspace as role, signed in.
func memberWithSession(t *testing.T, db bun.IDB, f *gofakeit.Faker, workspaceID int64, role auth.Role) factory.Session {
	t.Helper()
	session := apitest.SignedIn(t, db, f)
	apitest.Must(factory.MemberOf(context.Background(), db, f, session.UserID, workspaceID, role))(t)
	return session
}

// sessionFor signs a new account with email in.
func sessionFor(t *testing.T, db bun.IDB, f *gofakeit.Faker, email string) (factory.User, factory.Session) {
	t.Helper()
	ctx := context.Background()
	user := apitest.Must(factory.Users.State(func(u *factory.User) { u.Email = email }).CreateOne(ctx, db, f))(t)
	return user, apitest.Must(factory.SessionFor(ctx, db, f, user.ID))(t)
}

// roleIn is userID's role in the workspace, or "" when they aren't a member.
func roleIn(t *testing.T, db bun.IDB, workspaceID, userID int64) auth.Role {
	t.Helper()
	var role auth.Role
	_ = db.NewSelect().TableExpr("workspace_user").Column("role").
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Scan(context.Background(), &role)
	return role
}

func countInvitations(t *testing.T, db bun.IDB, workspaceID int64) int {
	t.Helper()
	n, err := db.NewSelect().TableExpr("workspace_invitations").Where("workspace_id = ?", workspaceID).Count(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

func TestCreateInvitation(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	path := apitest.WorkspacePath(workspace.PublicID, "/invitations")
	ownerID := apitest.PublicIDOf(t, s.Repo.DB, owner.UserID)

	t.Run("owners invite any role but owner", func(t *testing.T) {
		for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleMember, auth.RoleViewer} {
			w := apitest.Call(t, h, "POST", path, invite("new-"+string(role)+"@example.com", role), owner.CSRFToken, owner.Cookie(), 201)
			var got map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			by, _ := got["invitedBy"].(map[string]any)
			if !input.PublicID("inv", fmt.Sprint(got["id"])) || got["role"] != string(role) || got["sentAt"] != nil || by["id"] != ownerID || apitest.NumericID.MatchString(w.Body.String()) {
				t.Fatalf("invitation: %s", w.Body.String())
			}
		}
		refused := apitest.Call(t, h, "POST", path, invite("boss@example.com", auth.RoleOwner), owner.CSRFToken, owner.Cookie(), 422)
		if !strings.Contains(refused.Body.String(), `"field":"role"`) {
			t.Fatalf("owner invitation: %s", refused.Body.String())
		}
		apitest.Call(t, h, "POST", path, invite("not an email", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 422)
		apitest.Call(t, h, "POST", path, `{"email":"a@example.com","role":"member","workspace":"wsp_00000000000001"}`, owner.CSRFToken, owner.Cookie(), 400)
	})
	t.Run("the event is recorded with it", func(t *testing.T) {
		created := apitest.EventsNamed(t, db, "workspace.invitation.created")
		e := created[0]
		if len(created) != 3 || !apitest.Is(e.ActorID, owner.UserID) || !apitest.Is(e.WorkspaceID, workspace.ID) || e.Payload["email"] != "new-admin@example.com" || e.Payload["role"] != "admin" || e.Payload["workspace"] != workspace.PublicID {
			t.Fatalf("created: %+v", created)
		}
	})
	t.Run("admins invite members and viewers only; others can't invite", func(t *testing.T) {
		admin := memberWithSession(t, db, f, workspace.ID, auth.RoleAdmin)
		apitest.Call(t, h, "POST", path, invite("by-admin@example.com", auth.RoleViewer), admin.CSRFToken, admin.Cookie(), 201)
		apitest.Call(t, h, "POST", path, invite("another-admin@example.com", auth.RoleAdmin), admin.CSRFToken, admin.Cookie(), 403)
		for _, role := range []auth.Role{auth.RoleMember, auth.RoleViewer} {
			session := memberWithSession(t, db, f, workspace.ID, role)
			apitest.Call(t, h, "POST", path, invite("by-"+string(role)+"@example.com", auth.RoleViewer), session.CSRFToken, session.Cookie(), 403)
		}
	})
	t.Run("members and live invitations are refused; expired ones replaced", func(t *testing.T) {
		member := memberWithSession(t, db, f, workspace.ID, auth.RoleMember)
		w := apitest.Call(t, h, "POST", path, invite(apitest.EmailOf(t, db, member.UserID), auth.RoleViewer), owner.CSRFToken, owner.Cookie(), 422)
		if !strings.Contains(w.Body.String(), "Already a member") {
			t.Fatalf("member: %s", w.Body.String())
		}
		w = apitest.Call(t, h, "POST", path, invite("new-member@example.com", auth.RoleViewer), owner.CSRFToken, owner.Cookie(), 422)
		if !strings.Contains(w.Body.String(), "Already invited") || !strings.Contains(w.Body.String(), `"field":"email"`) {
			t.Fatalf("duplicate: %s", w.Body.String())
		}
		apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.InvitedAs("lapsed@example.com", auth.RoleMember)).State(factory.ExpiredInvitation).CreateOne(ctx, db, f))(t)
		apitest.Call(t, h, "POST", path, invite("lapsed@example.com", auth.RoleViewer), owner.CSRFToken, owner.Cookie(), 201)
		n, err := db.NewSelect().TableExpr("workspace_invitations").Where("email = 'lapsed@example.com' AND expires_at > now()").Count(ctx)
		if err != nil || n != 1 || countInvitations(t, db, workspace.ID) != 5 {
			t.Fatalf("lapsed invitation not replaced: %d (%v)", n, err)
		}
	})
	t.Run("the answer doesn't say whether the email has an account", func(t *testing.T) {
		registered := apitest.Must(factory.Users.CreateOne(ctx, db, f))(t)
		shape := func(email string) []string {
			var got map[string]any
			_ = json.Unmarshal(apitest.Call(t, h, "POST", path, invite(email, auth.RoleViewer), owner.CSRFToken, owner.Cookie(), 201).Body.Bytes(), &got)
			keys := make([]string, 0, len(got))
			for k := range got {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			return keys
		}
		if a, b := shape(registered.Email), shape("nobody-yet@example.com"); !slices.Equal(a, b) {
			t.Fatalf("%v vs %v", a, b)
		}
	})
	t.Run("the list, newest first", func(t *testing.T) {
		var page store.Page[map[string]any]
		if err := json.Unmarshal(apitest.Call(t, h, "GET", path, "", "", owner.Cookie(), 200).Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if page.Pages.Items != 7 || page.Data[0]["email"] != "nobody-yet@example.com" {
			t.Fatalf("list: %+v", page)
		}
	})
}
func TestInvitationCaps(t *testing.T) {
	ctx := context.Background()
	t.Run("50 pending", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		owner := apitest.SignedIn(t, db, f)
		workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
		apitest.Must(factory.InvitationsTo(workspace.ID).Create(ctx, db, f, store.MaxPendingInvitations))(t)
		// Expired ones don't count.
		apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.ExpiredInvitation).Create(ctx, db, f, 3))(t)
		w := apitest.Call(t, router.New(s), "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), invite("one-more@example.com", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 422)
		if !strings.Contains(w.Body.String(), "up to 50 pending invitations") {
			t.Fatalf("refusal: %s", w.Body.String())
		}
	})
	t.Run("100 members, counting pending invitations", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		owner := apitest.SignedIn(t, db, f)
		workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
		users := apitest.Must(factory.Users.Create(ctx, db, f, 59))(t)
		for _, u := range users {
			apitest.Must(factory.MemberOf(ctx, db, f, u.ID, workspace.ID, auth.RoleViewer))(t)
		}
		apitest.Must(factory.InvitationsTo(workspace.ID).Create(ctx, db, f, 39))(t)
		path := apitest.WorkspacePath(workspace.PublicID, "/invitations")
		ninetyNinth := apitest.Call(t, router.New(s), "POST", path, invite("ninety-ninth@example.com", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 201)
		w := apitest.Call(t, router.New(s), "POST", path, invite("hundred-and-first@example.com", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 422)
		if !strings.Contains(w.Body.String(), "up to 100 members, counting pending invitations") || strings.Contains(w.Body.String(), "field") {
			t.Fatalf("refusal: %s", w.Body.String())
		}
		// Accepting turns an invitation into a member, so it isn't refused at 100.
		sent := apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.Sent).State(factory.InvitedAs("late@example.com", auth.RoleMember)).CreateOne(ctx, db, f))(t)
		var revoke map[string]any
		_ = json.Unmarshal(ninetyNinth.Body.Bytes(), &revoke)
		apitest.Call(t, router.New(s), "DELETE", path+"/"+fmt.Sprint(revoke["id"]), "", owner.CSRFToken, owner.Cookie(), 204)
		_, late := sessionFor(t, db, f, "late@example.com")
		apitest.Call(t, router.New(s), "POST", "/api/invitations/accept", apitest.TokenBody(sent.Token), late.CSRFToken, late.Cookie(), 200)
		// Full without this invitation (filled some other way): accepting is refused.
		over := apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.Sent).State(factory.InvitedAs("over@example.com", auth.RoleMember)).CreateOne(ctx, db, f))(t)
		_, overSession := sessionFor(t, db, f, "over@example.com")
		w = apitest.Call(t, router.New(s), "POST", "/api/invitations/accept", apitest.TokenBody(over.Token), overSession.CSRFToken, overSession.Cookie(), 422)
		if !strings.Contains(w.Body.String(), "This workspace is full") {
			t.Fatalf("accept over the quota: %s", w.Body.String())
		}
	})
	t.Run("limits, per user before everyone", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		owner := apitest.SignedIn(t, db, f)
		workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
		path := apitest.WorkspacePath(workspace.PublicID, "/invitations")
		apitest.Must(factory.RateLimits.State(factory.ForLimit("invitation:per-user", apitest.PublicIDOf(t, s.Repo.DB, owner.UserID))).State(factory.Exhausted(20)).CreateOne(ctx, db, f))(t)
		refused := apitest.Call(t, router.New(s), "POST", path, invite("a@example.com", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 429)
		if !strings.Contains(refused.Body.String(), "Too many requests") {
			t.Fatalf("refusal: %s", refused.Body.String())
		}
		apitest.Reached(t, s.Repo.DB, "invitation:per-user:block")
		if apitest.CountRateLimits(t, s.Repo.DB, "invitation:per-email:%") != 0 || apitest.CountRateLimits(t, s.Repo.DB, "invitation:global") != 0 {
			t.Fatal("refused per user, but counted for the address or for everyone")
		}
		other := apitest.SignedIn(t, db, f)
		otherWorkspace := apitest.OwnedWorkspace(t, db, f, other.UserID)
		apitest.Must(factory.RateLimits.State(factory.ForLimit("invitation:global", "")).State(factory.Exhausted(200)).CreateOne(ctx, db, f))(t)
		busy := apitest.Call(t, router.New(s), "POST", apitest.WorkspacePath(otherWorkspace.PublicID, "/invitations"), invite("b@example.com", auth.RoleMember), other.CSRFToken, other.Cookie(), 503)
		if !strings.Contains(busy.Body.String(), "Invitations are busy") || countInvitations(t, db, otherWorkspace.ID) != 0 {
			t.Fatalf("busy: %s", busy.Body.String())
		}
	})
	t.Run("per email, across inviters and workspaces", func(t *testing.T) {
		s, db := server(t)
		f := factory.TestFaker(t)
		// Five owners of five workspaces invite one address, which has no account.
		for range 5 {
			owner := apitest.SignedIn(t, db, f)
			workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
			apitest.Call(t, router.New(s), "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), invite("target@example.com", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 201)
		}
		sixth := apitest.SignedIn(t, db, f)
		workspace := apitest.OwnedWorkspace(t, db, f, sixth.UserID)
		path := apitest.WorkspacePath(workspace.PublicID, "/invitations")
		refused := apitest.Call(t, router.New(s), "POST", path, invite("target@example.com", auth.RoleMember), sixth.CSRFToken, sixth.Cookie(), 429)
		if !strings.Contains(refused.Body.String(), "This email address has had too many invitations recently. Try again in") {
			t.Fatalf("refusal: %s", refused.Body.String())
		}
		apitest.RetryWithin(t, refused, time.Hour)
		apitest.Reached(t, s.Repo.DB, "invitation:per-email:block")
		var everyone int
		if err := db.NewSelect().TableExpr("rate_limits").Column("hits").Where("key = ?", "invitation:global").Scan(ctx, &everyone); err != nil {
			t.Fatal(err)
		}
		if countInvitations(t, db, workspace.ID) != 0 || everyone != 5 {
			t.Fatalf("the refused invitation was created, or counted for everyone (%d hits)", everyone)
		}
		// Other addresses are unaffected.
		apitest.Call(t, router.New(s), "POST", path, invite("someone-else@example.com", auth.RoleMember), sixth.CSRFToken, sixth.Cookie(), 201)
	})
	t.Run("no email delivery", func(t *testing.T) {
		s, db := server(t)
		s.Config.MailDriver = config.MailOff
		f := factory.TestFaker(t)
		owner := apitest.SignedIn(t, db, f)
		workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
		w := apitest.Call(t, router.New(s), "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), invite("a@example.com", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 503)
		if !strings.Contains(w.Body.String(), "aren’t available on this server") || countInvitations(t, db, workspace.ID) != 0 {
			t.Fatalf("mail off: %s", w.Body.String())
		}
	})
}

// From invitation to membership: the email, the link's page, accepting.
func TestInvitationLink(t *testing.T) {
	ctx := context.Background()
	s, db, box, deliver := mailServer(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	path := apitest.WorkspacePath(workspace.PublicID, "/invitations")
	apitest.Call(t, h, "POST", path, invite("bob@example.com", auth.RoleMember), owner.CSRFToken, owner.Cookie(), 201)
	deliver()
	if len(box.Sent) != 1 || box.Sent[0].To != "bob@example.com" {
		t.Fatalf("sent %+v", box.Sent)
	}
	token := invitationTokenIn(t, box.Sent[0])
	var digest string
	var sentAt bool
	if err := db.NewSelect().TableExpr("workspace_invitations").ColumnExpr("token_hash, sent_at IS NOT NULL").Scan(ctx, &digest, &sentAt); err != nil {
		t.Fatal(err)
	}
	if digest != auth.Digest(token) || !sentAt {
		t.Fatalf("stored %q, sent %v", digest, sentAt)
	}
	if e := apitest.OnlyEvent(t, db, "workspace.invitation.sent"); !apitest.Is(e.WorkspaceID, workspace.ID) || e.Payload["request"] == nil || strings.Contains(e.Raw, token) {
		t.Fatalf("sent: %+v", e)
	}

	t.Run("check shows it, signed out", func(t *testing.T) {
		var preview map[string]any
		_ = json.Unmarshal(apitest.Call(t, h, "POST", "/api/invitations/check", apitest.TokenBody(token), "", nil, 200).Body.Bytes(), &preview)
		ws, _ := preview["workspace"].(map[string]any)
		by, _ := preview["invitedBy"].(map[string]any)
		if ws["name"] != workspace.Name || by["email"] != apitest.EmailOf(t, db, owner.UserID) || preview["email"] != "bob@example.com" || preview["role"] != "member" {
			t.Fatalf("preview: %+v", preview)
		}
		for _, bad := range []string{"short", strings.Repeat("A", 43)} {
			w := apitest.Call(t, h, "POST", "/api/invitations/check", apitest.TokenBody(bad), "", nil, 422)
			if !strings.Contains(w.Body.String(), `"field":"token"`) {
				t.Fatalf("bad token: %s", w.Body.String())
			}
		}
	})
	t.Run("signing up or in with the invited email joins nothing", func(t *testing.T) {
		w := apitest.Call(t, h, "POST", "/api/auth/register", `{"name":"Bob","email":"bob@example.com","password":"correct horse battery staple"}`, "", nil, 201)
		apitest.Call(t, h, "POST", "/api/auth/login", `{"email":"bob@example.com","password":"correct horse battery staple"}`, "", nil, 200)
		var bob apitest.SessionResponse
		_ = json.Unmarshal(w.Body.Bytes(), &bob)
		if role := roleIn(t, db, workspace.ID, apitest.IDOf(t, db, "users", bob.User.PublicID)); role != "" {
			t.Fatalf("joined as %s without the link", role)
		}
	})
	t.Run("another account can't accept it", func(t *testing.T) {
		eve := apitest.SignedIn(t, db, f)
		w := apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(token), eve.CSRFToken, eve.Cookie(), 403)
		if !strings.Contains(w.Body.String(), "different email") || countInvitations(t, db, workspace.ID) != 1 || roleIn(t, db, workspace.ID, eve.UserID) != "" {
			t.Fatalf("accepted by someone else: %s", w.Body.String())
		}
		apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(token), "", nil, 401)
		apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(token), "wrong", eve.Cookie(), 403)
	})
	t.Run("the invitee accepts once", func(t *testing.T) {
		bob := apitest.Must(factory.SessionFor(ctx, db, f, apitest.IDOf(t, db, "users", userPublicIDByEmail(t, db, "bob@example.com"))))(t)
		w := apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(token), bob.CSRFToken, bob.Cookie(), 200)
		var joined store.Workspace
		_ = json.Unmarshal(w.Body.Bytes(), &joined)
		if joined.PublicID != workspace.PublicID || joined.Role != auth.RoleMember || roleIn(t, db, workspace.ID, bob.UserID) != auth.RoleMember || countInvitations(t, db, workspace.ID) != 0 {
			t.Fatalf("accepted: %s", w.Body.String())
		}
		e := apitest.OnlyEvent(t, db, "workspace.invitation.accepted")
		if !apitest.Is(e.ActorID, bob.UserID) || !apitest.Is(e.UserID, bob.UserID) || !apitest.Is(e.WorkspaceID, workspace.ID) || e.Payload["role"] != "member" {
			t.Fatalf("accepted: %+v", e)
		}
		apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(token), bob.CSRFToken, bob.Cookie(), 422)
		apitest.Call(t, h, "POST", "/api/invitations/check", apitest.TokenBody(token), "", nil, 422)
	})
	t.Run("revoked before sending: no email", func(t *testing.T) {
		w := apitest.Call(t, h, "POST", path, invite("carol@example.com", auth.RoleViewer), owner.CSRFToken, owner.Cookie(), 201)
		var inv map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &inv)
		apitest.Call(t, h, "DELETE", path+"/"+fmt.Sprint(inv["id"]), "", owner.CSRFToken, owner.Cookie(), 204)
		deliver()
		if len(box.Sent) != 1 {
			t.Fatalf("a revoked invitation was sent: %d", len(box.Sent))
		}
	})
	t.Run("a newer email replaces the link", func(t *testing.T) {
		inv := apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.Sent).CreateOne(ctx, db, f))(t)
		if _, err := s.Repo.IssueInvitationToken(ctx, inv.PublicID, auth.Digest("a-newer-token")); err != nil {
			t.Fatal(err)
		}
		apitest.Call(t, h, "POST", "/api/invitations/check", apitest.TokenBody(inv.Token), "", nil, 422)
	})
}

// userPublicIDByEmail is the usr_ ID of the account using email.
func userPublicIDByEmail(t *testing.T, db bun.IDB, email string) string {
	t.Helper()
	var id string
	if err := db.NewSelect().TableExpr("users").Column("public_id").Where("email = ?", email).Scan(context.Background(), &id); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestAcceptInvitation(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	sent := func(email string, role auth.Role, states ...func(*factory.WorkspaceInvitation)) factory.WorkspaceInvitation {
		build := factory.InvitationsTo(workspace.ID).State(factory.Sent).State(factory.InvitedAs(email, role))
		for _, state := range states {
			build = build.State(state)
		}
		return apitest.Must(build.CreateOne(ctx, db, f))(t)
	}

	t.Run("expired", func(t *testing.T) {
		inv := sent("late@example.com", auth.RoleMember, factory.ExpiredInvitation)
		_, late := sessionFor(t, db, f, "late@example.com")
		apitest.Call(t, h, "POST", "/api/invitations/check", apitest.TokenBody(inv.Token), "", nil, 422)
		apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(inv.Token), late.CSRFToken, late.Cookie(), 422)
	})
	t.Run("a member keeps their role", func(t *testing.T) {
		inv := sent("already@example.com", auth.RoleViewer)
		user, session := sessionFor(t, db, f, "already@example.com")
		apitest.Must(factory.MemberOf(ctx, db, f, user.ID, workspace.ID, auth.RoleAdmin))(t)
		apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(inv.Token), session.CSRFToken, session.Cookie(), 200)
		if roleIn(t, db, workspace.ID, user.ID) != auth.RoleAdmin {
			t.Fatal("accepting changed an existing member's role")
		}
	})
	t.Run("two accepts at once: one wins", func(t *testing.T) {
		for range 5 {
			email := fmt.Sprintf("twice-%s@example.com", strings.ToLower(f.LetterN(8)))
			inv := sent(email, auth.RoleMember)
			_, session := sessionFor(t, db, f, email)
			statuses := concurrently(2, func(int) int {
				return apitest.Send(h, "POST", "/api/invitations/accept", apitest.TokenBody(inv.Token), session.CSRFToken, session.Cookie()).Code
			})
			slices.Sort(statuses)
			if !slices.Equal(statuses, []int{200, 422}) {
				t.Fatalf("two accepts: %v", statuses)
			}
		}
	})
	t.Run("accept racing revoke", func(t *testing.T) {
		for range 5 {
			email := fmt.Sprintf("race-%s@example.com", strings.ToLower(f.LetterN(8)))
			inv := sent(email, auth.RoleMember)
			user, session := sessionFor(t, db, f, email)
			statuses := concurrently(2, func(i int) int {
				if i == 0 {
					return apitest.Send(h, "POST", "/api/invitations/accept", apitest.TokenBody(inv.Token), session.CSRFToken, session.Cookie()).Code
				}
				return apitest.Send(h, "DELETE", apitest.WorkspacePath(workspace.PublicID, "/invitations/"+inv.PublicID), "", owner.CSRFToken, owner.Cookie()).Code
			})
			joined := roleIn(t, db, workspace.ID, user.ID) != ""
			if !(statuses[0] == 200 && statuses[1] == 404 && joined || statuses[0] == 422 && statuses[1] == 204 && !joined) {
				t.Fatalf("accept %d, revoke %d, joined %v", statuses[0], statuses[1], joined)
			}
		}
	})
	t.Run("the inviter removed meanwhile", func(t *testing.T) {
		admin := memberWithSession(t, db, f, workspace.ID, auth.RoleAdmin)
		w := apitest.Call(t, h, "POST", apitest.WorkspacePath(workspace.PublicID, "/invitations"), invite("from-admin@example.com", auth.RoleViewer), admin.CSRFToken, admin.Cookie(), 201)
		var inv map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &inv)
		token := apitest.Must(auth.Token())(t)
		if _, err := s.Repo.IssueInvitationToken(ctx, fmt.Sprint(inv["id"]), auth.Digest(token)); err != nil {
			t.Fatal(err)
		}
		apitest.Call(t, h, "DELETE", apitest.WorkspacePath(workspace.PublicID, "/members/"+apitest.PublicIDOf(t, s.Repo.DB, admin.UserID)), "", owner.CSRFToken, owner.Cookie(), 204)
		user, session := sessionFor(t, db, f, "from-admin@example.com")
		apitest.Call(t, h, "POST", "/api/invitations/accept", apitest.TokenBody(token), session.CSRFToken, session.Cookie(), 200)
		if roleIn(t, db, workspace.ID, user.ID) != auth.RoleViewer {
			t.Fatal("not joined")
		}
	})
}

// concurrently runs n calls of fn at once and returns their results in order.
func concurrently(n int, fn func(i int) int) []int {
	results := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() { results[i] = fn(i) })
	}
	wg.Wait()
	return results
}
func TestRevokeInvitation(t *testing.T) {
	ctx := context.Background()
	s, db := server(t)
	h := router.New(s)
	f := factory.TestFaker(t)
	owner := apitest.SignedIn(t, db, f)
	workspace := apitest.OwnedWorkspace(t, db, f, owner.UserID)
	inv := apitest.Must(factory.InvitationsTo(workspace.ID).State(factory.Sent).State(factory.InvitedAs("dan@example.com", auth.RoleAdmin)).CreateOne(ctx, db, f))(t)
	// Another workspace's invitation is not found.
	other := apitest.OwnedWorkspace(t, db, f, apitest.SignedIn(t, db, f).UserID)
	elsewhere := apitest.Must(factory.InvitationsTo(other.ID).CreateOne(ctx, db, f))(t)
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(workspace.PublicID, "/invitations/"+elsewhere.PublicID), "", owner.CSRFToken, owner.Cookie(), 404)
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(workspace.PublicID, "/invitations/inv_bad"), "", owner.CSRFToken, owner.Cookie(), 404)
	// An admin can't revoke an invitation to a role they couldn't give.
	admin := memberWithSession(t, db, f, workspace.ID, auth.RoleAdmin)
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(workspace.PublicID, "/invitations/"+inv.PublicID), "", admin.CSRFToken, admin.Cookie(), 403)
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(workspace.PublicID, "/invitations/"+inv.PublicID), "", owner.CSRFToken, owner.Cookie(), 204)
	e := apitest.OnlyEvent(t, db, "workspace.invitation.revoked")
	if !apitest.Is(e.ActorID, owner.UserID) || e.Payload["email"] != "dan@example.com" || e.Payload["invitation"] != inv.PublicID {
		t.Fatalf("revoked: %+v", e)
	}
	apitest.Call(t, h, "POST", "/api/invitations/check", apitest.TokenBody(inv.Token), "", nil, 422)
	apitest.Call(t, h, "DELETE", apitest.WorkspacePath(workspace.PublicID, "/invitations/"+inv.PublicID), "", owner.CSRFToken, owner.Cookie(), 404)
}
