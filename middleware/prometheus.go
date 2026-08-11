// Package middleware provides Echo v5 middleware components.
package middleware

import (
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

// Prometheus returns an Echo v5 middleware that records Prometheus metrics
// for every HTTP request.
//
// Metrics recorded:
//   - http_requests_total: counter by method, path, status
//   - http_request_duration_seconds: histogram by method, path, status
//   - http_requests_in_flight: gauge of concurrent requests
//
// Usage:
//
//	e := echo.New()
//	e.Use(middleware.Prometheus())
func Prometheus() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			start := time.Now()
			httpRequestsInFlight.Inc()
			defer httpRequestsInFlight.Dec()

			err := next(c)

			// c.Response() returns http.ResponseWriter; the underlying type
			// is *echo.Response which carries the Status field.
			status := 200
			if resp, ok := c.Response().(*echo.Response); ok {
				status = resp.Status
			}
			statusStr := strconv.Itoa(status)
			method := c.Request().Method
			path := c.Path()

			duration := time.Since(start).Seconds()

			httpRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
			httpRequestDuration.WithLabelValues(method, path, statusStr).Observe(duration)

			return err
		}
	}
}
