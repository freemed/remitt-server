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
		r.GET("/:category", apiPluginsGetAll)
	}
}

func apiPluginsGetAll(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)
	cat := c.Param("category")

	tag := fmt.Sprintf("apiPluginsGetAll(%s) [%s]: ", cat, user)

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
