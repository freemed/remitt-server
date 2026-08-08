package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
	"github.com/labstack/echo/v5"
)

func init() {
	common.ApiMap["file"] = func(g *echo.Group) {
		g.GET("/get/:category/:filename", a.GetFile)
		g.GET("/list/:category/:criteria/:value", a.GetFileList)
		g.GET("/listgroups/year", a.GetOutputYears)
		g.GET("/listgroups/month/:year", a.GetOutputMonths)
	}
}

func (a Api) GetFile(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	category := c.Param("category")
	filename := c.Param("filename")

	tag := fmt.Sprintf("api.GetFile(%s,%s) [%s]: ", category, filename, user)

	if category == "" || filename == "" {
		log.Print(tag + "Missing category or filename")
		return echo.NewHTTPError(http.StatusBadRequest, http.StatusText(http.StatusBadRequest))
	}

	params := dbgen.GetFileParams{User: user, Category: category, Filename: filename}
	f, err := model.Queries.GetFile(context.Background(), params)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
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

	return c.Blob(http.StatusOK, contentType, []byte(f.Content.String))
}

func (a Api) GetFileList(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	category := c.Param("category")
	criteria := c.Param("criteria")
	value := c.Param("value")

	tag := fmt.Sprintf("api.GetFileList(%s,%s,%s) [%s]: ", category, criteria, value, user)

	if category == "" || criteria == "" || value == "" {
		log.Print(tag + "Missing category or criteria or value")
		return echo.NewHTTPError(http.StatusBadRequest, http.StatusText(http.StatusBadRequest))
	}

	var items []model.FileListItem

	switch strings.ToLower(criteria) {
	case "month":
		t, err := time.Parse("2006-01", value)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid month format, expected YYYY-MM")
		}
		params := dbgen.GetFileListByMonthParams{User: user, Category: category, Month: t}
		rows, err := model.Queries.GetFileListByMonth(context.Background(), params)
		if err != nil {
			log.Print(tag + err.Error())
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		items = make([]model.FileListItem, len(rows))
		for i, r := range rows {
			items[i] = model.FileListItem{
				FileName:   r.Filename,
				FileSize:   int64(r.Filesize),
				Inserted:   r.Inserted.Time,
				OriginalID: r.Originalid.String,
			}
		}
	case "year":
		t, err := time.Parse("2006", value)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid year format, expected YYYY")
		}
		params := dbgen.GetFileListByYearParams{User: user, Category: category, Year: t}
		rows, err := model.Queries.GetFileListByYear(context.Background(), params)
		if err != nil {
			log.Print(tag + err.Error())
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		items = make([]model.FileListItem, len(rows))
		for i, r := range rows {
			items[i] = model.FileListItem{
				FileName:   r.Filename,
				FileSize:   int64(r.Filesize),
				Inserted:   r.Inserted.Time,
				OriginalID: r.Originalid.String,
			}
		}
	case "payload":
		pid, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid payload id")
		}
		params := dbgen.GetFileListByPayloadParams{User: user, Category: category, PayloadID: pid}
		rows, err := model.Queries.GetFileListByPayload(context.Background(), params)
		if err != nil {
			log.Print(tag + err.Error())
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		items = make([]model.FileListItem, len(rows))
		for i, r := range rows {
			items[i] = model.FileListItem{
				FileName:   r.Filename,
				FileSize:   int64(r.Filesize),
				Inserted:   r.Inserted.Time,
				OriginalID: r.Originalid.String,
			}
		}
	default:
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("bad criteria %s", criteria))
	}

	return c.JSON(http.StatusOK, items)
}

func (a Api) GetOutputMonths(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	year := c.Param("year")

	tag := fmt.Sprintf("api.GetOutputMonths(%s) [%s]: ", year, user)

	if year == "" {
		log.Print(tag + "Missing year")
		return echo.NewHTTPError(http.StatusBadRequest, http.StatusText(http.StatusBadRequest))
	}

	t, err := time.Parse("2006", year)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid year format, expected YYYY")
	}

	params := dbgen.GetOutputMonthsParams{User: user, Year: t}
	items, err := model.Queries.GetOutputMonths(context.Background(), params)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, items)
}

func (a Api) GetOutputYears(c *echo.Context) error {
	user := c.Get(common.AuthUserKey).(string)

	tag := fmt.Sprintf("api.GetOutputYears() [%s]: ", user)

	rows, err := model.Queries.GetOutputYears(context.Background(), user)
	if err != nil {
		log.Print(tag + err.Error())
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	type outputYear struct {
		Year  int32 `json:"year"`
		Count int64 `json:"count"`
	}
	items := make([]outputYear, len(rows))
	for i, r := range rows {
		items[i] = outputYear{Year: r.Year, Count: r.C}
	}

	return c.JSON(http.StatusOK, items)
}
