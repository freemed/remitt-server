// Package middleware: test suite for the Prometheus metrics middleware.
//
// # Registry constraint (why these tests use the default registry)
//
// All three metrics in prometheus.go are package-level vars built with
// promauto.New* at package init time, which registers them into
// prometheus.DefaultRegisterer / DefaultGatherer. The vars are unexported and
// there is no constructor that accepts a prometheus.Registerer, so a FRESH
// registry per test is impossible without changing production code (the fix
// would be promauto.With(reg) plus an exported constructor taking a
// Registerer). Consequences that shape this file:
//
//   - Every test reads the SHARED default registry. To stay order-independent
//     (and safe under -shuffle / t.Parallel) each test uses its own unique
//     route paths as label values, and assertions are filtered to those labels
//     instead of counting series globally.
//   - testutil.CollectAndCount / GatherAndCount with no label filter are
//     deliberately NOT used: they count series in the whole default registry
//     and would therefore depend on which other tests ran first. Series-existence
//     checks gather from prometheus.DefaultGatherer directly and filter labels
//     in Go; scalar values use testutil.ToFloat64.
//   - The tests are therefore in-package (package middleware) because the
//     metric vars are unexported; an external middleware_test package cannot
//     reference them at all.
//
// Tests whose names start with TestPrometheusBug... PIN CURRENT (buggy)
// behaviour found while writing this suite. They are expected to trip (and be
// updated) when the bugs reported alongside this file are fixed — they exist so
// the behaviour is visible and regression-tested, not to bless it.
package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	echoMW "github.com/labstack/echo/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	metricRequestsTotal   = "http_requests_total"
	metricRequestDuration = "http_request_duration_seconds"
)

// series is one gathered metric line, flattened to label name -> value.
type series map[string]string

// newTestServer builds an echo server whose only middleware is the package
// under test (plus anything the caller passes in outer, which is applied
// BEFORE Prometheus so it ends up outside it — matching main.go, where the
// outer middleware list is registered before remittmiddleware.Prometheus()).
func newTestServer(outer ...echo.MiddlewareFunc) *echo.Echo {
	e := echo.New()
	for _, mw := range outer {
		e.Use(mw)
	}
	e.Use(Prometheus())
	return e
}

// doRequest drives the server in-process through httptest (no network).
func doRequest(e *echo.Echo, method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// gatherSeries returns every series currently present in the DEFAULT registry
// for the named metric family, as flat label maps.
func gatherSeries(t *testing.T, name string) []series {
	t.Helper()
	fams, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather %s from default registry: %v", name, err)
	}
	var out []series
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			s := series{}
			for _, l := range m.GetLabel() {
				s[l.GetName()] = l.GetValue()
			}
			out = append(out, s)
		}
	}
	return out
}

// matching returns the series of name whose labels all equal want.
func matching(t *testing.T, name string, want series) []series {
	t.Helper()
	var out []series
	for _, s := range gatherSeries(t, name) {
		ok := true
		for k, v := range want {
			if s[k] != v {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, s)
		}
	}
	return out
}

// counterValue reads a counter value using testutil. NOTE: WithLabelValues
// lazily CREATES the series, so never call this before an existence check.
func counterValue(t *testing.T, method, path, status string) float64 {
	t.Helper()
	return testutil.ToFloat64(httpRequestsTotal.WithLabelValues(method, path, status))
}

// histogramObservation reads the sample count and sum of the histogram series
// with the given labels, without creating it (it gathers instead).
func histogramObservation(t *testing.T, method, path, status string) (uint64, float64) {
	t.Helper()
	fams, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather %s from default registry: %v", metricRequestDuration, err)
	}
	for _, f := range fams {
		if f.GetName() != metricRequestDuration {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := series{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["method"] != method || labels["path"] != path || labels["status"] != status {
				continue
			}
			h := m.GetHistogram()
			return h.GetSampleCount(), h.GetSampleSum()
		}
	}
	return 0, 0
}

// ---------------------------------------------------------------------------
// 1. Counter + in-flight gauge
// ---------------------------------------------------------------------------

// TestPrometheusCounterAndInFlightGauge is deliberately NOT parallel: the
// in-flight gauge is a single shared gauge, so any other test with a request in
// flight would make these reads flaky. Go runs non-parallel tests before the
// parallel ones, so this stays deterministic.
func TestPrometheusCounterAndInFlightGauge(t *testing.T) {
	var inFlightDuringRequest float64 = -1
	e := newTestServer()
	e.GET("/mt1/ok", func(c *echo.Context) error {
		// The middleware increments the gauge before calling next(), so the
		// gauge must read 1 for the whole duration of the handler.
		inFlightDuringRequest = testutil.ToFloat64(httpRequestsInFlight)
		return c.String(http.StatusOK, "ok")
	})

	if got := testutil.ToFloat64(httpRequestsInFlight); got != 0 {
		t.Fatalf("in-flight gauge before any request = %v, want 0", got)
	}

	rec := doRequest(e, http.MethodGet, "/mt1/ok", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("response code = %d, want %d", rec.Code, http.StatusOK)
	}

	if inFlightDuringRequest != 1 {
		t.Errorf("in-flight gauge during the request = %v, want 1", inFlightDuringRequest)
	}
	if got := testutil.ToFloat64(httpRequestsInFlight); got != 0 {
		t.Errorf("in-flight gauge after the request = %v, want 0", got)
	}

	if got := counterValue(t, http.MethodGet, "/mt1/ok", "200"); got != 1 {
		t.Errorf("http_requests_total{method=GET,path=/mt1/ok,status=200} = %v, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// 2. Route-template label / cardinality guard
// ---------------------------------------------------------------------------

// TestPrometheusUsesRouteTemplateLabelNotRawURL is the cardinality guard: for
// a parameterised route the middleware must label with the route template
// ("/mt2/status/:id"), so N distinct ids collapse into ONE time series. If the
// middleware ever switches to the raw request path, an id-bearing endpoint
// like /api/status/12345 becomes one series per id and the series count grows
// without bound (the classic Prometheus cardinality blow-up).
func TestPrometheusUsesRouteTemplateLabelNotRawURL(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	e.GET("/mt2/status/:id", func(c *echo.Context) error {
		return c.String(http.StatusOK, "id="+c.Param("id"))
	})

	doRequest(e, http.MethodGet, "/mt2/status/90001", nil)
	doRequest(e, http.MethodGet, "/mt2/status/90002", nil)

	got := matching(t, metricRequestsTotal, series{
		"method": http.MethodGet,
		"path":   "/mt2/status/:id",
		"status": "200",
	})
	if len(got) != 1 {
		t.Fatalf("series for path=/mt2/status/:id,status=200 = %d, want exactly 1 (labels: %v)", len(got), got)
	}
	// Two distinct ids must have been folded into that single series.
	if v := counterValue(t, http.MethodGet, "/mt2/status/:id", "200"); v != 2 {
		t.Errorf("http_requests_total{path=/mt2/status/:id} = %v, want 2 (one per id-bearing request)", v)
	}

	// No series anywhere may carry a raw URL / id in its path label.
	for _, s := range gatherSeries(t, metricRequestsTotal) {
		p := s["path"]
		for _, raw := range []string{"90001", "90002", "/mt2/status/9"} {
			if strings.Contains(p, raw) {
				t.Errorf("raw request path leaked into the path label: %q (series %v)", p, s)
			}
		}
	}
}

// TestPrometheusUnmatchedRouteDoesNotUseRawURLLabel covers the same cardinality
// risk on the 404 path: an attacker probing random URLs must not mint a series
// per URL. Echo v5 leaves Context.Path() empty when routing fails
// (router.go: InitializeRoute(notFoundRouteInfo)), so all unmatched URLs land
// in the single series path="".
func TestPrometheusUnmatchedRouteDoesNotUseRawURLLabel(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	doRequest(e, http.MethodGet, "/mt3/uhoh/91234", nil)
	doRequest(e, http.MethodGet, "/mt3/uhoh/91235", nil)

	for _, s := range gatherSeries(t, metricRequestsTotal) {
		if strings.Contains(s["path"], "9123") {
			t.Errorf("unmatched request path leaked into the path label: %v", s)
		}
	}

	got := matching(t, metricRequestsTotal, series{"path": ""})
	if len(got) == 0 {
		t.Fatalf("no series with the empty path label; echo v5 sets an empty route path for 404s, so one is expected")
	}
	t.Logf("unmatched requests are labelled path=\"\" (all 404s share one series) — see the style/aggregation note in the test report")
}

// ---------------------------------------------------------------------------
// 3. Histogram
// ---------------------------------------------------------------------------

func TestPrometheusHistogramObservesRequest(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	e.GET("/mt4/hist", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	doRequest(e, http.MethodGet, "/mt4/hist", nil)

	count, sum := histogramObservation(t, http.MethodGet, "/mt4/hist", "200")
	if count == 0 {
		t.Fatalf("http_request_duration_seconds{path=/mt4/hist,status=200} sample count = 0, want > 0")
	}
	if count != 1 {
		t.Errorf("sample count = %d, want 1 (one request)", count)
	}
	if sum < 0 {
		t.Errorf("sample sum = %v, want >= 0", sum)
	}

	// The same request must also be counted, so a missing counter/histogram
	// pair is caught here rather than in the scrape.
	if v := counterValue(t, http.MethodGet, "/mt4/hist", "200"); v != 1 {
		t.Errorf("counter for the same labels = %v, want 1", v)
	}
}

// ---------------------------------------------------------------------------
// 4. Status label: what does and does not get recorded
// ---------------------------------------------------------------------------

// TestPrometheusStatusLabelForExplicitWrites pins the behaviour that DOES
// work: a status actually written to the response (c.String / c.JSON /
// c.NoContent) is reported on the status label.
func TestPrometheusStatusLabelForExplicitWrites(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	e.GET("/mt5/teapot", func(c *echo.Context) error { return c.String(http.StatusTeapot, "nope") })

	rec := doRequest(e, http.MethodGet, "/mt5/teapot", nil)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("response code = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if v := counterValue(t, http.MethodGet, "/mt5/teapot", "418"); v != 1 {
		t.Errorf("http_requests_total{path=/mt5/teapot,status=418} = %v, want 1", v)
	}
}

// TestPrometheusBugHTTPErrorStatusRecordedAs200 pins a REAL BUG.
//
// prometheus.go:61,65-68 reads the status from the response writer and records
// it immediately after next(c) returns — but a handler that merely RETURNS an
// error has not written anything yet: echo renders it afterwards, in
// Echo.HTTPErrorHandler (main.go:71-82), which runs in ServeHTTP *outside* the
// middleware chain (echo.go: serveHTTP -> e.HTTPErrorHandler(c, err)). Because
// Prometheus() is registered last (main.go:98) it is the innermost middleware,
// so it always inspects the response before any error response exists.
//
// Result: every request whose status is produced by the error handler
// (404/405, any echo.NewHTTPError returned by a handler, panics) is counted
// with status="200" on an http_requests_total series, so error-rate alerting
// built on this metric reads zero. (Requests rejected by a middleware that is
// registered OUTSIDE Prometheus, such as BasicAuth at main.go:86, are a
// different case — they never reach Prometheus at all; see
// TestPrometheusBugRejectedAuthRequestsAreNotCounted.)
func TestPrometheusBugHTTPErrorStatusRecordedAs200(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	e.GET("/mt6/err404", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound, "gone") // never writes a status itself
	})

	rec := doRequest(e, http.MethodGet, "/mt6/err404", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("response code = %d, want %d (echo rendered the error)", rec.Code, http.StatusNotFound)
	}

	if v := counterValue(t, http.MethodGet, "/mt6/err404", "200"); v != 1 {
		t.Errorf("BUG PIN: expected the current (wrong) status=200 series to hold 1, got %v", v)
	}
	if got := matching(t, metricRequestsTotal, series{
		"method": http.MethodGet, "path": "/mt6/err404", "status": "404",
	}); len(got) != 0 {
		t.Logf("status=404 is now recorded — the bug pinned here appears fixed, update this test")
	}
	t.Logf("BUG: GET /mt6/err404 answered 404 but recorded http_requests_total{status=\"200\"} (middleware/prometheus.go:65-68 reads resp.Status before Echo.HTTPErrorHandler runs)")
}

// TestPrometheusBugGzipWrappedStatusRecordedAs200 pins a second REAL BUG.
//
// The status is read with `c.Response().(*echo.Response)`
// (prometheus.go:66-68) and silently falls back to 200 when the assertion
// fails. Echo's Gzip middleware replaces the response writer with a
// *middleware.gzipResponseWriter whenever the client sends
// "Accept-Encoding: gzip" (compress.go: c.SetResponse(grw)). main.go registers
// Gzip (main.go:95) BEFORE remittmiddleware.Prometheus() (main.go:98), i.e.
// Gzip is OUTER, so for every gzip-capable client the assertion fails and the
// recorded status is hardcoded 200 — even for a status the handler explicitly
// wrote. The same server without the Accept-Encoding header records correctly,
// which isolates the wrapping as the trigger.
func TestPrometheusBugGzipWrappedStatusRecordedAs200(t *testing.T) {
	t.Parallel()

	e := newTestServer(echoMW.Gzip())
	e.GET("/mt7/teapot", func(c *echo.Context) error { return c.String(http.StatusTeapot, "nope") })
	e.GET("/mt7/plain", func(c *echo.Context) error { return c.String(http.StatusTeapot, "nope") })

	gz := doRequest(e, http.MethodGet, "/mt7/teapot", map[string]string{"Accept-Encoding": "gzip"})
	if gz.Code != http.StatusTeapot {
		t.Fatalf("gzip response code = %d, want %d", gz.Code, http.StatusTeapot)
	}
	plain := doRequest(e, http.MethodGet, "/mt7/plain", nil)
	if plain.Code != http.StatusTeapot {
		t.Fatalf("plain response code = %d, want %d", plain.Code, http.StatusTeapot)
	}

	// Control: without gzip wrapping the status IS recorded correctly.
	if v := counterValue(t, http.MethodGet, "/mt7/plain", "418"); v != 1 {
		t.Errorf("control: http_requests_total{path=/mt7/plain,status=418} = %v, want 1", v)
	}

	// BUG PIN: with gzip wrapping the handler-written 418 is recorded as 200.
	if v := counterValue(t, http.MethodGet, "/mt7/teapot", "200"); v != 1 {
		t.Errorf("BUG PIN: expected the wrong status=200 series for /mt7/teapot to hold 1, got %v", v)
	}
	if got := matching(t, metricRequestsTotal, series{
		"method": http.MethodGet, "path": "/mt7/teapot", "status": "418",
	}); len(got) != 0 {
		t.Logf("status=418 is now recorded for gzip clients — the bug pinned here appears fixed, update this test")
	}
	t.Logf("BUG: with Gzip registered outside Prometheus (main.go:95 vs main.go:98) any request carrying Accept-Encoding: gzip is recorded status=\"200\" (middleware/prometheus.go:66 type assertion fails on *middleware.gzipResponseWriter, which only exposes Unwrap())")
}

// ---------------------------------------------------------------------------
// 5. Panics
// ---------------------------------------------------------------------------

// TestPrometheusBugPanickedRequestIsNeverRecorded documents what IS wired and
// the gap: with echo's Recover OUTSIDE Prometheus (exactly main.go:85 vs 98) a
// panic is converted into a 500 for the client and the in-flight gauge is
// correctly decremented by the defer at prometheus.go:59 — but the recording
// block at prometheus.go:63-76 never runs, because a panic unwinds past it.
// The request leaves no trace in http_requests_total or
// http_request_duration_seconds at all, so panicking endpoints are invisible to
// both throughput and latency dashboards (and to the error-rate metric even if
// the status-label bug above were fixed).
func TestPrometheusBugPanickedRequestIsNeverRecorded(t *testing.T) {
	// Not parallel: it asserts on the shared in-flight gauge (see the note on
	// TestPrometheusCounterAndInFlightGauge).
	e := newTestServer(echoMW.Recover())
	e.GET("/mt8/panic", func(c *echo.Context) error { panic("boom") })
	e.GET("/mt8/ok", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	rec := doRequest(e, http.MethodGet, "/mt8/panic", nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panicking route response code = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	// Control: the same server records a non-panicking request, proving the
	// middleware was live for the panicking request too.
	doRequest(e, http.MethodGet, "/mt8/ok", nil)
	if v := counterValue(t, http.MethodGet, "/mt8/ok", "200"); v != 1 {
		t.Errorf("control: counter for /mt8/ok = %v, want 1", v)
	}

	if got := matching(t, metricRequestsTotal, series{"path": "/mt8/panic"}); len(got) != 0 {
		t.Logf("the panicked request is now counted (%v) — the gap documented here appears fixed, update this test", got)
	}
	count, _ := histogramObservation(t, http.MethodGet, "/mt8/panic", "500")
	if count != 0 {
		t.Logf("panicked request now observed in the histogram (count=%d) — update this test", count)
	}

	// The in-flight gauge is the one thing that DOES work on the panic path.
	if v := testutil.ToFloat64(httpRequestsInFlight); v != 0 {
		t.Errorf("in-flight gauge after a panic = %v, want 0", v)
	}
	t.Logf("GAP: a panicking request returns 500 but is absent from http_requests_total and http_request_duration_seconds (middleware/prometheus.go:61-76 run only on a normal return)")
}

// TestPrometheusBugRejectedAuthRequestsAreNotCounted pins a third finding,
// this one about middleware ORDER rather than about prometheus.go itself.
//
// main.go registers Prometheus last (main.go:98), i.e. innermost, so it only
// runs after every outer middleware has called next. A middleware that
// short-circuits and returns an error — BasicAuth (main.go:86) on a missing or
// bad credential, the classic 401 — never invokes the inner chain, so the
// request produces NO series at all in http_requests_total or
// http_request_duration_seconds. Rejected-auth traffic is therefore completely
// invisible to the metrics (not merely mislabelled), which is the worst case
// for a security dashboard; the same holds for any future outer middleware that
// short-circuits (IP allow-lists, rate limiters, CSRF, ...).
//
// The fix is ordering, not maths: register Prometheus FIRST (outermost, e.g.
// before Recover) so it observes the final status via echo's HTTPErrorHandler
// path, or record from a Pre()/HTTPErrorHandler hook. Both change production
// wiring in main.go, which is outside this task's scope, so this test pins the
// current behaviour.
func TestPrometheusBugRejectedAuthRequestsAreNotCounted(t *testing.T) {
	t.Parallel()

	e := echo.New()
	// Same relative order as main.go:35-98: an outer middleware that
	// short-circuits, then Prometheus inside it.
	e.Use(echoMW.BasicAuth(func(c *echo.Context, username, password string) (bool, error) {
		return false, nil
	}))
	e.Use(Prometheus())
	e.GET("/mt9/protected", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	rec := doRequest(e, http.MethodGet, "/mt9/protected", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated response code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	if got := matching(t, metricRequestsTotal, series{"path": "/mt9/protected"}); len(got) != 0 {
		t.Logf("rejected-auth requests are now counted (%v) — the gap documented here appears fixed, update this test", got)
	}
	t.Logf("GAP: a 401 rejected by BasicAuth produces no http_requests_total series at all (main.go:98 registers Prometheus inside BasicAuth at main.go:86, so the short-circuit never reaches it)")
}

// ---------------------------------------------------------------------------
// 7. Registration constraint
// ---------------------------------------------------------------------------

// TestPrometheusMetricNamesAreGlobalInDefaultRegistry demonstrates the
// constraint that prevents per-test registries: the names live in the
// process-global default registry, registered by promauto at package init, and
// a second registration of the same name panics. Any test file or package that
// (re)registers "http_requests_total" in the same test binary trips this — the
// same failure mode a test suite would hit if it tried to build a fresh
// registry by registering the middleware's metrics again. That is why this
// suite reads the default registry with unique label values per test instead,
// and why prometheus.go would need an exported constructor taking a
// prometheus.Registerer (promauto.With) before per-test registries become
// possible.
func TestPrometheusMetricNamesAreGlobalInDefaultRegistry(t *testing.T) {
	// (a) An identical descriptor (same name, help and label names) collides
	// with the metric registered by prometheus.go:15-21.
	dup := recoverPanic(t, func() {
		prometheus.MustRegister(prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricRequestsTotal,
				Help: "Total number of HTTP requests.", // identical to prometheus.go:18
			},
			[]string{"method", "path", "status"}, // identical to prometheus.go:20
		))
	})
	already, ok := dup.(prometheus.AlreadyRegisteredError)
	if !ok {
		t.Fatalf("(a) expected prometheus.AlreadyRegisteredError, got %T: %v", dup, dup)
	}
	if !strings.Contains(already.Error(), "duplicate metrics collector registration attempted") {
		t.Fatalf("(a) unexpected duplicate-registration error text: %v", already)
	}

	// (b) Same fully-qualified name but a different help string is a different
	// failure with the same cause: the fqName is a single global namespace, so
	// the name cannot be reused by anything else in the process either.
	conflict := recoverPanic(t, func() {
		prometheus.MustRegister(prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: metricRequestsTotal, Help: "different help"},
			[]string{"method"},
		))
	})
	if !strings.Contains(toErrorString(conflict), "has different label names or a different help string") {
		t.Fatalf("(b) unexpected error for a same-name/different-help collector: %v", conflict)
	}
}

// recoverPanic runs f and returns the recovered panic value, failing the test
// if f did not panic.
func recoverPanic(t *testing.T, f func()) any {
	t.Helper()
	var r any
	func() {
		defer func() { r = recover() }()
		f()
	}()
	if r == nil {
		t.Fatal("expected a panic, got none")
	}
	return r
}

func toErrorString(v any) string {
	if err, ok := v.(error); ok {
		return err.Error()
	}
	return ""
}
