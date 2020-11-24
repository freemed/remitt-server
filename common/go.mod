module github.com/freemed/remitt-server/common

go 1.15

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
)

require (
	github.com/freemed/remitt-server v0.0.0-20200620233920-b7fb5d5908dc
	github.com/gin-gonic/gin v1.6.3
)
