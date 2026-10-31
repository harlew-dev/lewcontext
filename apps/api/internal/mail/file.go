package mail

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// FileDriver appends each message to a file: the development mailbox, in
// place of email delivery. The file holds working reset links, so it is
// readable only by its owner, its contents never go to the server log, and
// config refuses this driver in production.
type FileDriver struct {
	Path string
	mu   sync.Mutex // one message at a time, so entries don't interleave
}

func (d *FileDriver) Deliver(ctx context.Context, m Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(d.Path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(d.Path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	// An existing file keeps its mode on open; make sure it is private.
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	_, err = fmt.Fprintf(f, "==== %s\nTo: %s\nSubject: %s\nTemplate: %s\n\n%s\n\n",
		time.Now().UTC().Format(time.RFC3339), m.To, m.Subject, m.Template, m.HTML)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	slog.Info("mail written to the development mailbox", "file", d.Path, "template", m.Template)
	return nil
}
