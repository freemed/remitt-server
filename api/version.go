package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["version"] = func(r *gin.RouterGroup) {
		r.GET("/", apiVersion)
		r.GET("/info", apiInfo)
		r.GET("/protocol", apiProtocolVersion)
	}
}

func apiVersion(c *gin.Context) {
	c.JSON(http.StatusOK, ProtocolVersion)
}

func apiInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":        ProtocolVersion,
		"remote_address": c.Request.RemoteAddr,
		"user":           c.Request.URL.User.Username(),
	})
}

func apiProtocolVersion(c *gin.Context) {
	c.JSON(http.StatusOK, common.Version)
}
