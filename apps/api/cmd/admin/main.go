package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"strings"
	"time"
)

// go run ./cmd/admin <command> [flags]
//
// The operator's commands, run with access to the server's environment and
// database. Allowed in production. Commands that change something preview
// unless -confirm.
func main() {
	flag.Usage = usage
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "reset-mfa":
		resetMFA(os.Args[2:])
	case "app-key":
		appKey(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

// resetMFA turns two-factor authentication off for someone who lost their
// authenticator and recovery codes, and signs out all their sessions. Check
// who's asking out of band first: this is a way around their second factor.
func resetMFA(args []string) {
	fs := flag.NewFlagSet("reset-mfa", flag.ExitOnError)
	email := fs.String("email", "", "the account's email (required)")
	confirm := fs.Bool("confirm", false, "reset; without it the command only previews")
	_ = fs.Parse(args)
	address := strings.ToLower(strings.TrimSpace(*email))
	if address == "" || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/admin reset-mfa -email <address> [-confirm]")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository := open(ctx)
	defer repository.DB.Close()

	if !*confirm {
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
}

func open(ctx context.Context) *store.Store {
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	return &store.Store{DB: db}
}

func usage() {
	out := os.Stderr
	fmt.Fprintln(out, "Usage: go run ./cmd/admin <command> [flags]")
	fmt.Fprintln(out, "\nCommands:")
	fmt.Fprintln(out, "  reset-mfa -email <address> [-confirm]   turn off two-factor authentication for someone locked out,")
	fmt.Fprintln(out, "                                          and sign out every session; previews without -confirm")
	fmt.Fprintln(out, "  app-key generate [-env path]            print a new APP_KEY, or write it into an .env file that has none")
}
