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

// grant makes an existing account an admin. Without MFA it opens the 24-hour
// window to set it up at the first admin sign-in; running it again on such an
// admin opens a new one.
func grant(args []string) {
	address, confirm := emailFlag("grant", args)
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
		state := "off: they'll set it up at their first admin sign-in, within 24 hours"
		if user.MFAEnabledAt != nil {
			state = "on"
		}
		fmt.Printf("%s (%s, %s): two-factor authentication %s.\n", user.Email, user.Name, user.PublicID, state)
		fmt.Println("Nothing changed. Re-run with -confirm (make admin cmd=grant email=… CONFIRM=1) to make them an admin.")
		return
	}
	admin, err := repository.GrantAdmin(ctx, address)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s (%s) is an admin.\n", admin.User.Email, admin.User.PublicID)
	if admin.MFASetupUntil != nil {
		fmt.Printf("They have no two-factor authentication yet: they set it up at their first admin sign-in, before %s. Run grant again to give them another day.\n", admin.MFASetupUntil.UTC().Format("2006-01-02 15:04 UTC"))
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
		fmt.Printf("%s\t%s\t%s\tMFA %s\tadmin since %s\n", a.User.Email, a.User.Name, a.User.PublicID, mfaState(a), a.CreatedAt.UTC().Format("2006-01-02"))
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
