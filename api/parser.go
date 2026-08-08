package api

import (
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/parser"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["parser"] = func(g *echo.Group) {
		g.POST("/parse/:parserClass", a.ParseData)
	}
}

func (a Api) ParseData(c *echo.Context) error {
	parserClass := c.Param("parserClass")

	p, err := parser.InstantiateParser(parserClass)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	data, err := common.BodyFromContext(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	result, err := p.ParseData(string(data))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}
