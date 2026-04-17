package metrics

import (
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// latencyBuckets are the Prometheus histogram buckets in seconds.
var latencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type labelKey struct {
	Method string
	Path   string
	Status string
}

type histogram struct {
	counts []uint64
	sum    float64
	total  uint64
}

func newHistogram() *histogram {
	return &histogram{counts: make([]uint64, len(latencyBuckets))}
}

func (h *histogram) observe(v float64) {
	h.sum += v
	h.total++
	for i, b := range latencyBuckets {
		if v <= b {
			h.counts[i]++
		}
	}
}

// Registry stores counters and a histogram for HTTP observability.
type Registry struct {
	mu        sync.Mutex
	requests  map[labelKey]uint64
	errors    map[labelKey]uint64
	durations map[labelKey]*histogram
}

func New() *Registry {
	return &Registry{
		requests:  make(map[labelKey]uint64),
		errors:    make(map[labelKey]uint64),
		durations: make(map[labelKey]*histogram),
	}
}

// Observe records one request with the given method, path, status code, and duration.
func (r *Registry) Observe(method, path string, status int, d time.Duration) {
	k := labelKey{Method: method, Path: path, Status: strconv.Itoa(status)}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.requests[k]++
	if status >= 500 {
		r.errors[k]++
	}
	h, ok := r.durations[k]
	if !ok {
		h = newHistogram()
		r.durations[k] = h
	}
	h.observe(d.Seconds())
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.status = code
	s.wroteHeader = true
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.status = http.StatusOK
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}

// Middleware records metrics for each HTTP request. The metrics endpoint itself
// is excluded to avoid self-recursion and cardinality explosion.
func (r *Registry) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/metrics" {
			next.ServeHTTP(w, req)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, req)
		r.Observe(req.Method, normalizePath(req.URL.Path), rec.status, time.Since(start))
	})
}

// normalizePath collapses /todos/{id} to /todos/:id so cardinality stays bounded.
func normalizePath(p string) string {
	if p == "/todos" || !strings.HasPrefix(p, "/todos/") {
		return p
	}
	rest := strings.Trim(strings.TrimPrefix(p, "/todos/"), "/")
	if rest == "" {
		return "/todos"
	}
	return "/todos/:id"
}

// Handler returns an http.HandlerFunc that exposes metrics in Prometheus text format.
func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		r.mu.Lock()
		defer r.mu.Unlock()

		writeCounter(w, "http_requests_total", "Total number of HTTP requests.", r.requests)
		writeCounter(w, "http_errors_total", "Total number of HTTP 5xx responses.", r.errors)
		writeHistogram(w, "http_request_duration_seconds", "HTTP request latency in seconds.", r.durations)
	}
}

func writeCounter(w io.Writer, name, help string, data map[labelKey]uint64) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	keys := sortedKeys(data)
	for _, k := range keys {
		fmt.Fprintf(w, "%s{method=%q,path=%q,status=%q} %d\n", name, k.Method, k.Path, k.Status, data[k])
	}
}

func writeHistogram(w io.Writer, name, help string, data map[labelKey]*histogram) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, help)
	fmt.Fprintf(w, "# TYPE %s histogram\n", name)
	keys := make([]labelKey, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return lessLabel(keys[i], keys[j]) })
	for _, k := range keys {
		h := data[k]
		for i, b := range latencyBuckets {
			fmt.Fprintf(w, "%s_bucket{method=%q,path=%q,status=%q,le=\"%s\"} %d\n",
				name, k.Method, k.Path, k.Status, formatFloat(b), h.counts[i])
		}
		fmt.Fprintf(w, "%s_bucket{method=%q,path=%q,status=%q,le=\"+Inf\"} %d\n",
			name, k.Method, k.Path, k.Status, h.total)
		fmt.Fprintf(w, "%s_sum{method=%q,path=%q,status=%q} %s\n",
			name, k.Method, k.Path, k.Status, formatFloat(h.sum))
		fmt.Fprintf(w, "%s_count{method=%q,path=%q,status=%q} %d\n",
			name, k.Method, k.Path, k.Status, h.total)
	}
}

func sortedKeys(data map[labelKey]uint64) []labelKey {
	keys := make([]labelKey, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return lessLabel(keys[i], keys[j]) })
	return keys
}

func lessLabel(a, b labelKey) bool {
	if a.Method != b.Method {
		return a.Method < b.Method
	}
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	return a.Status < b.Status
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
