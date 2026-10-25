module github.com/freemed/remitt-server/render

go 1.26.0

toolchain go1.26.10

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/freemed/remitt-server/common v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/config v0.0.0-20260409181504-5105c68ef4de
)

require (
	github.com/freemed/gokogiri/help v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/util v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/ratago/xslt v0.0.0-20260127145558-2a510afd68fb // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/labstack/echo/v5 v5.4.2 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
