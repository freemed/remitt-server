package api

import (
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
)

func init() {
	common.ApiMap["status"] = func(r *gin.RouterGroup) {
		r.POST("/:id", GetStatus)
	}
}

type getStatusResult struct {
	Status int    `db:"status" json:"status"`
	Stage  string `db:"stage" json:"stage"`
}

func GetStatus(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	payloadId, err := common.ParamInt(c, "id")
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusBadRequest, err)
		return
	}

	var obj getStatusResult
	err = model.DbMap.SelectOne(&obj, "CALL p_Status( ?, ? );", user, payloadId)
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}
	c.JSON(http.StatusOK, obj)
}
