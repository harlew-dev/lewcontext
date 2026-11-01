package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"time"
)

// The block list (plans/features/email-blocks.md): an operator can list it
// and unblock an address, for example after its links have expired. There is
// no block command: blocking takes the inbox owner's link.

// blocks lists every blocked address, oldest first.
func blocks(args []string) {
	fs := flag.NewFlagSet("blocks", flag.ExitOnError)
	_ = fs.Parse(args)
	if fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/admin blocks")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, _ := open(ctx)
	defer repository.DB.Close()
	list, err := repository.IndexEmailBlocks(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if len(list) == 0 {
		fmt.Println("No address is blocked.")
		return
	}
	for _, b := range list {
		fmt.Printf("%s  blocked %s\n", b.Email, blockedAt(b))
	}
}

// unblock takes an address off the block list, so it gets email and can
// sign up again. It previews unless -confirm.
func unblock(args []string) {
	address, confirm := emailFlag("unblock", args)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repository, _ := open(ctx)
	defer repository.DB.Close()
	block, err := repository.FindEmailBlock(ctx, address)
	if errors.Is(err, store.ErrEmailNotBlocked) {
		log.Fatalf("%s isn't blocked: nothing to do", address)
	}
	if err != nil {
		log.Fatal(err)
	}
	if !confirm {
		fmt.Printf("Would unblock %s (blocked %s).\n", block.Email, blockedAt(block))
		fmt.Println("Nothing changed. Re-run with -confirm (make admin cmd=unblock email=… CONFIRM=1) to unblock it.")
		return
	}
	if err := repository.UnblockEmailAddress(ctx, address); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s is unblocked: it gets email and can sign up again.\n", address)
}

func blockedAt(b store.EmailBlock) string {
	return b.CreatedAt.UTC().Format("2006-01-02 15:04 UTC")
}
