module github.com/freemed/remitt-server/transport

go 1.16

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/model => ../model
)

require (
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.2
	golang.org/x/crypto v0.0.0-20210711020723-a769d52b0f97
	golang.org/x/sys v0.0.0-20210630005230-0f9fa26af87c // indirect
)
