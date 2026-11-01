package observe

import (
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The metric registry (docs/api/observability.md#metrics): counters, gauges
// and fixed-bucket histograms, held in this process's memory, read by
// /metrics (prometheus.go) and summed once a minute into the database
// (minutes.go). Recording is lock-free: a series is found in a map that is
// replaced, never changed, when a series is added, and counted with atomic
// adds. Labels are bounded by construction: every label value comes from a
// fixed set the code names (a route pattern, a limit's or listener's name, a
// status class), never from a request; and each metric holds at most
// MaxSeries series besides its overflow series, whose every label is
// Unmatched, so no client can mint series by sending something new.

// MaxSeries is how many series one metric may hold before new label values
// are counted under the overflow series (every label Unmatched).
const MaxSeries = 100

// maxLabels is how many labels a metric may have.
const maxLabels = 2

// labelValues are a series' label values, in the metric's label order; a
// fixed-size array, so looking a series up never allocates.
type labelValues [maxLabels]string

// overflow is the label values every new series past MaxSeries joins.
var overflow = labelValues{Unmatched, Unmatched}

type kind int

const (
	counterKind kind = iota
	gaugeKind
	histogramKind
)

func (k kind) String() string {
	switch k {
	case counterKind:
		return "counter"
	case gaugeKind:
		return "gauge"
	}
	return "histogram"
}

// family is one metric: its name, help, labels and series.
type family struct {
	name, help string
	kind       kind
	labels     []string
	// bounds are a histogram's bucket upper bounds, in nanoseconds; a last
	// bucket counts what is above them all.
	bounds []int64
	// series maps label values to their series. The map is never changed
	// once stored: adding a series stores a new one (under mu).
	series atomic.Pointer[map[labelValues]*series]
	mu     sync.Mutex
}

// series is one combination of label values.
type series struct {
	values labelValues
	// count is a counter's value, or a histogram's number of observations.
	count atomic.Int64
	// gauge holds a gauge's value as float64 bits.
	gauge atomic.Uint64
	// sum is a histogram's sum, in nanoseconds.
	sum atomic.Int64
	// buckets are a histogram's counts per bucket (not cumulative).
	buckets []atomic.Int64
}

// lookup finds or adds the series for values: lock-free when it exists.
func (f *family) lookup(values labelValues) *series {
	if s, ok := (*f.series.Load())[values]; ok {
		return s
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	current := *f.series.Load()
	if s, ok := current[values]; ok {
		return s
	}
	if len(current) >= MaxSeries {
		values = overflowFor(len(f.labels))
		if s, ok := current[values]; ok {
			return s
		}
	}
	s := &series{values: values}
	if f.kind == histogramKind {
		s.buckets = make([]atomic.Int64, len(f.bounds)+1)
	}
	next := make(map[labelValues]*series, len(current)+1)
	for k, v := range current {
		next[k] = v
	}
	next[values] = s
	f.series.Store(&next)
	return s
}

// overflowFor is the overflow series' values for a metric with n labels.
func overflowFor(n int) labelValues {
	var v labelValues
	copy(v[:n], overflow[:n])
	return v
}

// key turns the label values given into a series key. A value that isn't
// plainly printable, or longer than maxLabelValue, is Unmatched: label
// values are fixed names, so anything else is a mistake that mustn't reach
// /metrics or the database as it is.
func (f *family) key(values []string) labelValues {
	var v labelValues
	for i := range f.labels {
		value := Unmatched
		if i < len(values) && validLabelValue(values[i]) {
			value = values[i]
		}
		v[i] = value
	}
	return v
}

// maxLabelValue is the longest label value kept: the longest route
// pattern is well under it.
const maxLabelValue = 200

func validLabelValue(v string) bool {
	if v == "" || len(v) > maxLabelValue {
		return v == ""
	}
	for i := 0; i < len(v); i++ {
		if c := v[i]; c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// Counter is a metric that only goes up.
type Counter struct{ f *family }

// Add adds n to the series with these label values (in the order the
// counter was declared with).
func (c Counter) Add(n int64, values ...string) {
	c.f.lookup(c.f.key(values)).count.Add(n)
}

// Inc adds one.
func (c Counter) Inc(values ...string) {
	c.Add(1, values...)
}

// Gauge is a metric that is set, read when /metrics or the minute's flush
// collects (Registry.OnCollect sets most of them).
type Gauge struct{ f *family }

// Set sets the series with these label values.
func (g Gauge) Set(v float64, values ...string) {
	g.f.lookup(g.f.key(values)).gauge.Store(math.Float64bits(v))
}

// Histogram counts observations into fixed buckets, in milliseconds.
type Histogram struct{ f *family }

// Observe counts a duration in the series with these label values.
func (h Histogram) Observe(d time.Duration, values ...string) {
	s := h.f.lookup(h.f.key(values))
	n := int64(d)
	if n < 0 {
		n = 0
	}
	i, _ := slices.BinarySearch(h.f.bounds, n)
	s.buckets[i].Add(1)
	s.sum.Add(n)
	s.count.Add(1)
}

// DurationBounds are the latency histograms' bucket bounds, in
// milliseconds: request_minutes.duration_buckets counts one more, above
// 10 s.
var DurationBounds = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}

// Registry is a set of metrics, and the requests' own table (Requests).
// The process's is Metrics; tests make their own with NewRegistry.
type Registry struct {
	// Requests counts each request by route (requests.go).
	Requests *Requests
	mu       sync.Mutex
	families []*family
	collect  []func()
	// minutes is what the last minute's flush saw (minutes.go); only the
	// flush touches it.
	minutes minuteState
}

// NewRegistry is an empty registry.
func NewRegistry() *Registry {
	return &Registry{Requests: newRequests()}
}

func (r *Registry) add(name, help string, k kind, bounds []float64, labels []string) *family {
	if len(labels) > maxLabels {
		panic("observe: at most two labels per metric: " + name)
	}
	f := &family{name: name, help: help, kind: k, labels: labels}
	for _, b := range bounds {
		f.bounds = append(f.bounds, int64(b*float64(time.Millisecond)))
	}
	empty := map[labelValues]*series{}
	f.series.Store(&empty)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.families {
		if existing.name == name {
			panic("observe: metric declared twice: " + name)
		}
	}
	r.families = append(r.families, f)
	return f
}

// Counter declares a counter with up to two labels.
func (r *Registry) Counter(name, help string, labels ...string) Counter {
	return Counter{r.add(name, help, counterKind, nil, labels)}
}

// Gauge declares a gauge with up to two labels.
func (r *Registry) Gauge(name, help string, labels ...string) Gauge {
	return Gauge{r.add(name, help, gaugeKind, nil, labels)}
}

// Histogram declares a histogram of durations, in milliseconds, with these
// bucket bounds and up to two labels.
func (r *Registry) Histogram(name, help string, bounds []float64, labels ...string) Histogram {
	return Histogram{r.add(name, help, histogramKind, bounds, labels)}
}

// OnCollect runs fn before each collection (/metrics, the minute's flush),
// to set gauges read from elsewhere (the connection pool, the runtime).
func (r *Registry) OnCollect(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collect = append(r.collect, fn)
}

// gather runs the collect functions and returns the families, by name.
func (r *Registry) gather() []*family {
	r.mu.Lock()
	collect := slices.Clone(r.collect)
	families := slices.Clone(r.families)
	r.mu.Unlock()
	for _, fn := range collect {
		fn()
	}
	slices.SortFunc(families, func(a, b *family) int { return strings.Compare(a.name, b.name) })
	return families
}

// sorted is a family's series, by label values.
func (f *family) sorted() []*series {
	current := *f.series.Load()
	list := make([]*series, 0, len(current))
	for _, s := range current {
		list = append(list, s)
	}
	slices.SortFunc(list, func(a, b *series) int {
		for i := range a.values {
			if c := strings.Compare(a.values[i], b.values[i]); c != 0 {
				return c
			}
		}
		return 0
	})
	return list
}

// Labels is a series' labels as metric_minutes.labels stores them, and as
// /metrics writes them between braces: name="value" pairs, sorted by name,
// each value escaped as Prometheus escapes it. Empty without labels.
func (f *family) labelText(values labelValues) string {
	if len(f.labels) == 0 {
		return ""
	}
	order := make([]int, len(f.labels))
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int { return strings.Compare(f.labels[a], f.labels[b]) })
	var b strings.Builder
	for n, i := range order {
		if n > 0 {
			b.WriteByte(',')
		}
		writeLabel(&b, f.labels[i], values[i])
	}
	return b.String()
}

func writeLabel(b *strings.Builder, name, value string) {
	b.WriteString(name)
	b.WriteString(`="`)
	b.WriteString(escapeLabel(value))
	b.WriteByte('"')
}

// escapeLabel escapes a label value as the Prometheus text format does: a
// backslash, a double quote and a line break.
func escapeLabel(v string) string {
	if !strings.ContainsAny(v, "\\\"\n") {
		return v
	}
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}
