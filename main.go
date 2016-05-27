package main

import (
	"flag"
	"fmt"
	"github.com/braintree/manners"
	_ "github.com/freemed/remitt-server/api"
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/contrib/gzip"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"strings"
)

var (
	ConfigFile = flag.String("config-file", "./remitt.yml", "Configuration file")
	Debug      = flag.Bool("debug", false, "Enable debugging (overrides config)")
)

func main() {
	flag.Parse()

	c, err := config.LoadConfigWithDefaults(*ConfigFile)
	if err != nil {
		panic(err)
	}
	if c == nil {
		panic("UNABLE TO LOAD CONFIG")
	}
	config.Config = c

	if *Debug {
		log.Print("Overriding existing debug configuration")
		config.Config.Debug = true
	}

	log.Print("Initializing database backend")
	model.DbMap = model.InitDb()

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
