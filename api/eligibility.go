package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/eligibility"
	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["eligibility"] = func(g *echo.Group) {
		g.POST("/get", a.GetEligibility)
		g.POST("/batch", a.BatchEligibilityCheck)
	}
}

// GetEligibility runs an eligibility check synchronously via the named plugin.
func (a Api) GetEligibility(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	var req eligibility.EligibilityRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	checker, err := eligibility.InstantiateChecker(req.Plugin)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	response, err := checker.CheckEligibility(user, req.Request, false, 0)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, response)
}

// BatchEligibilityCheck enqueues multiple eligibility requests for async processing.
func (a Api) BatchEligibilityCheck(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	var reqs []eligibility.EligibilityRequest
	if err := c.Bind(&reqs); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	count := 0
	for _, req := range reqs {
		payload, _ := json.Marshal(req.Request)
		params := dbgen.InsertEligibilityJobParams{
			User:     user,
			Inserted: time.Now(),
			Plugin:   req.Plugin,
			Payload:  sql.NullString{String: string(payload), Valid: true},
		}
		if _, err := model.Queries.InsertEligibilityJob(context.Background(), params); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		count++
	}

	return c.JSON(http.StatusOK, count)
}
