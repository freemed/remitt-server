package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/validation"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["validation"] = func(g *echo.Group) {
		g.POST("/validate/:validatorClass", a.ValidatePayload)
	}
}

// ValidatePayload runs a named validator against the request body.
func (a Api) ValidatePayload(c *echo.Context) error {
	validatorClass := c.Param("validatorClass")

	v, err := validation.InstantiateValidator(validatorClass)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	data, err := common.BodyFromContext(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	response, err := v.Validate(data)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, response)
}
