package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/validation"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["validation"] = func(g *echo.Group) {
		g.POST("/validate", a.ValidatePayload)
	}
}

// validateRequest is the JSON body for a validation request.
type validateRequest struct {
	Plugin string `json:"plugin"`
	Data   string `json:"data"`
}

// ValidatePayload runs a named validator against the provided X12 data.
func (a Api) ValidatePayload(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)
	_ = user // user is extracted and available for audit/logging

	// Read and parse the request body
	var req validateRequest
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Instantiate the validator plugin by name
	v, err := validation.InstantiateValidator(req.Plugin)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Run validation
	response, err := v.Validate([]byte(req.Data))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, response)
}
