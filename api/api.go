package api

import (
	"net/http"

	"slices"

	"github.com/freemed/remitt-server/common"
	"github.com/labstack/echo/v5"
)

const (
	// ProtocolVersion defines the version of the protocol used by the
	// REMITT API
	ProtocolVersion = "0.6"
)

var (
	a Api
)

type Api struct {
}

func init() {
	common.ApiMap["ping"] = func(g *echo.Group) {
		g.POST("/:text", a.Ping)
	}
}

func (a Api) Ping(c *echo.Context) error {
	return c.JSON(http.StatusOK, c.Param("text"))
}

// aclRequireRole requires a certain role before it will grant access
func (a Api) aclRequireRole(c *echo.Context, role string) error {
	r := c.Get("roles")
	if r == nil {
		return echo.NewHTTPError(http.StatusNetworkAuthenticationRequired, http.StatusText(http.StatusNetworkAuthenticationRequired))
	}

	if slices.Contains(r.([]string), role) {
		return nil
	}
	return echo.NewHTTPError(http.StatusNetworkAuthenticationRequired, http.StatusText(http.StatusNetworkAuthenticationRequired))
}

func (a Api) isAdmin(c *echo.Context) bool {
	r := c.Get("roles")
	if r == nil {
		return false
	}

	return slices.Contains(r.([]string), "admin")
}
