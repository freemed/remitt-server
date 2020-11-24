module github.com/freemed/remitt-server/model

go 1.15

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
)

require (
	github.com/freemed/remitt-server v0.0.0-20200620233920-b7fb5d5908dc
	github.com/freemed/remitt-server/common v0.0.0-00010101000000-000000000000
	github.com/go-gorp/gorp v2.2.0+incompatible
	github.com/go-sql-driver/mysql v1.5.0
	github.com/mattes/migrate v3.0.1+incompatible
)
