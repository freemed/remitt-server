package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["payload"] = func(g *echo.Group) {
		g.POST("/", a.PayloadInsert)
	}
}

func (a Api) PayloadInsert(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	type inputPayload struct {
		OriginalID      model.NullString `json:"original_id"`
		InputPayload    string           `json:"input_payload"`
		RenderPlugin    string           `json:"render_plugin"`
		RenderOption    string           `json:"render_option"`
		TransportPlugin string           `json:"transport_plugin"`
		TransportOption string           `json:"transport_option"`
	}

	tag := fmt.Sprintf("api.PayloadInsert() [%s]: ", user)

	var raw inputPayload
	if err := c.Bind(&raw); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	obj := model.PayloadModel{
		User:            user,
		Payload:         []byte(raw.InputPayload),
		RenderPlugin:    raw.RenderPlugin,
		RenderOption:    raw.RenderOption,
		TransportPlugin: raw.TransportPlugin,
		TransportOption: raw.TransportOption,
		OriginalId:      raw.OriginalID,
	}

	err := model.DbMap.Insert(&obj)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, obj.Id)
}

func (a Api) PayloadResubmit(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	tag := fmt.Sprintf("api.PayloadResubmit() [%s]: ", user)

	id, err := common.ParamInt(c, "id")
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	obj, err := model.DbMap.Get(model.PayloadModel{}, id)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	payload := obj.(*model.PayloadModel)

	if payload.User != user {
		log.Printf(tag+"payload user is not correct : %s != %s", user, payload.User)
		return echo.NewHTTPError(http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))
	}

	// Overload for insert
	payload.Id = 0
	payload.InsertStamp = time.Now()
	payload.PayloadState = model.PayloadStateValid

	// Reinsert
	err = model.DbMap.Insert(&payload)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, payload.Id)
}
