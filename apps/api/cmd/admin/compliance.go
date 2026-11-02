package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"strings"
	"time"
)

// Requests handled by staff (docs/compliance.md): erasing an account and
// exporting its data for someone who can't sign in to do it themselves.
// Check who is asking, outside the app, first: each one is a way round the
// account's password.

// openWithKeys is open, with the keys derived from APP_KEY (erasure finds
// events already minimised by the email's pseudonym).
func openWithKeys(ctx context.Context) (*store.Store, *auth.Keys) {
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	keys, err := auth.NewKeys(c.AppKey)
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	return &store.Store{DB: db}, keys
}

// erase runs `erase -email … [-confirm]`.
func erase(args []string) {
	address, confirm := emailFlag("erase", args)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	repository, keys := openWithKeys(ctx)
	defer repository.DB.Close()
	if err := eraseAccount(ctx, repository, keys.EmailPseudonym, address, confirm, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// eraseAccount previews or, with confirm, erases the account using address,
// as deleting it from Account settings would: the account, its memberships
// and the events about it go, and its email (and name) is replaced in every
// other event. The workspaces only it belongs to go with it. It refuses an
// admin, and the only owner of a workspace with other members, naming
// those workspaces.
func eraseAccount(ctx context.Context, repository *store.Store, pseudonym func(string) string, address string, confirm bool, out io.Writer) error {
	address = strings.ToLower(strings.TrimSpace(address))
	user, _, err := repository.FindUser(ctx, address)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no account uses %s", address)
	}
	if err != nil {
		return err
	}
	if !confirm {
		fmt.Fprintf(out, "Would erase %s (%s, %s): the account, its memberships, its sessions and the events about it, with the workspaces only it belongs to; its email and name are replaced with %q in every other event. It can't be undone.\n", user.Email, user.Name, user.PublicID, store.Redacted)
		fmt.Fprintln(out, "Refused for an admin (revoke first), and for the only owner of a workspace with other members (make someone else an owner first).")
		fmt.Fprintln(out, "Nothing changed. Re-run with -confirm (make admin cmd=erase email=… CONFIRM=1) once you've confirmed who is asking.")
		return nil
	}
	done, err := repository.EraseAccount(ctx, address, pseudonym)
	var lastOwner store.LastOwnerError
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("no account uses %s", address)
	case errors.Is(err, store.ErrActionOnAdmin):
		return fmt.Errorf("%s is an admin: revoke admin first (admin revoke -email … -confirm)", address)
	case errors.As(err, &lastOwner):
		names := make([]string, len(lastOwner.Workspaces))
		for i, w := range lastOwner.Workspaces {
			names[i] = fmt.Sprintf("%s (%s)", w.Name, w.PublicID)
		}
		return fmt.Errorf("%s is the only owner of workspaces with other members: %s. Make someone else an owner of each first", address, strings.Join(names, ", "))
	case err != nil:
		return err
	}
	fmt.Fprintf(out, "Erased %s (%s), with %d workspaces only it belonged to. Its email is gone from the events that remain; backups and shipped logs keep it until they expire.\n", address, done.User, done.Workspaces)
	return nil
}

// export runs `export -email …`, writing the file to standard output.
func export(args []string) {
	address, _ := emailFlag("export", args)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	repository, _ := open(ctx)
	defer repository.DB.Close()
	if err := exportAccount(ctx, repository, address, os.Stdout); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "Exported %s's data. Send the file only to the person, by a channel you've checked is theirs.\n", address)
}

// exportAccount writes the data of the account using address, as "Download
// my data" would (store.FindAccountExport), and records admin.user.exported
// with no actor before writing it.
func exportAccount(ctx context.Context, repository *store.Store, address string, out io.Writer) error {
	address = strings.ToLower(strings.TrimSpace(address))
	user, _, err := repository.FindUser(ctx, address)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("no account uses %s", address)
	}
	if err != nil {
		return err
	}
	data, err := repository.FindAccountExport(ctx, user.ID)
	if err != nil {
		return err
	}
	if err := repository.Record(ctx, events.AdminUserExported{User: user.PublicID}, store.EventScope{UserID: user.ID}); err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}
