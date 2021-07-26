module github.com/freemed/remitt-server/transport

go 1.16

replace (
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/model => ../model
	github.com/freemed/remitt-server/model/user => ../model/user
)

require (
	github.com/freemed/remitt-server/common v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/model v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/model/user v0.0.0-00010101000000-000000000000
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.2
	github.com/robertkrimen/otto v0.0.0-20210614181706-373ff5438452
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
)
