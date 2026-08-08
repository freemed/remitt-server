package api

import (
	"context"
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/internal/dbgen"
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
		g.POST("/add", a.UserAdd)
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
	err = model.Queries.ChangePassword(context.Background(), dbgen.ChangePasswordParams{
		Username: user,
		Passhash: pass,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, user)
}

func (a Api) UserList(c *echo.Context) error {
	if err := a.aclRequireRole(c, "admin"); err != nil {
		return err
	}

	o, err := model.Queries.ListUsers(context.Background())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, o)
}

func (a Api) UserAdd(c *echo.Context) error {
	if err := a.aclRequireRole(c, "admin"); err != nil {
		return err
	}

	type userInput struct {
		Username               string `json:"username"`
		Password               string `json:"password"`
		Role                   string `json:"role"`
		ContactEmail           string `json:"contact_email"`
		CallbackServiceUri     string `json:"callback_service_uri"`
		CallbackServiceWsdlUri string `json:"callback_service_wsdl_uri"`
		CallbackUsername       string `json:"callback_username"`
		CallbackPassword       string `json:"callback_password"`
	}

	var raw userInput
	if err := c.Bind(&raw); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	u := model.UserModel{
		Username:               raw.Username,
		PasswordHash:           raw.Password,
		Role:                   raw.Role,
		ContactEmail:           model.NewNullStringValue(raw.ContactEmail),
		CallbackServiceUri:     raw.CallbackServiceUri,
		CallbackServiceWsdlUri: raw.CallbackServiceWsdlUri,
		CallbackUsername:       model.NewNullStringValue(raw.CallbackUsername),
		CallbackPassword:       model.NewNullStringValue(raw.CallbackPassword),
	}

	id, err := model.AddUser(u)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, id)
}
