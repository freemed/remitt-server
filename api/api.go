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

func init() {
	common.ApiMap["ping"] = func(r *gin.RouterGroup) {
		r.POST("/:text", apiPing)
	}
}

func apiPing(c *gin.Context) {
	c.JSON(http.StatusOK, c.Param("text"))
}
