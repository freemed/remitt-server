// Package middleware provides Echo v5 middleware components.
package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// httpRequestsTotal counts all HTTP requests by method, path, and status code.
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests.",
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestDuration tracks HTTP request duration in seconds.
	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	// httpRequestsInFlight tracks the number of in-flight HTTP requests.
	httpRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Current number of HTTP requests being served.",
		},
	)
)

// recordRequest is the single place where a request is counted and timed.
// It must be called exactly once per request, with the status the client
// actually received.
func recordRequest(method, path string, status int, start time.Time) {
	statusStr := strconv.Itoa(status)
	httpRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	httpRequestDuration.WithLabelValues(method, path, statusStr).Observe(time.Since(start).Seconds())
}

// Prometheus returns an Echo v5 middleware that records Prometheus metrics
// for every HTTP request.
//
// Metrics recorded:
//   - http_requests_total: counter by method, path, status
//   - http_request_duration_seconds: histogram by method, path, status
//   - http_requests_in_flight: gauge of concurrent requests
//
// # Registration order
//
// Register Prometheus OUTERMOST, i.e. before Recover, BasicAuth and anything
// else that can shorten the chain:
//
//	e.Use(middleware.Prometheus()) // must be first
//	e.Use(middleware.RequestLogger())
//	e.Use(middleware.Recover())
//	e.Use(middleware.BasicAuth(func(...) (bool, error) { ... }))
//
// Two reasons, both about observing the request the client actually got:
//
//   - A middleware registered OUTSIDE Prometheus that short-circuits without
//     calling next (BasicAuth rejecting a credential, an IP allow-list, a rate
//     limiter, CSRF) never reaches the recording code at all, so that traffic is
//     invisible in the metrics instead of merely mislabelled. Rejected
//     authentication is the most security-relevant traffic there is.
//   - A request's status is only final once the response has been committed.
//     For a handler that RETURNS an error (and for any 404/405), that happens in
//     Echo.HTTPErrorHandler, which ServeHTTP runs *after* the whole middleware
//     chain has returned (echo.go: serveHTTP -> e.HTTPErrorHandler). A recorder
//     that reads the status on the way out of next() therefore sees no status
//     at all and must not guess one.
//
// # Status reporting
//
// The recorded status is the status the client received:
//
//   - Normal completion: recorded from the context's *echo.Response, which is
//     the writer that talks to the transport, so it holds the code that was
//     actually written - including codes written through another middleware's
//     wrapper (Gzip replaces the response with its own writer once the client
//     sends Accept-Encoding: gzip, and that writer still commits through
//     *echo.Response).
//   - Handler-returned errors and panics: the response does not exist yet when
//     the chain returns, so recording is deferred to Response.Before, which
//     Echo fires exactly once from inside Response.WriteHeader with Status
//     already set to the code being sent. Echo's error handling can never
//     render a status that goes unrecorded, because it always commits through
//     that same Response.
//   - A panic that escapes every inner Recover (Echo's Recover re-panics
//     http.ErrAbortHandler, and Recover need not be wired at all) is recorded
//     from a deferred recover, then re-panicked, so the request is never lost
//     even though ServeHTTP never reaches HTTPErrorHandler for it.
//
// The one case that is not recorded is an application whose custom
// HTTPErrorHandler writes no response at all: there is no commit to observe and
// nothing on the wire to describe.
func Prometheus() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			httpRequestsInFlight.Inc()
			defer httpRequestsInFlight.Dec()

			// Routing has already run when the middleware chain executes
			// (echo.go: serveHTTP -> router.Route(c) -> e.chain(c)), so the
			// route template is resolved here, before the handler. Reading
			// method and path up front keeps the closure handed to
			// Response.Before free of *echo.Context references, so a status
			// written after this middleware returned cannot read a Context that
			// the pool has already recycled.
			method := c.Request().Method
			path := c.Path()

			resp, unwrapErr := echo.UnwrapResponse(c.Response())
			if unwrapErr != nil {
				// Defensive path: some outer middleware replaced the response
				// writer with a type that does not unwrap to *echo.Response, so
				// the final status cannot be observed. Record after the chain
				// with whatever status is visible, as the previous
				// implementation did.
				err := next(c)
				status := http.StatusOK
				if r, ok := c.Response().(*echo.Response); ok && r.Status != 0 {
					status = r.Status
				}
				recordRequest(method, path, status, start)
				return err
			}

			// A panic that no inner Recover converts into an error unwinds
			// through here, and ServeHTTP does not run HTTPErrorHandler for it,
			// so without this the request would leave no trace in either
			// metric. Record what the client got (the committed status, or 500
			// for a connection the server aborts) and let the panic continue.
			defer func() {
				if r := recover(); r != nil {
					status := http.StatusInternalServerError
					if resp.Committed {
						status = resp.Status
					}
					recordRequest(method, path, status, start)
					panic(r)
				}
			}()

			err := next(c)

			switch {
			case err == nil || resp.Committed:
				// The chain finished cleanly, or returned an error after a
				// status was already committed to the client. Either way the
				// status is final now: a handler that wrote nothing leaves
				// Status at 200, which is the response the client gets, and a
				// committed response cannot be changed by the error handler.
				// Recording here also times the request when the handler
				// finished rather than at its first byte.
				recordRequest(method, path, resp.Status, start)
			default:
				// The handler returned an error without writing anything, so
				// the client status does not exist yet: Echo renders it in
				// Echo.HTTPErrorHandler after this middleware has returned.
				// Response.Before fires exactly once, from inside
				// Response.WriteHeader, with Status already set to the code
				// being sent.
				resp.Before(func() {
					recordRequest(method, path, resp.Status, start)
				})
			}

			return err
		}
	}
}
