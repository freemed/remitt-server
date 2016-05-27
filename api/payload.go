package api

import (
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

func init() {
	common.ApiMap["payload"] = func(r *gin.RouterGroup) {
		r.POST("/", PayloadInsert)
	}
}

func PayloadInsert(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	// TODO: FIXME: GET DATA
	payload := []byte{}

	obj := model.PayloadModel{
		User: user,
		Payload: payload,
		RenderPlugin: "",
		RenderOption: "",
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

