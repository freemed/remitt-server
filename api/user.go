package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["currentuser"] = func(g *echo.Group) {
		g.GET("/", a.GetUsername)
		g.POST("/password", a.ChangePassword)
	}
	common.ApiMap["user"] = func(g *echo.Group) {
		g.GET("/list", a.UserList)
	}
}

func (a Api) GetUsername(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)
	return c.JSON(http.StatusOK, user)
}

func (a Api) ChangePassword(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)
	var pass string
	err := c.Bind(&pass)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	_, err = model.DbMap.Exec("UPDATE "+model.TABLE_USER+" SET passhash = ? WHERE username = ?", pass, user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, user)
}

func (a Api) UserList(c *echo.Context) error {
	if err := a.aclRequireRole(c, "admin"); err != nil {
		return err
	}

	o := []string{}
	_, err := model.DbMap.Select(&o, "SELECT username FROM "+model.TABLE_USER+" ORDER BY username")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, o)
}
