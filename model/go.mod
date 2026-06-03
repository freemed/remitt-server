module github.com/freemed/remitt-server/model

go 1.25.0

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/freemed/remitt-server/common v0.0.0-20260409181504-5105c68ef4de
	github.com/freemed/remitt-server/config v0.0.0-20260409181504-5105c68ef4de
	github.com/go-gorp/gorp v2.2.0+incompatible
	github.com/go-sql-driver/mysql v1.10.0
	github.com/mattes/migrate v3.0.1+incompatible
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/Microsoft/go-winio v0.4.15 // indirect
	github.com/docker/distribution v2.8.0+incompatible // indirect
	github.com/docker/docker v1.13.1 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/freemed/gokogiri/help v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/util v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20260127145523-0d7d36b651ea // indirect
	github.com/freemed/ratago/xslt v0.0.0-20260127145558-2a510afd68fb // indirect
	github.com/labstack/echo/v5 v5.1.1 // indirect
	github.com/lib/pq v1.8.0 // indirect
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/poy/onpar v1.0.1 // indirect
	github.com/ziutek/mymysql v1.5.4 // indirect
	golang.org/x/sys v0.0.0-20220722155257-8c9f86f7a55f // indirect
	golang.org/x/text v0.36.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
