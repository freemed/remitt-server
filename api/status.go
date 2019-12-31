package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["status"] = func(r *gin.RouterGroup) {
		r.POST("/:id", apiGetStatus)
	}
}

type getStatusResult struct {
	Status int    `db:"status" json:"status"`
	Stage  string `db:"stage" json:"stage"`
}

func apiGetStatus(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	payloadID, err := common.ParamInt(c, "id")

	tag := fmt.Sprintf("apiGetStatus(%d) [%s]: ", payloadID, user)

	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	var obj getStatusResult
	err = model.DbMap.SelectOne(&obj, "CALL p_Status( ?, ? );", user, payloadID)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, obj)
}
