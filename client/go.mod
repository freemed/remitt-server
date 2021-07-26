module github.com/freemed/remitt-server/client

go 1.16

replace (
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/model => ../model
)

require github.com/freemed/remitt-server/model v0.0.0-00010101000000-000000000000
