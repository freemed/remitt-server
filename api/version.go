package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["version"] = func(r *gin.RouterGroup) {
		r.GET("/protocol", apiProtocolVersion)
	}
}

func apiProtocolVersion(c *gin.Context) {
	c.JSON(http.StatusOK, ProtocolVersion)
}
