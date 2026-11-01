package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// Suspending from the command line (docs/api/admin.md#suspending-an-account):
// as the dashboard's Suspend, for any account but an admin (revoke admin
// first), recording auth.suspended with by: cli and the reason.

// suspensionFlags parses `<command> -email <address> -reason <why>
// [-confirm]`. The reason is 10 to 500 characters, as the dashboard's.
func suspensionFlags(name string, args []string) (string, string, bool, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	email := fs.String("email", "", "the account's email (required)")
	reason := fs.String("reason", "", "why, 10 to 500 characters (required), kept in the audit trail")
	confirm := fs.Bool("confirm", false, "make the change; without it the command only previews")
	if err := fs.Parse(args); err != nil {
		return "", "", false, err
	}
	address := strings.ToLower(strings.TrimSpace(*email))
	why := strings.TrimSpace(*reason)
	if address == "" || fs.NArg() > 0 {
		return "", "", false, fmt.Errorf("usage: go run ./cmd/admin %s -email <address> -reason <why> [-confirm]", name)
	}
	if n := utf8.RuneCountInString(why); n < 10 || n > 500 {
		return "", "", false, errors.New("give a -reason of 10 to 500 characters")
	}
	return address, why, *confirm, nil
}

// suspend runs `suspend -email … -reason … [-confirm]`.
func suspend(args []string) {
	runSuspension("suspend", args, suspendAccount)
}

// unsuspend runs `unsuspend -email … -reason … [-confirm]`.
func unsuspend(args []string) {
	runSuspension("unsuspend", args, unsuspendAccount)
}

func runSuspension(name string, args []string, run func(context.Context, *store.Store, string, string, bool, io.Writer) error) {
	address, reason, confirm, err := suspensionFlags(name, args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, _ := open(ctx)
	defer repository.DB.Close()
	if err := run(ctx, repository, address, reason, confirm, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// suspendAccount previews or, with confirm, suspends the account using
// address: every session ends and its live invitations are revoked.
func suspendAccount(ctx context.Context, repository *store.Store, address, reason string, confirm bool, out io.Writer) error {
	user, _, err := repository.FindUser(ctx, address)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no account uses %s", address)
	}
	if err != nil {
		return err
	}
	if !confirm {
		fmt.Fprintf(out, "Would suspend %s (%s, %s): every session signed out, their live invitations revoked, and sign-in refused until unsuspended.\n", user.Email, user.Name, user.PublicID)
		fmt.Fprintln(out, "Nothing changed. Re-run with -confirm (make admin cmd=suspend email=… reason=\"…\" CONFIRM=1) to suspend it.")
		return nil
	}
	done, err := repository.SuspendUser(ctx, user.PublicID, store.Suspension{Reason: reason, RevokeInvitations: true}, nil)
	switch {
	case errors.Is(err, store.ErrActionOnAdmin):
		return fmt.Errorf("%s is an admin: revoke admin first (admin revoke), then suspend", address)
	case errors.Is(err, store.ErrAlreadySuspended):
		return fmt.Errorf("%s is already suspended", address)
	case err != nil:
		return err
	}
	fmt.Fprintf(out, "%s is suspended: %d sessions signed out, %d invitations revoked.\n", address, done.SessionsRevoked, done.InvitationsRevoked)
	return nil
}

// unsuspendAccount previews or, with confirm, lifts the suspension of the
// account using address.
func unsuspendAccount(ctx context.Context, repository *store.Store, address, reason string, confirm bool, out io.Writer) error {
	user, _, err := repository.FindUser(ctx, address)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no account uses %s", address)
	}
	if err != nil {
		return err
	}
	if !user.Suspended {
		return fmt.Errorf("%s isn't suspended: nothing to do", address)
	}
	if !confirm {
		fmt.Fprintf(out, "Would unsuspend %s (%s, %s): they can sign in again.\n", user.Email, user.Name, user.PublicID)
		fmt.Fprintln(out, "Nothing changed. Re-run with -confirm (make admin cmd=unsuspend email=… reason=\"…\" CONFIRM=1) to unsuspend it.")
		return nil
	}
	if _, err := repository.UnsuspendUser(ctx, user.PublicID, reason, nil); errors.Is(err, store.ErrNotSuspended) {
		return fmt.Errorf("%s isn't suspended: nothing to do", address)
	} else if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s is unsuspended: they can sign in again.\n", address)
	return nil
}
