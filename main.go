package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/braintree/manners"
	_ "github.com/freemed/remitt-server/api"
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/jobqueue"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/contrib/gzip"
	"github.com/gin-gonic/gin"
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
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	log.Print("Initializing database backend")
	model.DbMap = model.InitDb()

	if config.Config.Paths.TemporaryPath != "/tmp" {
		log.Print("Ensuring temporary directory exists")
		err = os.MkdirAll(config.Config.Paths.TemporaryPath, 0700)
		if err != nil {
			panic(err)
		}
	}

	log.Printf("Initializing worker threads")
	jobqueue.StartDispatcher(config.Config.TimingIterations.NumWorkerThreads)

	log.Print("Initializing web services")
	m := gin.New()
	m.Use(gin.Logger())
	m.Use(gin.Recovery())
	m.Use(BasicAuth(model.BasicAuthCallback, "REMITT"))

	// Enable gzip compression
	m.Use(gzip.Gzip(gzip.DefaultCompression))

	// Serve up the static UI...
	m.Static("/ui", "./ui")
	m.StaticFile("/favicon.ico", "./ui/favicon.ico")

	// ... with a redirection for the root page
	m.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "./ui/index.html")
	})

	a := m.Group("/api")

	// Iterate through initializing API maps
	for k, v := range common.ApiMap {
		f := make([]string, 0)
		f = append(f, "AUTH")

		log.Printf("Adding handler /api/%s [%s]", k, strings.Join(f, ","))
		v(a.Group("/" + k))
	}

	// HTTP
	log.Printf("Launching http on port :%d", config.Config.Port)
	log.Fatal(manners.ListenAndServe(fmt.Sprintf(":%d", config.Config.Port), m))
}
