package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"time"
)

// Signing everyone out (plans/features/admin/actions.md, decision 9): only
// here, never from the dashboard. For a suspected leak of the session
// tables; part of the incident runbook.

// sessions runs `sessions revoke-all [-confirm]`.
func sessions(args []string) {
	if len(args) == 0 || args[0] != "revoke-all" {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/admin sessions revoke-all [-confirm]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("sessions revoke-all", flag.ExitOnError)
	confirm := fs.Bool("confirm", false, "sign everyone out; without it the command only previews")
	_ = fs.Parse(args[1:])
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/admin sessions revoke-all [-confirm]")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	repository, _ := open(ctx)
	defer repository.DB.Close()
	if err := revokeAllSessions(ctx, repository, *confirm, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// revokeAllSessions previews (how many app and admin sessions there are)
// or, with confirm, deletes every one and records
// auth.sessions_revoked_all.
func revokeAllSessions(ctx context.Context, repository *store.Store, confirm bool, out io.Writer) error {
	if !confirm {
		sessions, adminSessions, err := repository.CountSessions(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Would sign everyone out: %d app sessions and %d admin sessions.\n", sessions, adminSessions)
		fmt.Fprintln(out, "Nothing changed. Re-run with -confirm (make admin cmd=\"sessions revoke-all\" CONFIRM=1) to do it.")
		return nil
	}
	revoked, err := repository.RevokeAllSessions(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Signed everyone out: %d app sessions and %d admin sessions. Everyone signs in again with their password (and code).\n", revoked.Sessions, revoked.AdminSessions)
	return nil
}
