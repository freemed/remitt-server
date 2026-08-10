package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/parser"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["parser"] = func(g *echo.Group) {
		g.POST("/parse", a.ParseData)
	}
}

type parseRequest struct {
	Plugin string `json:"plugin"`
	Data   string `json:"data"`
}

// ParseData parses X12 EDI data using the specified parser plugin.
func (a Api) ParseData(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	var req parseRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if req.Plugin == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "plugin is required")
	}
	if req.Data == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "data is required")
	}

	p, err := parser.InstantiateParser(req.Plugin)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Set context on the parser instance
	_ = p.SetContext(c.Request().Context())

	result, err := p.ParseData(req.Data)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	_ = user // user is available for audit/logging if needed

	return c.JSON(http.StatusOK, map[string]string{
		"result": result,
	})
}
