module github.com/freemed/remitt-server/model

go 1.16

replace (
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/Microsoft/go-winio v0.4.15 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v1.13.1 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/freemed/remitt-server/common v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/config v0.0.0-00010101000000-000000000000
	github.com/go-gorp/gorp v2.2.0+incompatible
	github.com/go-playground/validator/v10 v10.7.0 // indirect
	github.com/go-sql-driver/mysql v1.6.0
	github.com/lib/pq v1.8.0 // indirect
	github.com/mattes/migrate v3.0.1+incompatible
	github.com/mattn/go-sqlite3 v1.14.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/poy/onpar v1.0.1 // indirect
	github.com/ziutek/mymysql v1.5.4 // indirect
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
	google.golang.org/protobuf v1.27.1 // indirect
)
