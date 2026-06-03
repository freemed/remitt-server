module github.com/freemed/remitt-server

go 1.25.0

replace (
	//github.com/freemed/gokogiri/help => ../gokogiri/help
	//github.com/freemed/gokogiri/util => ../gokogiri/util
	//github.com/freemed/gokogiri/xml => ../gokogiri/xml
	//github.com/freemed/gokogiri/xpath => ../gokogiri/xpath
	//github.com/freemed/ratago => ../ratago
	//github.com/freemed/ratago/xslt => ../ratago/xslt
	github.com/freemed/remitt-server => ./
	github.com/freemed/remitt-server/api => ./api
	github.com/freemed/remitt-server/client => ./client
	github.com/freemed/remitt-server/common => ./common
	github.com/freemed/remitt-server/config => ./config
	github.com/freemed/remitt-server/jobqueue => ./jobqueue
	github.com/freemed/remitt-server/model => ./model
	github.com/freemed/remitt-server/model/user => ./model/user
	github.com/freemed/remitt-server/translation => ./translation
	github.com/freemed/remitt-server/transport => ./transport
)

require (
	github.com/freemed/remitt-server/api v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/common v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/config v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/jobqueue v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/model v0.0.0-20260409181504-5105c68ef4de
	github.com/labstack/echo/v5 v5.1.1
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/PuerkitoBio/goquery v1.12.0 // indirect
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/freemed/gokogiri/help v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/util v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/ratago/xslt v0.0.0-20260127145558-2a510afd68fb // indirect
	github.com/freemed/remitt-server/model/user v0.0.0-20260409181504-5105c68ef4de // indirect
	github.com/freemed/remitt-server/translation v0.0.0-20260409181504-5105c68ef4de // indirect
	github.com/freemed/remitt-server/transport v0.0.0-20260409181504-5105c68ef4de // indirect
	github.com/go-gorp/gorp v2.2.0+incompatible // indirect
	github.com/go-sql-driver/mysql v1.10.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mattes/migrate v3.0.1+incompatible // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e // indirect
	github.com/phpdave11/gofpdf v1.4.3 // indirect
	github.com/phpdave11/gofpdi v1.0.16 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.10 // indirect
	github.com/robertkrimen/otto v0.5.1 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
