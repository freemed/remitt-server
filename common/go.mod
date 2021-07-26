module github.com/freemed/remitt-server/common

go 1.16

replace (
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/freemed/remitt-server/config v0.0.0-00010101000000-000000000000
	github.com/gin-gonic/gin v1.7.2
	github.com/go-playground/validator/v10 v10.8.0 // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/json-iterator/go v1.1.11 // indirect
	github.com/mattn/go-isatty v0.0.13 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/ugorji/go v1.2.6 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df
)
