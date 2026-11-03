package observe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Request IDs are req_ and 14 base62 characters, all different.
func TestNewRequestID(t *testing.T) {
	shape := regexp.MustCompile(`^req_[0-9A-Za-z]{14}$`)
	seen := map[string]bool{}
	counts := map[byte]int{}
	for range 10000 {
		id := NewRequestID()
		if !shape.MatchString(id) || !ValidRequestID(id) {
			t.Fatalf("bad ID %q", id)
		}
		if seen[id] {
			t.Fatalf("repeated ID %q", id)
		}
		seen[id] = true
		for i := 4; i < len(id); i++ {
			counts[id[i]]++
		}
	}
	// 140,000 characters over 62: each about 2,258. Every one appears, and
	// none is far off (a biased modulo would favour the first 8).
	if len(counts) != 62 {
		t.Fatalf("only %d characters used", len(counts))
	}
	for c, n := range counts {
		if n < 1900 || n > 2650 {
			t.Errorf("%q appeared %d times", c, n)
		}
	}
}

// A proxy's ID is taken only in the safe shape: nothing that could break a
// log line, a header or the events table's CHECK.
func TestValidRequestID(t *testing.T) {
	for id, want := range map[string]bool{
		"req_0123456789abcd":                   true,
		"a":                                    true,
		"0a1b2c3d-4e5f-6789-abcd-ef0123456789": true,
		"lb.123_45-6":                          true,
		strings.Repeat("a", 64):                true,
		strings.Repeat("a", 65):                false,
		"":                                     false,
		"has space":                            false,
		"new\nline":                            false,
		"carriage\rreturn":                     false,
		"quote\"":                              false,
		"tab\t":                                false,
		"semi;colon":                           false,
		"slash/":                               false,
		"é":                                    false,
		"null\x00":                             false,
	} {
		if got := ValidRequestID(id); got != want {
			t.Errorf("ValidRequestID(%q) = %v", id, got)
		}
	}
}

// The record sees what the handler writes: the first final status (not a
// 1xx), an implicit 200, and the bytes.
func TestRequestRecordsTheAnswer(t *testing.T) {
	w := httptest.NewRecorder()
	q := Begin(w, "203.0.113.9")
	if q.Written() || q.Route != Unmatched || q.IP != "203.0.113.9" {
		t.Fatalf("new record: %+v", q)
	}
	hints := Begin(httptest.NewRecorder(), "")
	hints.WriteHeader(http.StatusEarlyHints)
	if hints.Written() {
		t.Fatal("a 1xx counted as the answer")
	}
	q.WriteHeader(http.StatusTeapot)
	_, _ = q.Write([]byte("hello"))
	if q.Status != http.StatusTeapot || q.Bytes != 5 || !q.Written() {
		t.Fatalf("after writing: %+v", q)
	}
	implicit := Begin(httptest.NewRecorder(), "")
	_, _ = implicit.Write([]byte("hi"))
	if implicit.Status != http.StatusOK {
		t.Fatalf("implicit status: %d", implicit.Status)
	}
	read := Begin(httptest.NewRecorder(), "")
	if n, err := read.ReadFrom(strings.NewReader("12345678")); n != 8 || err != nil || read.Bytes != 8 || read.Status != 200 {
		t.Fatalf("ReadFrom: %d %v %+v", n, err, read)
	}
	if http.NewResponseController(q) == nil || q.Unwrap() != w {
		t.Fatal("Unwrap")
	}
	// A flush starts the answer: its implicit 200 is recorded.
	recorder := httptest.NewRecorder()
	flushed := Begin(recorder, "")
	var _ http.Flusher = flushed
	flushed.Flush()
	if !flushed.Written() || flushed.Status != http.StatusOK || !recorder.Flushed {
		t.Fatalf("after Flush: %+v", flushed)
	}
	viaController := Begin(httptest.NewRecorder(), "")
	if err := http.NewResponseController(viaController).Flush(); err != nil || !viaController.Written() {
		t.Fatalf("ResponseController: %v %+v", err, viaController)
	}
}

func TestRequestInContext(t *testing.T) {
	ctx := context.Background()
	if FromContext(ctx) != nil || RequestID(ctx) != "" || Route(ctx) != "" {
		t.Fatal("a request outside one")
	}
	SetRoute(ctx, "x") // outside a request: nothing to set, no panic
	SetUser(ctx, "x")
	q := Begin(httptest.NewRecorder(), "203.0.113.9")
	id := q.NewID()
	ctx = WithRequest(ctx, q)
	SetRoute(ctx, "GET /api/health")
	SetUser(ctx, "usr_0123456789abcd")
	if RequestID(ctx) != id || Route(ctx) != "GET /api/health" || q.User != "usr_0123456789abcd" || q.IDHeader()[0] != id {
		t.Fatalf("in context: %+v", q)
	}
}
