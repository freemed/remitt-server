package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["currentuser"] = func(r *gin.RouterGroup) {
		r.GET("/", a.GetUsername)
		r.POST("/password", a.ChangePassword)
	}
	common.ApiMap["user"] = func(r *gin.RouterGroup) {
		r.GET("/list", a.UserList)
	}
}

func (a Api) GetUsername(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	c.JSON(http.StatusOK, user)
}

func (a Api) ChangePassword(c *gin.Context) {
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

func (a Api) UserList(c *gin.Context) {
	a.aclRequireRole(c, "admin")

	o := []string{}
	_, err := model.DbMap.Select(&o, "SELECT username FROM "+model.TABLE_USER+" ORDER BY username")
	if err != nil {
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, o)
}
