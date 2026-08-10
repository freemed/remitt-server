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
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/soap"
	"github.com/freemed/remitt-server/task"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
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
