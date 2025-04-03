package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["version"] = func(r *gin.RouterGroup) {
		r.GET("/", a.Version)
		r.GET("/info", a.Info)
		r.GET("/protocol", a.ProtocolVersion)
	}
}

func (a Api) Version(c *gin.Context) {
	c.JSON(http.StatusOK, ProtocolVersion)
}

func (a Api) Info(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"version":        ProtocolVersion,
		"remote_address": c.Request.RemoteAddr,
		"user":           c.Request.URL.User.Username(),
	})
}

func (a Api) ProtocolVersion(c *gin.Context) {
	c.JSON(http.StatusOK, common.Version)
}
