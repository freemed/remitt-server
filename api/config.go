package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["config"] = func(r *gin.RouterGroup) {
		r.GET("/all", apiConfigGetAll)
		r.POST("/set/:namespace/:option/:value", apiConfigSetValue)
	}
}

func apiConfigGetAll(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	tag := fmt.Sprintf("ConfigGetAll(%s): ", user)
	o, err := model.GetConfigValues(user)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

func apiConfigSetValue(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	namespace := c.Param("namespace")
	option := c.Param("option")
	value := c.Param("value")

	tag := fmt.Sprintf("ConfigSetValue(%s,%s,%s) [%s]: ", namespace, option, value, user)

	err := model.SetConfigValue(user, namespace, option, []byte(value))
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, true)
}
