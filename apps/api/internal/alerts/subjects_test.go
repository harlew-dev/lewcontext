package alerts

import (
	"lewcontext/api/internal/mail"
	"lewcontext/api/internal/monitor"
	"testing"
)

// Every rule's signal title makes a valid subject, for each kind of email.
func TestEveryRuleMakesASubject(t *testing.T) {
	for _, r := range monitor.AlertRules {
		for _, kind := range []string{"opened", "escalated", "reminded", "resolved"} {
			for _, level := range []string{"Critical", "Warning"} {
				data := mail.AlertData{Kind: kind, Level: level, Title: AboutSignal(r.Signal).Title}
				if err := mail.CheckSubject(mail.Alert, data); err != nil {
					t.Errorf("%s %s %s: %q: %v", r.Signal, kind, level, data.Subject(), err)
				}
			}
		}
	}
}

// An alert whose signal is no longer defined is told by a fixed title that
// makes a valid subject, not by its raw name (which holds an underscore).
func TestAnUndefinedSignalHasAFixedTitle(t *testing.T) {
	about := AboutSignal("auth.renamed_since")
	if about.Title != "An alert" {
		t.Fatalf("title: %q", about.Title)
	}
	for _, kind := range []string{"opened", "escalated", "reminded", "resolved"} {
		data := mail.AlertData{Kind: kind, Level: "Critical", Title: about.Title}
		if err := mail.CheckSubject(mail.Alert, data); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}
