package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["plugins"] = func(g *echo.Group) {
		g.GET("/get/:category", a.PluginsGetAll)
		g.GET("/options/:plugin", a.PluginGetOptions)
	}
}

func (a Api) PluginsGetAll(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)
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
		return echo.NewHTTPError(http.StatusBadRequest, http.StatusText(http.StatusBadRequest))
	}

	o, err := model.GetPluginsForCategory(cat)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, o)
}

func (a Api) PluginGetOptions(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)
	p := c.Param("plugin")

	tag := fmt.Sprintf("api.PluginGetOptions(%s) [%s]: ", p, user)

	o, err := model.GetPluginOptions(p)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, o)
}
