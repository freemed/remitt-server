package api

import (
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

func init() {
	common.ApiMap["plugins"] = func(r *gin.RouterGroup) {
		r.GET("/", PluginsGetAll)
	}
}

func PluginsGetAll(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	cat := c.Param("category")

	switch cat {
	case "validation":
	case "render":
	case "translation":
	case "transport":
	case "eligibility":
	case "scooper":
	default:
		log.Printf("PluginsGetAll() [%s]: Could not find plugins for category %s", user, cat)
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	o, err := model.GetPluginsForCategory(cat)
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, o)
}
