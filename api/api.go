package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/gin-gonic/gin"
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
	common.ApiMap["ping"] = func(r *gin.RouterGroup) {
		r.POST("/:text", a.Ping)
	}
}

func (a Api) Ping(c *gin.Context) {
	c.JSON(http.StatusOK, c.Param("text"))
}

// aclRequireRole requires a certain role before it will grant access
func (a Api) aclRequireRole(c *gin.Context, role string) {
	r, exists := c.Get("roles")
	if !exists {
		c.AbortWithStatus(http.StatusNetworkAuthenticationRequired)
	}

	for _, x := range r.([]string) {
		if x == role {
			return
		}
	}
	c.AbortWithStatus(http.StatusNetworkAuthenticationRequired)
}

func (a Api) isAdmin(c *gin.Context) bool {
	r, exists := c.Get("roles")
	if !exists {
		return false
	}

	for _, role := range r.([]string) {
		if role == "admin" {
			return true
		}
	}
	return false
}
