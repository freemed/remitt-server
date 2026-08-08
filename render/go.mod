module github.com/freemed/remitt-server/render

go 1.25.0

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/freemed/remitt-server/common v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/config v0.0.0-20260409181504-5105c68ef4de
)
