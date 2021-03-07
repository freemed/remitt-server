module github.com/freemed/remitt-server/transport

go 1.16

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/model => ../model
)

require (
	github.com/pkg/sftp v1.12.0
	golang.org/x/crypto v0.0.0-20201117144127-c1f2f97bffc9
)
