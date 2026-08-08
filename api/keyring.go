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

func (a Api) KeyringAdd(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	type keyringInput struct {
		KeyName    string `json:"keyname"`
		PrivateKey []byte `json:"privatekey"`
		PublicKey  []byte `json:"publickey"`
	}

	var raw keyringInput
	if err := c.Bind(&raw); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	err := model.AddKeyToKeyring(user, raw.KeyName, raw.PrivateKey, raw.PublicKey)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, true)
}
