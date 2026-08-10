package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["keyring"] = func(g *echo.Group) {
		g.POST("/add", a.KeyringAdd)
	}
}

// keyringInput is the JSON request body for adding a key to the keyring.
type keyringInput struct {
	KeyName    string `json:"key_name"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

func (a Api) KeyringAdd(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	var raw keyringInput
	if err := c.Bind(&raw); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	err := model.AddKeyToKeyring(user, raw.KeyName, []byte(raw.PrivateKey), []byte(raw.PublicKey))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, true)
}
