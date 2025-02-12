package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model"
	"github.com/gin-gonic/gin"
)

func init() {
	common.ApiMap["file"] = func(r *gin.RouterGroup) {
		r.GET("/get/:category/:filename", apiGetFile)
		r.GET("/list/:category/:criteria/:value", apiGetFileList)
		r.GET("/listgroups/year", apiGetOutputYears)
		r.GET("/listgroups/month/:year", apiGetOutputMonths)
	}
}

func apiGetFile(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	category := c.Param("category")
	filename := c.Param("filename")

	tag := fmt.Sprintf("apiGetFile(%s,%s) [%s]: ", category, filename, user)

	if category == "" || filename == "" {
		log.Print(tag + "Missing category or filename")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	var o model.FileStoreModel
	err := model.DbMap.SelectOne(&o, "SELECT * FROM "+model.TABLE_FILE_STORE+" WHERE user = ? AND category = ? AND filename = ?", user, category, filename)
	if err != nil {
		log.Print(tag + err.Error())
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

func apiGetFileList(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	category := c.Param("category")
	criteria := c.Param("criteria")
	value := c.Param("value")

	tag := fmt.Sprintf("apiGetFileList(%s,%s,%s) [%s]: ", category, criteria, value, user)

	if category == "" || criteria == "" || value == "" {
		log.Print(tag + "Missing category or criteria or value")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	queryBase := "SELECT f.filename AS filename " +
		" , f.contentsize AS filesize" +
		" , p.originalId AS originalId " +
		" , p.insert_stamp AS inserted " +
		" FROM tFileStore f " +
		" LEFT OUTER JOIN tPayload p ON p.id = f.payloadId " +
		" WHERE f.user = ? " +
		" AND f.category = ? " +
		" AND "
	switch strings.ToLower(criteria) {
	case "month":
		queryBase += " DATE_FORMAT(f.stamp, '%Y-%m') = ? " + ";"
	case "year":
		queryBase += " DATE_FORMAT(f.stamp, '%Y') = ? " + ";"
	case "payload":
		queryBase += " f.payloadId = ? " + ";"
	default:
		c.AbortWithError(http.StatusBadRequest, fmt.Errorf("bad criteria %s", criteria))
		return
	}

	var items []model.FileListItem
	_, err := model.DbMap.Select(&items, queryBase, user, category, value)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, items)
}

func apiGetOutputMonths(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	year := c.Param("year")

	tag := fmt.Sprintf("apiGetOutputMonths(%s) [%s]: ", year, user)

	if year == "" {
		log.Print(tag + "Missing year")
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	query := "SELECT DATE_FORMAT(stamp, '%Y-%m') AS m " +
		" FROM tFileStore " +
		" WHERE user = ? AND YEAR(stamp) = ? " +
		" GROUP BY m "

	var items []string
	_, err := model.DbMap.Select(&items, query, user, year)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, items)
}

func apiGetOutputYears(c *gin.Context) {
	user := c.MustGet(gin.AuthUserKey).(string)

	tag := fmt.Sprintf("apiGetOutputYears() [%s]: ", user)

	query := "SELECT " +
		"  DISTINCT(YEAR(stamp)) AS year " +
		", COUNT(YEAR(stamp)) AS c " +
		" FROM tFileStore " +
		" WHERE user = ? " +
		" GROUP BY YEAR(stamp) "

	var items []struct {
		Year  string `json:"year" db:"year"`
		Count int64  `json:"count" db:"c"`
	}
	_, err := model.DbMap.Select(&items, query, user)
	if err != nil {
		log.Print(tag + err.Error())
		c.AbortWithError(http.StatusInternalServerError, err)
		return
	}

	c.JSON(http.StatusOK, items)
}
