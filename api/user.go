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

// optionalUserString maps an optional user field to the column representation the
// API means: a value the caller actually supplied is stored, and an omitted or
// empty one stays SQL NULL ("not configured") rather than becoming an empty
// string. This has to be explicit because model.NewNullStringValue marks any
// string valid, including the empty string, which would otherwise flip every
// blank credential column from NULL to an empty value now that the constructor
// behaves correctly.
func optionalUserString(s string) model.NullString {
	if s == "" {
		return model.NullString{}
	}
	return model.NewNullStringValue(s)
}

func (a Api) UserAdd(c *echo.Context) error {
	if err := a.aclRequireRole(c, "admin"); err != nil {
		return err
	}

	type userInput struct {
		Username string `json:"username"`
		Password string `json:"password"`
		// Role is the rolename to grant in tRole. tUser has no role column, so
		// model.AddUser writes it as a tRole row (the Java's UserManagement.addUser
		// does the same, hardcoding 'default' because its API has no role input);
		// an empty value becomes 'default' rather than leaving the new account
		// with no role at all.
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
		ContactEmail:           optionalUserString(raw.ContactEmail),
		CallbackServiceUri:     raw.CallbackServiceUri,
		CallbackServiceWsdlUri: raw.CallbackServiceWsdlUri,
		CallbackUsername:       optionalUserString(raw.CallbackUsername),
		CallbackPassword:       optionalUserString(raw.CallbackPassword),
	}

	id, err := model.AddUser(u)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, id)
}
