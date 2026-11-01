package monitor

import (
	"context"
	"errors"
	"time"
)

// A page is read in parts, each on its own budget (Env.Budget) within the
// page's deadline: a signal or a section that takes too long is listed as
// not loaded, and the rest of the page still shows. A slow query, or one
// someone's own events make slow, can then hide its part, never the page.
// Only when nothing at all could be read does the page fail
// (ErrNothingRead), and then nothing was read to record.

// ErrNothingRead: no part of a page could be read within its budget.
var ErrNothingRead = errors.New("monitoring: nothing read in time")

// section is one part of a page, read on its own. Name is its key in the
// page's sections (several parts may share one).
type section struct {
	name string
	read func(ctx context.Context) error
}

// readPart runs read under env's budget. It reports whether the part
// timed out (its budget, or the page's deadline, passed); any other error
// is returned, a request canceled by its client included.
func readPart(ctx context.Context, env Env, read func(ctx context.Context) error) (timedOut bool, err error) {
	partCtx, cancel := ctx, context.CancelFunc(func() {})
	if env.Budget > 0 {
		partCtx, cancel = context.WithTimeout(ctx, env.Budget)
	}
	defer cancel()
	err = read(partCtx)
	if err == nil {
		return false, nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return false, err
	}
	if partCtx.Err() != nil {
		return true, nil
	}
	return false, err
}

// Sections are a page's sections as read: its own value, the names of the
// parts that timed out (each once), and how many parts were read.
type Sections struct {
	Value       any
	Unavailable []string
	Read        int
}

// readSections reads each part in turn into value.
func readSections(ctx context.Context, env Env, value any, parts []section) (Sections, error) {
	out := Sections{Value: value, Unavailable: []string{}}
	for _, p := range parts {
		timedOut, err := readPart(ctx, env, p.read)
		if err != nil {
			return Sections{}, err
		}
		if !timedOut {
			out.Read++
			continue
		}
		if len(out.Unavailable) == 0 || out.Unavailable[len(out.Unavailable)-1] != p.name {
			out.Unavailable = append(out.Unavailable, p.name)
		}
	}
	return out, nil
}

// readSignals reads a page's signals, each under the budget: one that
// times out is listed as unavailable.
func readSignals(ctx context.Context, env Env, signals []Signal) ([]Result, int, error) {
	out := make([]Result, 0, len(signals))
	read := 0
	for _, sig := range signals {
		var r Result
		timedOut, err := readPart(ctx, env, func(ctx context.Context) (err error) {
			r, err = Read(ctx, env, sig)
			return
		})
		if err != nil {
			return nil, 0, err
		}
		if timedOut {
			r = Result{Name: sig.Name, Label: sig.Label, Unit: sig.Unit, Level: LevelOK, Warn: sig.Warn, Critical: sig.Critical,
				MinSample: sig.MinSample, WindowSeconds: int(sig.Window / time.Second), Unavailable: true}
		} else {
			read++
		}
		out = append(out, r)
	}
	return out, read, nil
}
