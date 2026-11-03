package observe

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// /metrics (docs/api/observability.md#metrics-for-an-external-system): this
// process's registry in the Prometheus text format, written by hand (the
// format is small; no client library), on a listener of its own
// (METRICS_ADDR), never the public one.

// The request table's metrics, written from Registry.Requests.
const (
	requestsTotal   = "http_requests_total"
	requestDuration = "http_request_duration_ms"
	requestTimeouts = "http_timeouts_total"
)

// WritePrometheus writes every metric in r, sorted by name and labels, in
// the Prometheus text exposition format (version 0.0.4).
func (r *Registry) WritePrometheus(out io.Writer) error {
	w := bufio.NewWriter(out)
	families := r.gather()
	written := false
	for _, f := range families {
		if !written && f.name > requestDuration {
			writeRequests(w, r.Requests)
			written = true
		}
		writeFamily(w, f)
	}
	if !written {
		writeRequests(w, r.Requests)
	}
	return w.Flush()
}

func writeHeader(w *bufio.Writer, name, help, kind string) {
	w.WriteString("# HELP " + name + " " + help + "\n")
	w.WriteString("# TYPE " + name + " " + kind + "\n")
}

// writeRequests writes the request table as three metrics, in name order:
// the latency histogram by route, the requests by route and status class
// (aborted for those that got no answer), and the timeouts by route.
func writeRequests(w *bufio.Writer, q *Requests) {
	routes := q.sorted()
	totals := make([]RouteTotals, len(routes))
	for i, s := range routes {
		totals[i] = s.totals()
	}
	writeHeader(w, requestDuration, "How long requests took, by route, in milliseconds.", "histogram")
	for _, t := range totals {
		labels := `route="` + escapeLabel(t.Route) + `"`
		writeBuckets(w, requestDuration, labels, DurationBounds, t.Buckets[:], t.Requests, float64(t.DurationSum)/float64(time.Millisecond))
	}
	writeHeader(w, requestsTotal, "Requests answered, by route and status class; aborted got no answer.", "counter")
	for _, t := range totals {
		route := `route="` + escapeLabel(t.Route) + `",status="`
		for _, c := range []struct {
			class string
			n     int64
		}{{"2xx", t.Status2xx}, {"3xx", t.Status3xx}, {"4xx", t.Status4xx}, {"5xx", t.Status5xx}, {"aborted", t.Aborted}} {
			if c.n > 0 {
				writeSample(w, requestsTotal, route+c.class+`"`, float64(c.n))
			}
		}
	}
	writeHeader(w, requestTimeouts, "Requests the 10 s deadline ended, by route: a write may have committed though the client saw an error.", "counter")
	for _, t := range totals {
		if t.Timeouts > 0 {
			writeSample(w, requestTimeouts, `route="`+escapeLabel(t.Route)+`"`, float64(t.Timeouts))
		}
	}
}

func writeFamily(w *bufio.Writer, f *family) {
	writeHeader(w, f.name, f.help, f.kind.String())
	bounds := make([]float64, len(f.bounds))
	for i, b := range f.bounds {
		bounds[i] = float64(b) / float64(time.Millisecond)
	}
	for _, s := range f.sorted() {
		labels := f.labelText(s.values)
		switch f.kind {
		case counterKind:
			writeSample(w, f.name, labels, float64(s.count.Load()))
		case gaugeKind:
			writeSample(w, f.name, labels, math.Float64frombits(s.gauge.Load()))
		case histogramKind:
			counts := make([]int64, len(s.buckets))
			for i := range s.buckets {
				counts[i] = s.buckets[i].Load()
			}
			writeBuckets(w, f.name, labels, bounds, counts, s.count.Load(), float64(s.sum.Load())/float64(time.Millisecond))
		}
	}
}

// writeBuckets writes a histogram's cumulative buckets, +Inf, sum and count.
func writeBuckets(w *bufio.Writer, name, labels string, bounds []float64, counts []int64, count int64, sum float64) {
	prefix := labels
	if prefix != "" {
		prefix += ","
	}
	var cumulative int64
	for i, b := range bounds {
		cumulative += counts[i]
		writeSample(w, name+"_bucket", prefix+`le="`+formatFloat(b)+`"`, float64(cumulative))
	}
	writeSample(w, name+"_bucket", prefix+`le="+Inf"`, float64(count))
	writeSample(w, name+"_sum", labels, sum)
	writeSample(w, name+"_count", labels, float64(count))
}

func writeSample(w *bufio.Writer, name, labels string, value float64) {
	w.WriteString(name)
	if labels != "" {
		w.WriteString("{" + labels + "}")
	}
	w.WriteString(" " + formatFloat(value) + "\n")
}

func formatFloat(v float64) string {
	switch {
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// MetricsHandler serves GET /metrics from r and nothing else: any other
// path is 404, any other method 405. With a token (METRICS_TOKEN) it
// requires Authorization: Bearer <token>, compared in constant time (as
// SHA-256 digests, so not even the token's length shows); without one it
// answers anyone who can reach its listener.
func MetricsHandler(r *Registry, token string) http.Handler {
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		if req.URL.Path != "/metrics" {
			http.Error(w, "Not found.", http.StatusNotFound)
			return
		}
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
			return
		}
		if token != "" && !bearerMatches(req.Header.Get("Authorization"), want) {
			h.Set("WWW-Authenticate", `Bearer realm="metrics"`)
			http.Error(w, "Unauthorized.", http.StatusUnauthorized)
			return
		}
		h.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		if req.Method == http.MethodHead {
			return
		}
		_ = r.WritePrometheus(w)
	})
}

// bearerMatches reports whether an Authorization header carries the token
// whose SHA-256 is want, comparing digests in constant time.
func bearerMatches(header string, want [sha256.Size]byte) bool {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		token = ""
	}
	got := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1 && ok && token != ""
}

// MetricsServer is the listener /metrics runs on (METRICS_ADDR), with its
// own timeouts: a scrape is small, so a slow client is cut off quickly.
func MetricsServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
	}
}
