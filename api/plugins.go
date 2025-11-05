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
	common.ApiMap["plugins"] = func(r *gin.RouterGroup) {
		r.GET("/get/:category", a.PluginsGetAll)
		r.GET("/options/:plugin", a.PluginGetOptions)
	}
}

func (a Api) PluginsGetAll(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	cat := c.Param("category")

	tag := fmt.Sprintf("api.PluginsGetAll(%s) [%s]: ", cat, user)

	switch cat {
	case "validation":
	case "render":
	case "translation":
	case "transport":
	case "eligibility":
	case "scooper":
		break
	default:
		log.Printf(tag+"Could not find plugins for category %s", cat)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	o, err := model.GetPluginsForCategory(cat)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, o)
}

func (a Api) PluginGetOptions(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	p := c.Param("plugin")

	tag := fmt.Sprintf("api.PluginGetOptions(%s) [%s]: ", p, user)

	o, err := model.GetPluginOptions(p)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, o)
}
