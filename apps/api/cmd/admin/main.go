package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"time"
)

// go run ./cmd/admin <command> [flags]
//
// The operator's commands, run with access to the server's environment and
// database. Allowed in production. Commands that change something preview
// unless -confirm.
func main() {
	useLogger()
	flag.Usage = usage
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "reset-mfa":
		resetMFA(os.Args[2:])
	case "grant":
		grant(os.Args[2:])
	case "revoke":
		revoke(os.Args[2:])
	case "list":
		list(os.Args[2:])
	case "blocks":
		blocks(os.Args[2:])
	case "unblock":
		unblock(os.Args[2:])
	case "app-key":
		appKey(os.Args[2:])
	case "route-key":
		routeKey(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

// resetMFA turns two-factor authentication off for someone who lost their
// authenticator and recovery codes, and signs out all their sessions. Check
// who's asking out of band first: this is a way around their second factor.
func resetMFA(args []string) {
	address, confirm := emailFlag("reset-mfa", args)
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
		state := "off (nothing to reset)"
		if user.MFAEnabledAt != nil {
			state = "on since " + user.MFAEnabledAt.UTC().Format("2006-01-02 15:04 UTC")
		}
		fmt.Printf("%s (%s, %s): two-factor authentication %s.\n", user.Email, user.Name, user.PublicID, state)
		fmt.Println("Nothing changed. Re-run with -confirm (make admin cmd=reset-mfa email=… CONFIRM=1) to turn it off and sign out every session.")
		return
	}
	user, err := repository.ResetMFA(ctx, address)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		log.Fatalf("no account uses %s", address)
	case errors.Is(err, store.ErrMFANotEnabled):
		log.Fatalf("%s has no two-factor authentication to reset", address)
	case err != nil:
		log.Fatal(err)
	}
	fmt.Printf("Two-factor authentication is off for %s (%s), and every session is signed out. They can sign in with their password and set it up again.\n", user.Email, user.PublicID)
	if admin, err := repository.FindAdmin(ctx, user.ID); err == nil && admin.MFASetupUntil != nil {
		fmt.Printf("They're an admin: they set up a new authenticator at their next admin sign-in, before %s.\n", admin.MFASetupUntil.UTC().Format("2006-01-02 15:04 UTC"))
	}
}

// open connects to the configured database, and says whether this is
// production.
func open(ctx context.Context) (*store.Store, bool) {
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	return &store.Store{DB: db}, c.Production
}

func usage() {
	out := os.Stderr
	fmt.Fprintln(out, "Usage: go run ./cmd/admin <command> [flags]")
	fmt.Fprintln(out, "\nCommands:")
	fmt.Fprintln(out, "  reset-mfa -email <address> [-confirm]   turn off two-factor authentication for someone locked out,")
	fmt.Fprintln(out, "                                          and sign out every session; previews without -confirm")
	fmt.Fprintln(out, "  grant -email <address> [-role viewer|operator] [-confirm]")
	fmt.Fprintln(out, "                                          make an existing account an admin (a viewer unless -role),")
	fmt.Fprintln(out, "                                          or change an admin's role; without MFA, they set it up at")
	fmt.Fprintln(out, "                                          their first admin sign-in, within 24 hours")
	fmt.Fprintln(out, "  revoke -email <address> [-confirm]      take admin away and sign out their admin sessions")
	fmt.Fprintln(out, "  list                                    every admin, with their role and MFA")
	fmt.Fprintln(out, "  blocks                                  every blocked email address, oldest first")
	fmt.Fprintln(out, "  unblock -email <address> [-confirm]     take an address off the block list; previews without -confirm")
	fmt.Fprintln(out, "  app-key generate [-env path]            print a new APP_KEY, or write it into an .env file that has none")
	fmt.Fprintln(out, "  route-key generate                      print a new ADMIN_ROUTE_KEY")
}

// useLogger installs the server's log handler (LOG_FORMAT, LOG_LEVEL) for
// anything logged through slog, keeping this command's own output plain
// (observe.UseForCommand).
func useLogger() {
	logging, err := config.LoadLogging()
	if err != nil {
		log.Fatal(err)
	}
	observe.UseForCommand(logging.LogFormat, logging.LogLevel)
}
