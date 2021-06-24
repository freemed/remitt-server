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
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e
	github.com/phpdave11/gofpdf v1.4.2
	github.com/phpdave11/gofpdi v1.0.13 // indirect
)
