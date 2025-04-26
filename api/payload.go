package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["payload"] = func(r *gin.RouterGroup) {
		r.POST("/", a.PayloadInsert)
	}
}

func (a Api) PayloadInsert(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

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
	if c.BindJSON(&raw) != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
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
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, obj.Id)
}

func (a Api) PayloadResubmit(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	tag := fmt.Sprintf("api.PayloadResubmit() [%s]: ", user)

	id, err := common.ParamInt(c, "id")
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	obj, err := model.DbMap.Get(model.PayloadModel{}, id)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	payload := obj.(*model.PayloadModel)

	if payload.User != user {
		log.Printf(tag+"payload user is not correct : %s != %s", user, payload.User, user)
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	// Overload for insert
	payload.Id = 0
	payload.InsertStamp = time.Now()
	payload.PayloadState = model.PayloadStateValid

	// Reinsert
	err = model.DbMap.Insert(&payload)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, payload.Id)
}
