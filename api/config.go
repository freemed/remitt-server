package api

import (
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

func init() {
	common.ApiMap["config"] = func(r *gin.RouterGroup) {
		r.GET("/all", ConfigGetAll)
		r.POST("/set/:namespace/:option", ConfigSetValue)
		//r.Get("/view", MessagesView)
	}
}

func ConfigGetAll(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	o, err := model.GetConfigValues(user)
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

func ConfigSetValue(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	namespace := c.Param("namespace")
	option := c.Param("option")

	// TODO: FIXME: get actual value
	value := []byte{}

	err := model.SetConfigValue(user, namespace, option, value)
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, true)
}
