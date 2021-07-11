module github.com/freemed/remitt-server/translation

go 1.16

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/api => ../api
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/model => ../model
)

require (
	github.com/freemed/remitt-server/model v0.0.0-00010101000000-000000000000
	github.com/go-playground/validator/v10 v10.7.0 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e
	github.com/phpdave11/gofpdf v1.4.2
	github.com/phpdave11/gofpdi v1.0.13 // indirect
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97 // indirect
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
	google.golang.org/protobuf v1.27.1 // indirect
)
