package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["currentuser"] = func(r *gin.RouterGroup) {
		r.GET("/", apiGetUsername)
		r.POST("/password", apiChangePassword)
	}
}

func apiGetUsername(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	c.JSON(http.StatusOK, user)
}

func apiChangePassword(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	var pass string
	err := c.BindJSON(&pass)
	if err != nil {
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	_, err = model.DbMap.Exec("UPDATE "+model.TABLE_USER+" SET passhash = ? WHERE username = ?", pass, user)
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, user)
}
