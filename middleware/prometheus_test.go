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
// # What this suite pins
//
// The middleware records the status the CLIENT received, counts requests that
// an inner middleware short-circuits (BasicAuth 401s), and records requests
// that panic. The four tests that used to pin the opposite (buggy) behaviour
// have been retargeted and renamed off the "Bug" framing:
// TestPrometheusHTTPErrorStatusMatchesClientStatus,
// TestPrometheusGzipWrappedStatusMatchesClientStatus,
// TestPrometheusPanickedRequestIsRecorded and
// TestPrometheusRejectedAuthRequestsAreCounted.
//
// Every test builds its server the way cmd/remitt-server/main.go does:
// Prometheus is registered OUTERMOST (first), and anything that can
// short-circuit the chain or render an error lives INSIDE it. Registering
// Prometheus inside such a middleware is what made 401s invisible and made
// error statuses unobservable, so the helper below mirrors production order
// and TestRemittServerRegistersPrometheusOutermost guards main.go itself.
package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
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

// newTestServer builds an echo server wired like cmd/remitt-server/main.go:
// Prometheus is registered FIRST, so it ends up outside every inner middleware
// and observes requests that an inner middleware short-circuits (BasicAuth) or
// renders through Echo's HTTPErrorHandler (Recover, the error handler itself).
// Middleware passed in inner is registered INSIDE Prometheus, mirroring main.go
// where Recover/BasicAuth/Gzip all sit inside it.
func newTestServer(inner ...echo.MiddlewareFunc) *echo.Echo {
	e := echo.New()
	e.Use(Prometheus())
	for _, mw := range inner {
		e.Use(mw)
	}
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

// TestPrometheusHTTPErrorStatusMatchesClientStatus (formerly
// TestPrometheusBugHTTPErrorStatusRecordedAs200).
//
// A handler that merely RETURNS an error has written nothing: Echo renders it
// afterwards, in Echo.HTTPErrorHandler, which runs in ServeHTTP *after* the
// middleware chain returns (echo.go: serveHTTP -> e.HTTPErrorHandler). A
// recorder that reads the response on the way out of next() sees no status and
// must not invent "200" — error-rate alerting built on http_requests_total
// would read zero for every 404/405/500.
//
// The middleware therefore defers the recording of this path to
// Response.Before, which Echo fires from inside Response.WriteHeader with the
// final code. The assertion below is that the recorded status EQUALS the status
// the client received (the 404 the client sees is the 404 in the metric), and
// that no status="200" series is minted for the route.
func TestPrometheusHTTPErrorStatusMatchesClientStatus(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	e.GET("/mt6/err404", func(c *echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound, "gone") // never writes a status itself
	})

	rec := doRequest(e, http.MethodGet, "/mt6/err404", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("response code = %d, want %d (echo rendered the error)", rec.Code, http.StatusNotFound)
	}

	if got := matching(t, metricRequestsTotal, series{
		"method": http.MethodGet, "path": "/mt6/err404", "status": "200",
	}); len(got) != 0 {
		t.Errorf("handler-returned error still recorded as 200: %v", got)
	}
	if v := counterValue(t, http.MethodGet, "/mt6/err404", "404"); v != 1 {
		t.Errorf("http_requests_total{path=/mt6/err404,status=404} = %v, want 1 (the status the client received)", v)
	}
	count, _ := histogramObservation(t, http.MethodGet, "/mt6/err404", "404")
	if count != 1 {
		t.Errorf("http_request_duration_seconds{path=/mt6/err404,status=404} sample count = %d, want 1", count)
	}
}

// TestPrometheusCommittedStatusWinsWhenHandlerReturnsError covers the branch
// where a handler writes a status and THEN returns an error. The response is
// already committed, so the client keeps what was written and Echo's error
// render cannot change it (Response.WriteHeader refuses to rewrite a committed
// response); the metric must agree with the client instead of reporting the
// error's code, and must not record twice.
func TestPrometheusCommittedStatusWinsWhenHandlerReturnsError(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	e.GET("/mt12/committed", func(c *echo.Context) error {
		if err := c.String(http.StatusTeapot, "already sent"); err != nil {
			return err
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "too late")
	})

	rec := doRequest(e, http.MethodGet, "/mt12/committed", nil)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("response code = %d, want %d (the status already on the wire)", rec.Code, http.StatusTeapot)
	}
	if v := counterValue(t, http.MethodGet, "/mt12/committed", "418"); v != 1 {
		t.Errorf("http_requests_total{path=/mt12/committed,status=418} = %v, want 1", v)
	}
	if got := matching(t, metricRequestsTotal, series{"path": "/mt12/committed", "status": "500"}); len(got) != 0 {
		t.Errorf("recorded a status the client never received: %v", got)
	}
}

// TestPrometheusGzipWrappedStatusMatchesClientStatus (formerly
// TestPrometheusBugGzipWrappedStatusRecordedAs200).
//
// Echo's Gzip middleware replaces the response writer with a
// *middleware.gzipResponseWriter whenever the client sends
// "Accept-Encoding: gzip" (compress.go: c.SetResponse(grw)), and that type is
// not *echo.Response. A recorder that type-asserts the writer and silently
// falls back to 200 therefore recorded a bogus 200 for every gzip-capable
// client (i.e. every browser), even for a status the handler explicitly wrote.
//
// The middleware reads the status from the context's *echo.Response — reached
// through echo.UnwrapResponse, which walks the wrapper chain — and that is the
// writer every wrapper commits through, so the recorded status is the one the
// client got. The same route without Accept-Encoding is kept as the control.
func TestPrometheusGzipWrappedStatusMatchesClientStatus(t *testing.T) {
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

	// Control: without gzip wrapping the status is recorded correctly.
	if v := counterValue(t, http.MethodGet, "/mt7/plain", "418"); v != 1 {
		t.Errorf("control: http_requests_total{path=/mt7/plain,status=418} = %v, want 1", v)
	}

	// The gzip-wrapped request must be recorded with the same status the client
	// received, not with the old hardcoded 200.
	if got := matching(t, metricRequestsTotal, series{
		"method": http.MethodGet, "path": "/mt7/teapot", "status": "200",
	}); len(got) != 0 {
		t.Errorf("gzip-wrapped request still recorded as 200: %v", got)
	}
	if v := counterValue(t, http.MethodGet, "/mt7/teapot", "418"); v != 1 {
		t.Errorf("http_requests_total{path=/mt7/teapot,status=418} = %v, want 1 (gzip client saw 418)", v)
	}
	count, _ := histogramObservation(t, http.MethodGet, "/mt7/teapot", "418")
	if count != 1 {
		t.Errorf("http_request_duration_seconds{path=/mt7/teapot,status=418} sample count = %d, want 1", count)
	}
}

// TestPrometheusRouterLevelNotFoundIsRecorded is the 404/405 half of the same
// fix: Echo's router answers an unmatched request by returning ErrNotFound from
// its not-found handler, so the 404 is rendered by HTTPErrorHandler *after* the
// chain returns, exactly like a handler-returned error. Both statuses must end
// up on the metric — the delta assertion below proves this request (and not just
// some other test's) was recorded, without depending on test order.
func TestPrometheusRouterLevelNotFoundIsRecorded(t *testing.T) {
	t.Parallel()

	e := newTestServer()
	before := counterValue(t, http.MethodGet, "", "404")

	rec := doRequest(e, http.MethodGet, "/mt11/never/registered", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unmatched route response code = %d, want %d", rec.Code, http.StatusNotFound)
	}

	after := counterValue(t, http.MethodGet, "", "404")
	if after <= before {
		t.Errorf("http_requests_total{path=\"\",status=404} = %v, want more than the %v observed before the unmatched request", after, before)
	}
	if got := matching(t, metricRequestsTotal, series{"path": "", "status": "200"}); len(got) != 0 {
		t.Errorf("unmatched route recorded as 200: %v", got)
	}
}

// ---------------------------------------------------------------------------
// 5. Panics
// ---------------------------------------------------------------------------

// TestPrometheusPanickedRequestIsRecorded (formerly
// TestPrometheusBugPanickedRequestIsNeverRecorded).
//
// Recover converts a panic into an error, which ServeHTTP hands to
// HTTPErrorHandler *after* the middleware chain returns. The request used to
// leave no trace at all: the recording block was skipped by the unwind, so
// panicking endpoints were invisible in http_requests_total AND
// http_request_duration_seconds. It is now recorded with the 500 the client
// received (via the same Response.Before path as any other handler error), while
// the in-flight gauge still returns to zero.
//
// Not parallel: it asserts on the shared in-flight gauge (see the note on
// TestPrometheusCounterAndInFlightGauge).
func TestPrometheusPanickedRequestIsRecorded(t *testing.T) {
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

	if v := counterValue(t, http.MethodGet, "/mt8/panic", "500"); v != 1 {
		t.Errorf("http_requests_total{path=/mt8/panic,status=500} = %v, want 1 (the panicked request must be counted)", v)
	}
	if got := matching(t, metricRequestsTotal, series{"path": "/mt8/panic", "status": "200"}); len(got) != 0 {
		t.Errorf("panicked request recorded as 200: %v", got)
	}
	count, _ := histogramObservation(t, http.MethodGet, "/mt8/panic", "500")
	if count != 1 {
		t.Errorf("http_request_duration_seconds{path=/mt8/panic,status=500} sample count = %d, want 1", count)
	}

	// The in-flight gauge must still return to zero on the panic path.
	if v := testutil.ToFloat64(httpRequestsInFlight); v != 0 {
		t.Errorf("in-flight gauge after a panic = %v, want 0", v)
	}
}

// TestPrometheusPanicWithoutRecoverIsStillRecorded covers the panic that no
// inner Recover converts into an error — Echo's Recover re-panics
// http.ErrAbortHandler, and a deployment need not wire Recover at all. Such a
// panic unwinds through the metric middleware and out of ServeHTTP, so
// HTTPErrorHandler is never called and there is no status to observe; the
// middleware records the attempt (500, since the server aborts the connection)
// from a deferred recover before re-panicking, so the request is not lost.
func TestPrometheusPanicWithoutRecoverIsStillRecorded(t *testing.T) {
	// Not parallel: asserts on the shared in-flight gauge.
	e := newTestServer() // no Recover: the panic escapes ServeHTTP
	e.GET("/mt10/panic", func(c *echo.Context) error { panic("boom without recover") })

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected the panic to propagate out of ServeHTTP when no Recover is registered")
			}
		}()
		doRequest(e, http.MethodGet, "/mt10/panic", nil)
	}()

	if v := counterValue(t, http.MethodGet, "/mt10/panic", "500"); v != 1 {
		t.Errorf("http_requests_total{path=/mt10/panic,status=500} = %v, want 1", v)
	}
	count, _ := histogramObservation(t, http.MethodGet, "/mt10/panic", "500")
	if count != 1 {
		t.Errorf("http_request_duration_seconds{path=/mt10/panic,status=500} sample count = %d, want 1", count)
	}
	if v := testutil.ToFloat64(httpRequestsInFlight); v != 0 {
		t.Errorf("in-flight gauge after an un-recovered panic = %v, want 0", v)
	}
}

// ---------------------------------------------------------------------------
// 6. Middleware order
// ---------------------------------------------------------------------------

// TestPrometheusRejectedAuthRequestsAreCounted (formerly
// TestPrometheusBugRejectedAuthRequestsAreNotCounted).
//
// This one is about ORDER, not about the status read. A request rejected by
// BasicAuth (missing/bad credentials -> 401) never invokes the chain inside it,
// so a Prometheus middleware registered INSIDE BasicAuth never sees it and the
// request produced no series at all: rejected authentication — the most
// security-relevant traffic there is — was completely invisible. Registering
// Prometheus outside BasicAuth counts it, and the 401 the client received is the
// 401 on the status label.
func TestPrometheusRejectedAuthRequestsAreCounted(t *testing.T) {
	t.Parallel()

	// Same relative order as main.go: Prometheus outermost, BasicAuth inside it.
	e := echo.New()
	e.Use(Prometheus())
	e.Use(echoMW.BasicAuth(func(c *echo.Context, username, password string) (bool, error) {
		return false, nil
	}))
	e.GET("/mt9/protected", func(c *echo.Context) error { return c.String(http.StatusOK, "ok") })

	rec := doRequest(e, http.MethodGet, "/mt9/protected", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated response code = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	if v := counterValue(t, http.MethodGet, "/mt9/protected", "401"); v != 1 {
		t.Errorf("http_requests_total{path=/mt9/protected,status=401} = %v, want 1 (rejected auth must be counted)", v)
	}
	count, _ := histogramObservation(t, http.MethodGet, "/mt9/protected", "401")
	if count != 1 {
		t.Errorf("http_request_duration_seconds{path=/mt9/protected,status=401} sample count = %d, want 1", count)
	}
	if got := matching(t, metricRequestsTotal, series{"path": "/mt9/protected", "status": "200"}); len(got) != 0 {
		t.Errorf("rejected request recorded as 200: %v", got)
	}
}

// TestRemittServerRegistersPrometheusOutermost is the wiring guard for the order
// this middleware requires. It reads cmd/remitt-server/main.go as source: main()
// is not callable from a test (it loads config and a database), and the
// registration is a plain sequence of e.Use(...) calls inside it, so the source
// IS the wiring. In echo v5 those calls build one global chain that every route
// runs through, which is why the order is load-bearing:
//   - Prometheus must be FIRST so nothing registered outside it can
//     short-circuit a request before it is observed (BasicAuth 401s).
//   - Recover and BasicAuth must come AFTER it so the statuses they produce
//     (500 for a panic, 401 for rejected credentials) are observed too.
func TestRemittServerRegistersPrometheusOutermost(t *testing.T) {
	t.Parallel()

	const mainGoPath = "../cmd/remitt-server/main.go"
	src, err := os.ReadFile(mainGoPath)
	if err != nil {
		t.Fatalf("read %s: %v (this guard asserts the production middleware order and must run from the middleware package directory)", mainGoPath, err)
	}

	var uses []string
	for _, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.Contains(trimmed, "e.Use(") {
			uses = append(uses, trimmed)
		}
	}
	if len(uses) == 0 {
		t.Fatalf("no e.Use(...) calls found in %s", mainGoPath)
	}

	indexOf := func(needle string) int {
		for i, u := range uses {
			if strings.Contains(u, needle) {
				return i
			}
		}
		return -1
	}

	promIdx := indexOf("remittmiddleware.Prometheus()")
	if promIdx != 0 {
		t.Errorf("Prometheus is registered at position %d of the %d e.Use(...) calls in %s, want position 0 (outermost):\n%s",
			promIdx, len(uses), mainGoPath, strings.Join(uses, "\n"))
	}
	for _, required := range []string{"middleware.Recover()", "middleware.BasicAuth("} {
		idx := indexOf(required)
		if idx == -1 {
			t.Errorf("%s is no longer registered in %s; this guard needs updating with the new wiring", required, mainGoPath)
			continue
		}
		if idx < promIdx {
			t.Errorf("%s is registered at position %d, before Prometheus (position %d); it must be INSIDE Prometheus or the status it renders (401/500) is never recorded:\n%s",
				required, idx, promIdx, strings.Join(uses, "\n"))
		}
	}

	// /metrics is served by the root Echo instance. Note that in echo v5 a
	// single global chain built from e.Use(...) serves EVERY route regardless of
	// registration order, so "registered before the /api group" is not by itself
	// an auth exemption — see the report accompanying this change.
	if !strings.Contains(string(src), `e.GET("/metrics"`) {
		t.Errorf(`no e.GET("/metrics", ...) registration found in %s; the scrape endpoint must stay on the root Echo instance`, mainGoPath)
	}
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
	// with the metric registered by prometheus.go.
	dup := recoverPanic(t, func() {
		prometheus.MustRegister(prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: metricRequestsTotal,
				Help: "Total number of HTTP requests.", // identical to prometheus.go
			},
			[]string{"method", "path", "status"}, // identical to prometheus.go
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
