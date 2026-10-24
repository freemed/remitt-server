module github.com/freemed/remitt-server

go 1.26.0

replace (
	github.com/freemed/remitt-server => ./
	github.com/freemed/remitt-server/api => ./api
	github.com/freemed/remitt-server/common => ./common
	github.com/freemed/remitt-server/config => ./config
	github.com/freemed/remitt-server/jobqueue => ./jobqueue
	github.com/freemed/remitt-server/model => ./model
	github.com/freemed/remitt-server/render => ./render
)

require (
	github.com/freemed/remitt-server/api v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/common v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/config v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/jobqueue v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/model v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/model/user v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/render v0.0.0-20260811020147-71c029df58dc
	github.com/freemed/remitt-server/translation v0.0.0-20260915205449-392f6ab3ea60
	github.com/freemed/remitt-server/transport v0.0.0-20260915205449-392f6ab3ea60
	github.com/labstack/echo/v5 v5.4.2
	github.com/pkg/sftp v1.13.12
	github.com/prometheus/client_golang v1.25.0
	github.com/robertkrimen/otto v0.5.1
	golang.org/x/crypto v0.58.0
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/PuerkitoBio/goquery v1.13.0 // indirect
	github.com/andybalholm/cascadia v1.3.5 // indirect
	github.com/antchfx/xpath v1.3.8 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20260915172826-7894ad67f15c // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20260915172826-7894ad67f15c // indirect
	github.com/freemed/ratago/xslt v0.0.0-20260915205955-ad59da3cc45f // indirect
	github.com/freemed/xpath v1.3.12 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/mattes/migrate v3.0.1+incompatible // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e // indirect
	github.com/phpdave11/gofpdf v1.4.3 // indirect
	github.com/phpdave11/gofpdi v1.0.16 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.72.1 // indirect
	github.com/prometheus/procfs v0.23.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
