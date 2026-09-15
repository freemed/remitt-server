package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["payload"] = func(g *echo.Group) {
		g.POST("/", a.PayloadInsert)

		// Registered as GET, and MUTATES state: PayloadResubmit inserts a new
		// payload row and returns its id. GET is used purely for client
		// compatibility -- client/client.go:272 (PayloadResubmit) issues
		// `GET /api/payload/resubmit/<id>`, so GET is the only verb that makes
		// Api.PayloadResubmit reachable for the shipped client. There is no
		// Java-era HTTP verb to consult: the original Java server exposes
		// resubmitPayload only as a SOAP operation
		// (../remitt/.../server/Service.java:99, adapted at
		// soap/adapters.go:108), i.e. the Go REST surface is a Go-ism and the
		// shipped client is the de-facto contract. Note this is an unsafe-method
		// purge on a safe verb: any intermediary (browser prefetch, crawler,
		// proxy retry) may trigger a real resubmission.
		g.GET("/resubmit/:id", a.PayloadResubmit)
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

	params := dbgen.InsertPayloadParams{
		User:            user,
		Payload:         sql.NullString{String: string(obj.Payload), Valid: true},
		RenderPlugin:    obj.RenderPlugin,
		RenderOption:    obj.RenderOption,
		TransportPlugin: obj.TransportPlugin,
		TransportOption: sql.NullString{String: obj.TransportOption, Valid: obj.TransportOption != ""},
		OriginalID:      sql.NullString{String: string(obj.OriginalId.String), Valid: obj.OriginalId.Valid},
	}
	result, err := model.Queries.InsertPayload(context.Background(), params)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	id, _ := result.LastInsertId()
	return c.JSON(http.StatusOK, id)
}

func (a Api) PayloadResubmit(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	tag := fmt.Sprintf("api.PayloadResubmit() [%s]: ", user)

	id, err := common.ParamInt(c, "id")
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	p, err := model.Queries.GetPayloadById(context.Background(), id)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if p.User != user {
		log.Printf(tag+"payload user is not correct : %s != %s", user, p.User)
		return echo.NewHTTPError(http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))
	}

	params := dbgen.ResubmitPayloadParams{ID: id, User: user}
	result, err := model.Queries.ResubmitPayload(context.Background(), params)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	newId, _ := result.LastInsertId()
	return c.JSON(http.StatusOK, newId)
}
