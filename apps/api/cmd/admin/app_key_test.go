package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAppKey(t *testing.T) {
	write := func(content string) string {
		path := filepath.Join(t.TempDir(), ".env")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	empty := write("APP_NAME=lewcontext\nAPP_KEY=\nDEMO_PASSWORD=password\n")
	if err := writeAppKey(empty, "new-key"); err != nil {
		t.Fatal(err)
	}
	if got := read(empty); got != "APP_NAME=lewcontext\nAPP_KEY=new-key\nDEMO_PASSWORD=password\n" {
		t.Fatalf("filled an empty line: %q", got)
	}

	missing := write("APP_NAME=lewcontext")
	if err := writeAppKey(missing, "new-key"); err != nil {
		t.Fatal(err)
	}
	if got := read(missing); got != "APP_NAME=lewcontext\nAPP_KEY=new-key\n" {
		t.Fatalf("appended: %q", got)
	}
	if info, _ := os.Stat(missing); info.Mode().Perm() != 0o600 {
		t.Fatalf("the file's mode changed to %v", info.Mode().Perm())
	}

	set := write("APP_KEY=old-key\n")
	err := writeAppKey(set, "new-key")
	if err == nil || !strings.Contains(err.Error(), "already has an APP_KEY") || strings.Contains(err.Error(), "old-key") {
		t.Fatalf("replaced an existing key: %v", err)
	}
	if read(set) != "APP_KEY=old-key\n" {
		t.Fatal("the file changed")
	}
	if err := writeAppKey(filepath.Join(t.TempDir(), "nope"), "k"); err == nil {
		t.Fatal("wrote a missing file")
	}
}
