package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/database"
	"lewcontext/api/internal/observe"
	"lewcontext/api/internal/prune"
	"lewcontext/api/internal/store"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// go run ./cmd/prune [target] [-older-than 90d] [-confirm]
//
// Without -confirm it only reports what it would delete; without a target it
// previews every target at its default age and never deletes. -older-than
// needs a target. Allowed in production, where it would run from cron.
func main() {
	useLogger()
	olderThan := flag.String("older-than", "", "with a target: delete rows older than this, in days or hours (90d, 36h); default per target")
	confirm := flag.Bool("confirm", false, "delete; without it the command only previews")
	flag.Usage = usage
	target := parseArgs()
	if target == "" && *olderThan != "" {
		log.Fatal("-older-than needs a target; a run over every target uses each one's default age")
	}
	var age time.Duration
	if *olderThan != "" {
		var err error
		if age, err = prune.ParseAge(*olderThan); err != nil {
			log.Fatal(err)
		}
	}
	c, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	db, err := database.Open(ctx, c.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	repository := &store.Store{DB: db}
	keys, err := auth.NewKeys(c.AppKey)
	if err != nil {
		log.Fatal(err)
	}
	options := prune.Options{Pseudonym: keys.EmailPseudonym}

	if target == "" {
		for _, p := range prune.Pruners() {
			report, err := prune.Run(ctx, repository, p, options)
			if err != nil {
				log.Fatalf("prune %s: %v", p.Name, err)
			}
			fmt.Println(report)
		}
		if *confirm {
			fmt.Println("Confirming needs a target.")
		}
		fmt.Println("Nothing deleted. To delete: make prune target=<target> [older-than=90d] CONFIRM=1")
		return
	}
	p, ok := prune.Find(target)
	if !ok {
		log.Fatalf("unknown target %q; choose %s", target, names())
	}
	options.Age, options.Confirm = age, *confirm
	report, err := prune.Run(ctx, repository, p, options)
	fmt.Println(report)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		log.Fatalf("stopped early: %v (what's listed as deleted is gone)", err)
	}
	if err != nil {
		log.Fatalf("prune %s: %v", p.Name, err)
	}
	if !*confirm {
		fmt.Println("Nothing deleted. Re-run with CONFIRM=1 (or -confirm) to delete.")
	}
}

// parseArgs reads flags before and after the target, so both
// `prune -confirm events` and `prune events -confirm` work.
func parseArgs() string {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		return ""
	}
	target := args[0]
	if err := flag.CommandLine.Parse(args[1:]); err != nil {
		os.Exit(2)
	}
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	return target
}

func names() string {
	text := ""
	for i, p := range prune.Pruners() {
		if i > 0 {
			text += ", "
		}
		text += p.Name
	}
	return text
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintln(out, "Usage: go run ./cmd/prune [target] [-older-than 90d] [-confirm]")
	fmt.Fprintln(out, "Previews unless -confirm; without a target previews every target at its default age and deletes nothing.")
	fmt.Fprintln(out, "\nTargets:")
	for _, p := range prune.Pruners() {
		fmt.Fprintf(out, "  %-13s %s (default %s)\n", p.Name, p.Description, prune.FormatAge(p.DefaultAge))
	}
	fmt.Fprintln(out, "\nFlags:")
	flag.PrintDefaults()
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
