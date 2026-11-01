package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"strings"
	"time"
)

// Granting admin happens only here, by someone with the server's
// environment and database: there is no API route, page or seeder for it
// (docs/api/admin.md).

// fakeDomain is the domain of the seeders' and factories' accounts, which
// production never makes an admin.
const fakeDomain = "@example.test"

// emailFlag parses `<command> -email <address> [-confirm]`.
func emailFlag(name string, args []string) (string, bool) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	email := fs.String("email", "", "the account's email (required)")
	confirm := fs.Bool("confirm", false, "make the change; without it the command only previews")
	_ = fs.Parse(args)
	address := strings.ToLower(strings.TrimSpace(*email))
	if address == "" || fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "Usage: go run ./cmd/admin %s -email <address> [-confirm]\n", name)
		os.Exit(2)
	}
	return address, *confirm
}

// grantable refuses the seeders' fake accounts in production, so a seeded
// account (with a known password) can never become an admin there.
func grantable(address string, production bool) error {
	if production && strings.HasSuffix(address, fakeDomain) {
		return fmt.Errorf("%s is a fake account (%s): production never makes one an admin", address, fakeDomain)
	}
	return nil
}

// grantFlags parses `grant -email <address> [-role viewer|operator]
// [-confirm]`. No -role keeps an existing admin's role, and makes a new one
// a viewer.
func grantFlags(args []string) (string, auth.AdminRole, bool, error) {
	fs := flag.NewFlagSet("grant", flag.ContinueOnError)
	email := fs.String("email", "", "the account's email (required)")
	role := fs.String("role", "", "viewer (reads every admin page) or operator (may also act); without it, a new admin is a viewer and an existing one keeps their role")
	confirm := fs.Bool("confirm", false, "make the change; without it the command only previews")
	if err := fs.Parse(args); err != nil {
		return "", "", false, err
	}
	address := strings.ToLower(strings.TrimSpace(*email))
	if address == "" || fs.NArg() > 0 {
		return "", "", false, errors.New("usage: go run ./cmd/admin grant -email <address> [-role viewer|operator] [-confirm]")
	}
	if *role == "" {
		return address, "", *confirm, nil
	}
	parsed, ok := auth.ParseAdminRole(*role)
	if !ok {
		return "", "", false, fmt.Errorf("-role must be viewer or operator, not %q", *role)
	}
	return address, parsed, *confirm, nil
}

// grant makes an existing account an admin, or changes an admin's role.
// Without MFA it opens the 24-hour window to set it up at the first admin
// sign-in; running it again on such an admin (with the same role, or none)
// opens a new one. A new role on an existing admin only changes the role
// (admin.updated). There is no other way to grant admin or change a role.
func grant(args []string) {
	address, role, confirm, err := grantFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, production := open(ctx)
	defer repository.DB.Close()
	if err := grantable(address, production); err != nil {
		log.Fatal(err)
	}
	user, _, err := repository.FindUser(ctx, address)
	if errors.Is(err, sql.ErrNoRows) {
		log.Fatalf("no account uses %s: they register first, then you grant admin", address)
	}
	if err != nil {
		log.Fatal(err)
	}
	if !confirm {
		previewGrant(ctx, repository, user, role)
		return
	}
	admin, from, err := repository.GrantAdmin(ctx, address, role)
	if err != nil {
		log.Fatal(err)
	}
	if from != "" {
		fmt.Printf("%s (%s) is now an admin %s (was %s).\n", admin.User.Email, admin.User.PublicID, admin.Role, from)
		fmt.Println(roleChangeMFA(admin))
		return
	}
	fmt.Printf("%s (%s) is an admin, with the %s role.\n", admin.User.Email, admin.User.PublicID, admin.Role)
	if admin.MFASetupUntil != nil {
		fmt.Printf("They have no two-factor authentication yet: they set it up at their first admin sign-in, before %s. Run grant again to give them another day.\n", admin.MFASetupUntil.UTC().Format("2006-01-02 15:04 UTC"))
	}
}

// roleChangeMFA says where an admin whose role changes stands with MFA: a
// role change opens no setup window.
func roleChangeMFA(a store.Admin) string {
	switch {
	case a.User.MFAEnabledAt != nil:
		return "Two-factor authentication is on."
	case a.SetupOpen():
		return "Two-factor authentication is off: they can still set it up at their next admin sign-in, before " + a.MFASetupUntil.UTC().Format("2006-01-02 15:04 UTC") + " (a window already open; a role change opens none)."
	default:
		return "Two-factor authentication is off and no setup window is open, so they can't sign in to the dashboard: run grant again without -role to open one."
	}
}

// previewGrant says what grant would do, changing nothing.
func previewGrant(ctx context.Context, repository *store.Store, user store.User, role auth.AdminRole) {
	fmt.Printf("%s (%s, %s).\n", user.Email, user.Name, user.PublicID)
	existing, err := repository.FindAdmin(ctx, user.ID)
	switch {
	case errors.Is(err, store.ErrNotAdmin):
		fmt.Printf("Would make them an admin, with the %s role.\n", grantedRole(role, ""))
		fmt.Println(grantMFA(user))
	case err != nil:
		log.Fatal(err)
	case role != "" && role != existing.Role:
		fmt.Printf("They're an admin with the %s role. Would change it to %s, and nothing else.\n", existing.Role, role)
		fmt.Println(roleChangeMFA(existing))
	default:
		fmt.Printf("They're already an admin, with the %s role. Would grant it again, keeping the role.\n", existing.Role)
		fmt.Println(grantMFA(user))
	}
	fmt.Println("Nothing changed. Re-run with -confirm (make admin cmd=grant email=… CONFIRM=1) to make the change.")
}

// grantMFA says what a grant (new, or again) does about MFA.
func grantMFA(user store.User) string {
	if user.MFAEnabledAt != nil {
		return "Two-factor authentication is on."
	}
	return "Two-factor authentication is off: the grant opens a 24-hour window to set it up at their first admin sign-in."
}

// grantedRole is the role a grant gives: role, or else the role the admin
// has, or else viewer.
func grantedRole(role, existing auth.AdminRole) auth.AdminRole {
	switch {
	case role != "":
		return role
	case existing != "":
		return existing
	default:
		return auth.AdminViewer
	}
}

// revoke takes admin away and signs out the user's admin sessions. Their
// account and app sessions stay.
func revoke(args []string) {
	address, confirm := emailFlag("revoke", args)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, _ := open(ctx)
	defer repository.DB.Close()
	if !confirm {
		user, _, err := repository.FindUser(ctx, address)
		if errors.Is(err, sql.ErrNoRows) {
			log.Fatalf("no account uses %s", address)
		}
		if err != nil {
			log.Fatal(err)
		}
		if _, err := repository.FindAdmin(ctx, user.ID); errors.Is(err, store.ErrNotAdmin) {
			log.Fatalf("%s isn't an admin", address)
		} else if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s (%s, %s) is an admin.\n", user.Email, user.Name, user.PublicID)
		fmt.Println("Nothing changed. Re-run with -confirm (make admin cmd=revoke email=… CONFIRM=1) to take admin away and sign out their admin sessions.")
		return
	}
	user, revoked, err := repository.RevokeAdmin(ctx, address)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		log.Fatalf("no account uses %s", address)
	case errors.Is(err, store.ErrNotAdmin):
		log.Fatalf("%s isn't an admin", address)
	case err != nil:
		log.Fatal(err)
	}
	fmt.Printf("%s (%s) is no longer an admin; %d admin sessions signed out.\n", user.Email, user.PublicID, revoked)
}

// list prints every admin.
func list(args []string) {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/admin list")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, _ := open(ctx)
	defer repository.DB.Close()
	admins, err := repository.IndexAdmins(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if len(admins) == 0 {
		fmt.Println("No admins. Grant one with make admin cmd=grant email=… CONFIRM=1.")
		return
	}
	for _, a := range admins {
		fmt.Printf("%s\t%s\t%s\t%s\tMFA %s\tadmin since %s\n", a.User.Email, a.User.Name, a.User.PublicID, a.Role, mfaState(a), a.CreatedAt.UTC().Format("2006-01-02"))
	}
}

// mfaState is an admin's MFA as list shows it.
func mfaState(a store.Admin) string {
	switch {
	case a.User.MFAEnabledAt != nil:
		return "on"
	case a.SetupOpen():
		return "off (setup open until " + a.MFASetupUntil.UTC().Format("2006-01-02 15:04 UTC") + ")"
	default:
		return "off (can't sign in: run grant again)"
	}
}

// routeKey runs `route-key generate`: it prints a new random ADMIN_ROUTE_KEY
// (32 random bytes, base64url). It doesn't load the config, so it works
// before the key exists.
func routeKey(args []string) {
	if len(args) != 1 || args[0] != "generate" {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/admin route-key generate")
		os.Exit(2)
	}
	key, err := newRouteKey()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("ADMIN_ROUTE_KEY=" + key)
}

// newRouteKey is a random ADMIN_ROUTE_KEY: 256 bits, base64url.
func newRouteKey() (string, error) {
	return auth.Token()
}
