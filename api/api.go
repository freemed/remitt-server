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
