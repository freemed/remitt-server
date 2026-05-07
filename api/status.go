package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["status"] = func(g *echo.Group) {
		g.GET("/:id", a.GetStatus)
		g.POST("/bulk/", a.GetBulkStatus)
	}
}

type getStatusResult struct {
	Status int    `db:"status" json:"status"`
	Stage  string `db:"stage" json:"stage"`
}

func (a Api) GetStatus(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	payloadID, err := common.ParamInt(c, "id")

	tag := fmt.Sprintf("api.GetStatus(%d) [%s]: ", payloadID, user)

	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	var obj getStatusResult
	err = model.DbMap.SelectOne(&obj, "CALL p_Status( ?, ? );", user, payloadID)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, obj)
}

func (a Api) GetBulkStatus(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	tag := fmt.Sprintf("api.GetBulkStatus() [%s]: ", user)

	var ids []int64
	err := c.Bind(&ids)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	out := map[int64]getStatusResult{}
	for _, id := range ids {
		var obj getStatusResult
		err = model.DbMap.SelectOne(&obj, "CALL p_Status( ?, ? );", user, id)
		if err != nil {
			log.Print(tag + err.Error())
			continue
		}
		out[id] = obj
	}
	return c.JSON(http.StatusOK, out)
}
