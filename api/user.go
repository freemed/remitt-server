package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["currentuser"] = func(r *gin.RouterGroup) {
		r.GET("/", apiGetUsername)
	}
}

func apiGetUsername(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	c.JSON(http.StatusOK, user)
}
