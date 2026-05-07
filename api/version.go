package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["version"] = func(g *echo.Group) {
		g.GET("/", a.Version)
		g.GET("/info", a.Info)
		g.GET("/protocol", a.ProtocolVersion)
	}
}

func (a Api) Version(c *echo.Context) error {
	return c.JSON(http.StatusOK, ProtocolVersion)
}

func (a Api) Info(c *echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"version":        ProtocolVersion,
		"remote_address": c.Request().RemoteAddr,
		"user":           c.Request().URL.User.Username(),
	})
}

func (a Api) ProtocolVersion(c *echo.Context) error {
	return c.JSON(http.StatusOK, common.Version)
}
