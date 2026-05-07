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
	common.ApiMap["config"] = func(g *echo.Group) {
		g.GET("/all", a.ConfigGetAll)
		g.POST("/set/:namespace/:option/:value", a.ConfigSetValue)
	}
}

func (a Api) ConfigGetAll(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)
	tag := fmt.Sprintf("api.ConfigGetAll(%s): ", user)
	o, err := model.GetConfigValues(user)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, o)
}

func (a Api) ConfigSetValue(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	namespace := c.Param("namespace")
	option := c.Param("option")
	value := c.Param("value")

	tag := fmt.Sprintf("api.ConfigSetValue(%s,%s,%s) [%s]: ", namespace, option, value, user)

	err := model.SetConfigValue(user, namespace, option, []byte(value))
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, true)
}
