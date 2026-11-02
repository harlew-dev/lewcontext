package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"lewcontext/api/internal/auth"
	"log"
	"os"
	"strings"
)

// appKey runs `app-key generate [-env path]`: it makes a new APP_KEY and
// prints it, or with -env writes it into that .env file, but only when the
// file has no APP_KEY yet. It never replaces one: every account with
// two-factor authentication on would be locked out. It doesn't load the
// config, so it works before APP_KEY exists.
func appKey(args []string) {
	if len(args) == 0 || args[0] != "generate" {
		fmt.Fprintln(os.Stderr, "Usage: go run ./cmd/admin app-key generate [-env ../../.env]")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("app-key generate", flag.ExitOnError)
	envFile := fs.String("env", "", "write the key into this .env file if it has none; without it the key is only printed")
	_ = fs.Parse(args[1:])
	key, err := auth.GenerateAppKey()
	if err != nil {
		log.Fatal(err)
	}
	if *envFile == "" {
		fmt.Println("APP_KEY=" + key)
		return
	}
	if os.Getenv("APP_ENV") == "production" {
		log.Fatal("app-key generate doesn't write files in production: run it without -env and put the key in your secret store")
	}
	if err := writeAppKey(*envFile, key); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("APP_KEY written to %s. Back it up with the database: changing it later locks out every account with two-factor authentication.\n", *envFile)
}

// writeAppKey sets APP_KEY in an .env file: it fills an empty APP_KEY= line
// or appends one, and refuses a file whose APP_KEY already has a value.
func writeAppKey(path, key string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s doesn't exist: copy .env.example to it first", path)
	}
	if err != nil {
		return err
	}
	var out bytes.Buffer
	written := false
	lines := bufio.NewScanner(bytes.NewReader(data))
	for lines.Scan() {
		line := lines.Text()
		if name, value, ok := strings.Cut(line, "="); ok && strings.TrimSpace(name) == "APP_KEY" {
			if strings.TrimSpace(value) != "" {
				return fmt.Errorf("%s already has an APP_KEY, and replacing it locks out every account with two-factor authentication. To replace it anyway, delete that line and run this again", path)
			}
			if written {
				continue
			}
			line, written = "APP_KEY="+key, true
		}
		out.WriteString(line + "\n")
	}
	if err := lines.Err(); err != nil {
		return err
	}
	if !written {
		out.WriteString("APP_KEY=" + key + "\n")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), info.Mode().Perm())
}
