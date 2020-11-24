module github.com/freemed/remitt-server/api

go 1.15

replace (
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/model => ../model
)

require github.com/gin-gonic/gin v1.6.3
