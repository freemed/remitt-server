package api

import (
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"strings"
)

func init() {
	common.ApiMap["file"] = func(r *gin.RouterGroup) {
		r.GET("/:category/:filename", GetFile)
	}
}

func GetFile(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	category := c.Param("category")
	filename := c.Param("filename")
	if category == "" || filename == "" {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	var o model.FileStoreModel
	err := model.DbMap.SelectOne("SELECT * FROM "+model.TABLE_FILE_STORE+" WHERE user = ? AND category = ? AND filename = ?", user, category, filename)
	if err != nil {
		log.Print(err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	// Clumsy content-type detection
	var contentType string
	switch {
	case strings.HasSuffix(filename, ".html"):
		contentType = "text/html"
	case strings.HasSuffix(filename, ".json"):
		contentType = "application/json"
	case strings.HasSuffix(filename, ".pdf"):
		contentType = "application/pdf"
	case strings.HasSuffix(filename, ".txt"):
		contentType = "text/plain"
	case strings.HasSuffix(filename, ".x12"):
		contentType = "application/edi-x12"
	case strings.HasSuffix(filename, ".xml"):
		contentType = "text/xml"
	default:
		contentType = "application/octet-stream"
	}

	c.Data(http.StatusOK, contentType, o.Content)
}
