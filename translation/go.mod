module github.com/freemed/remitt-server/translation

go 1.15

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/api => ../api
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/model => ../model
)

require (
	github.com/Microsoft/go-winio v0.4.15 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v1.13.1 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/freemed/remitt-server/model v0.0.0-00010101000000-000000000000
	github.com/lib/pq v1.8.0 // indirect
	github.com/mattn/go-sqlite3 v1.14.5 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e
	github.com/phpdave11/gofpdf v1.4.2
	github.com/phpdave11/gofpdi v1.0.13 // indirect
	github.com/poy/onpar v1.0.1 // indirect
	github.com/ziutek/mymysql v1.5.4 // indirect
)
