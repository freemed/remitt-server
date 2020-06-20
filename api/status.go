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
		r.GET("/:id", apiGetStatus)
		r.POST("/bulk/", apiGetBulkStatus)
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

func apiGetBulkStatus(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	tag := fmt.Sprintf("apiGetBulkStatus() [%s]: ", user)

	var ids []int64
	err := c.BindJSON(&ids)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusBadRequest, err)
		return
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
	c.JSON(http.StatusOK, out)
}
