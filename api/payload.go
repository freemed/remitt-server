package api

import (
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"time"
)

func init() {
	common.ApiMap["payload"] = func(r *gin.RouterGroup) {
		r.POST("/", PayloadInsert)
		r.GET("/resubmit/:id", PayloadResubmit)
	}
}

func PayloadInsert(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	// TODO: FIXME: GET DATA
	payload := []byte{}

	obj := model.PayloadModel{
		User:            user,
		Payload:         payload,
		RenderPlugin:    "",
		RenderOption:    "",
		TransportPlugin: "",
		TransportOption: "",
	}

	err := model.DbMap.Insert(&obj)
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, obj.Id)
}

func PayloadResubmit(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	id, err := common.ParamInt(c, "id")
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	obj, err := model.DbMap.Get(model.PayloadModel{}, id)
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	payload := obj.(*model.PayloadModel)

	if payload.User != user {
		log.Printf("PayloadResubmit() [%s]: payload user is not correct : %s != %s", user, payload.User, user)
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
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, payload.Id)
}
