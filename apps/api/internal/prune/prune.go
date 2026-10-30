// Package prune deletes old rows from tables that grow without limit, such as
// the events audit trail, for the prune command (cmd/prune). It previews
// unless told to delete. The queries live in internal/store; this package
// holds the targets and the batching.
package prune

import (
	"context"
	"fmt"
	"forma/api/internal/store"
	"regexp"
	"slices"
	"strconv"
	"time"
)

// Pruner is one table the command can prune.
type Pruner struct {
	Name        string
	Description string
	DefaultAge  time.Duration
	// count reports how many rows older than the cutoff would be deleted,
	// and how many are kept on purpose.
	count func(s *store.Store, ctx context.Context, before time.Time) (prunable, kept int, err error)
	// delete removes up to limit of them and says how many it did.
	delete func(s *store.Store, ctx context.Context, before time.Time, limit int) (int, error)
	// kept says why rows are kept, after their count ("with pending deliveries").
	kept string
}

// Pruners lists every target.
var Pruners = []Pruner{
	{
		Name:        "events",
		Description: "the audit trail (events), keeping events whose listeners haven't run yet",
		DefaultAge:  90 * 24 * time.Hour,
		count:       (*store.Store).CountOldEvents,
		delete:      (*store.Store).PruneEvents,
		kept:        "with pending deliveries",
	},
}

// Find returns the target with a name.
func Find(name string) (Pruner, bool) {
	i := slices.IndexFunc(Pruners, func(p Pruner) bool { return p.Name == name })
	if i < 0 {
		return Pruner{}, false
	}
	return Pruners[i], true
}

// MinAge is the youngest rows a run may delete, so a typo such as "0d"
// can't empty a table.
const MinAge = time.Hour

var agePattern = regexp.MustCompile(`^([0-9]{1,5})([dh])$`)

// ParseAge reads an -older-than value: a whole number of days or hours,
// such as "90d" or "36h", of at least an hour.
func ParseAge(value string) (time.Duration, error) {
	m := agePattern.FindStringSubmatch(value)
	if m == nil {
		return 0, fmt.Errorf("-older-than %q: use days or hours, such as 90d or 36h", value)
	}
	n, _ := strconv.Atoi(m[1])
	unit := time.Hour
	if m[2] == "d" {
		unit = 24 * time.Hour
	}
	age := time.Duration(n) * unit
	if age < MinAge {
		return 0, fmt.Errorf("-older-than %q: the minimum is 1h", value)
	}
	return age, nil
}

// FormatAge writes an age the way ParseAge reads it.
func FormatAge(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", age/(24*time.Hour))
	}
	return fmt.Sprintf("%dh", age/time.Hour)
}

// Options for a run. Zero values use the defaults.
type Options struct {
	Age     time.Duration // the pruner's DefaultAge
	Confirm bool          // false previews
	Now     time.Time     // time.Now()
	Batch   int           // rows per DELETE: 5000
}

// Report is what a run found or did.
type Report struct {
	Pruner    Pruner
	Age       time.Duration
	Before    time.Time
	Confirmed bool
	Deleted   int // or, in a preview, would delete
	Kept      int
}

func (r Report) String() string {
	verb := "would delete"
	if r.Confirmed {
		verb = "deleted"
	}
	text := fmt.Sprintf("%s: %s %d older than %s (%s)", r.Pruner.Name, verb, r.Deleted, r.Before.UTC().Format("2006-01-02 15:04 UTC"), FormatAge(r.Age))
	if r.Kept > 0 {
		text += fmt.Sprintf("; kept %d %s", r.Kept, r.Pruner.kept)
	}
	return text
}

// Run previews or prunes. The cutoff is fixed when it starts, so rows created
// meanwhile are never in scope. Deleting goes in batches, each its own short
// statement; when ctx ends between batches it stops and the report counts
// what was deleted so far, alongside ctx's error.
func Run(ctx context.Context, s *store.Store, p Pruner, o Options) (Report, error) {
	if o.Age == 0 {
		o.Age = p.DefaultAge
	}
	if o.Age < MinAge {
		return Report{}, fmt.Errorf("prune %s: the minimum age is %s", p.Name, FormatAge(MinAge))
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.Batch == 0 {
		o.Batch = 5000
	}
	r := Report{Pruner: p, Age: o.Age, Before: o.Now.Add(-o.Age), Confirmed: o.Confirm}
	prunable, kept, err := p.count(s, ctx, r.Before)
	r.Kept = kept
	if err != nil || !o.Confirm {
		r.Deleted = prunable
		return r, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		n, err := p.delete(s, ctx, r.Before, o.Batch)
		r.Deleted += n
		// A short batch means nothing older is left (rows another run holds
		// are that run's to delete).
		if err != nil || n < o.Batch {
			return r, err
		}
	}
}
