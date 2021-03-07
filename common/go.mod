module github.com/freemed/remitt-server/common

go 1.16

replace (
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/freemed/remitt-server/config v0.0.0-00010101000000-000000000000
	github.com/gin-gonic/gin v1.6.3
)
