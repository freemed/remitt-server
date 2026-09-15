package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	_ "github.com/freemed/remitt-server/api"
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/jobqueue"
	remittmiddleware "github.com/freemed/remitt-server/middleware"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/soap"
	"github.com/freemed/remitt-server/task"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	configFile = flag.String("config-file", "./remitt.yml", "Configuration file")
	debug      = flag.Bool("debug", false, "Enable debugging (overrides config)")
)

func main() {
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Ltime | log.Lshortfile)

	c, err := config.LoadConfigWithDefaults(*configFile)
	if err != nil {
		panic(err)
	}
	if c == nil {
		panic("UNABLE TO LOAD CONFIG")
	}
	config.Config = c

	if *debug {
		log.Print("Overriding existing debug configuration")
		config.Config.Debug = true
	}

	log.Print("Initializing database backend")
	model.InitDb()

	if config.Config.Paths.TemporaryPath != "/tmp" {
		log.Print("Ensuring temporary directory exists")
		err = os.MkdirAll(config.Config.Paths.TemporaryPath, 0o700)
		if err != nil {
			panic(err)
		}
	}

	log.Printf("Initializing worker threads")
	jobqueue.StartDispatcher(config.Config.TimingIterations.NumWorkerThreads)

	log.Print("Initializing task scheduler")
	scheduler := task.NewScheduler()
	scheduler.Start()
	defer scheduler.Stop()

	log.Print("Initializing web services")
	e := echo.New()

	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		var he *echo.HTTPError
		if errors.As(err, &he) {
			_ = c.JSON(he.Code, map[string]interface{}{
				"error": he.Message,
			})
		} else {
			_ = c.JSON(http.StatusInternalServerError, map[string]interface{}{
				"error": err.Error(),
			})
		}
	}

	// REQUIRED MIDDLEWARE ORDER (metrics correctness, do not reorder):
	//
	// Prometheus must be registered FIRST (outermost) and Recover/BasicAuth
	// must be INSIDE it. Two things depend on that:
	//
	//  1. A middleware registered outside Prometheus that short-circuits
	//     without calling next() is invisible to the metrics. BasicAuth
	//     returning 401 for a missing/bad credential is exactly that case, and
	//     rejected authentication is the traffic a security dashboard must see.
	//  2. The status of a request is only final once the response is committed.
	//     For a handler-returned error (and for every 404/405) that happens in
	//     e.HTTPErrorHandler below, which echo runs AFTER the whole middleware
	//     chain returns, so a recorder inside the chain cannot see it.
	//
	// middleware.Prometheus() handles (2) by recording from the response's
	// commit hook; (1) is purely this ordering. The order is asserted by
	// TestRemittServerRegistersPrometheusOutermost in middleware/prometheus_test.go.
	e.Use(remittmiddleware.Prometheus())

	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	e.Use(middleware.BasicAuth(func(c *echo.Context, username, password string) (bool, error) {
		return model.BasicAuthCallback(username, password), nil
	}))
	e.Use(LoadUserMiddleware())

	// SOAP compatibility layer (intercepts /services/interface)
	e.Use(soap.Middleware())

	// Enable gzip compression
	e.Use(middleware.Gzip())

	// Serve up the static UI...
	e.Static("/ui", "ui")
	e.File("/favicon.ico", "ui/favicon.ico")

	// ... with a redirection for the root page
	e.GET("/", func(c *echo.Context) error {
		return c.Redirect(http.StatusMovedPermanently, "./ui/index.html")
	})

	// Prometheus /metrics endpoint.
	//
	// NOTE (measured against echo v5, not assumed): in echo v5 e.Use(...) builds
	// ONE global chain that every request runs through, no matter whether the
	// route was registered before or after that e.Use call (echo.go:
	// buildRouterChains + serveHTTP use e.chain for all requests). Registering
	// this route before the /api group therefore does NOT put it outside
	// BasicAuth: an unauthenticated scrape gets 401, both with Prometheus
	// outermost and with the previous (Prometheus innermost) order. Exempting
	// /metrics needs a BasicAuth Skipper or route-level middleware, which is a
	// change to auth behaviour and was left alone here; with Prometheus
	// outermost those 401s are at least visible now
	// (http_requests_total{path="/metrics",status="401"}).
	e.GET("/metrics", echo.WrapHandler(promhttp.Handler()))

	api := e.Group("/api")

	// Iterate through initializing API maps
	for k, v := range common.ApiMap {
		f := make([]string, 0)
		f = append(f, "AUTH")

		log.Printf("Adding handler /api/%s [%s]", k, strings.Join(f, ","))
		v(api.Group("/" + k))
	}

	// HTTP
	log.Printf("Launching http on port :%d", config.Config.Port)
	log.Fatal(e.Start(fmt.Sprintf(":%d", config.Config.Port)))
}
