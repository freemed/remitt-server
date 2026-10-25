module github.com/freemed/remitt-server/api

go 1.26.0

toolchain go1.26.10

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/jobqueue => ../jobqueue
	github.com/freemed/remitt-server/model => ../model
)

require (
	github.com/freemed/remitt-server v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/common v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/jobqueue v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/model v0.0.0-20260811020147-71c029df58dc
	github.com/labstack/echo/v5 v5.4.2
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/PuerkitoBio/goquery v1.13.0 // indirect
	github.com/andybalholm/cascadia v1.3.5 // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20260915172826-7894ad67f15c // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20260915172826-7894ad67f15c // indirect
	github.com/freemed/ratago/xslt v0.0.0-20260915205955-ad59da3cc45f // indirect
	github.com/freemed/remitt-server/config v0.0.0-20260811020147-71c029df58dc // indirect
	github.com/freemed/remitt-server/model/user v0.0.0-20260811020147-71c029df58dc // indirect
	github.com/freemed/remitt-server/render v0.0.0-20260811020147-71c029df58dc // indirect
	github.com/freemed/remitt-server/translation v0.0.0-20260915205449-392f6ab3ea60 // indirect
	github.com/freemed/remitt-server/transport v0.0.0-20260915205449-392f6ab3ea60 // indirect
	github.com/freemed/xpath v1.3.12 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/golang-migrate/migrate/v4 v4.20.1 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e // indirect
	github.com/phpdave11/gofpdf v1.4.3 // indirect
	github.com/phpdave11/gofpdi v1.0.16 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.12 // indirect
	github.com/robertkrimen/otto v0.5.1 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
