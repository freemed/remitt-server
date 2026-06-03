module github.com/freemed/remitt-server/common

go 1.25.0

replace (
	//github.com/freemed/gokogiri => ../../gokogiri
	//github.com/freemed/gokogiri/help => ../../gokogiri/help
	//github.com/freemed/gokogiri/html => ../../gokogiri/html
	//github.com/freemed/gokogiri/xml => ../../gokogiri/xml
	//github.com/freemed/gokogiri/xpath => ../../gokogiri/xpath
	//github.com/freemed/ratago/xslt => ../../ratago/xslt
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/config => ../config
	github.com/ugorji/go => github.com/ugorji/go/codec v1.1.7
)

require (
	github.com/freemed/gokogiri/xml v0.0.0-20260127145523-0d7d36b651ea
	github.com/freemed/ratago/xslt v0.0.0-20260127145558-2a510afd68fb
	github.com/freemed/remitt-server/config v0.0.0-20260409181504-5105c68ef4de
	github.com/labstack/echo/v5 v5.1.1
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df
)

require (
	github.com/freemed/gokogiri/help v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/util v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/kr/text v0.2.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
