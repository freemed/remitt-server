module github.com/freemed/remitt-server/common

go 1.16

replace (
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/freemed/remitt-server/config v0.0.0-00010101000000-000000000000
	github.com/gin-gonic/gin v1.7.2
	github.com/go-playground/validator/v10 v10.6.1 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/leodido/go-urn v1.2.1 // indirect
	github.com/mattn/go-isatty v0.0.13 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/ugorji/go v1.2.6 // indirect
	golang.org/x/crypto v0.0.0-20210616213533-5ff15b29337e // indirect
	golang.org/x/sys v0.0.0-20210616094352-59db8d763f22 // indirect
	golang.org/x/text v0.3.6 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
