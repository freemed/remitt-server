module github.com/freemed/remitt-server/transport

go 1.16

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/model => ../model
)

require (
	github.com/pkg/sftp v1.13.1
	golang.org/x/crypto v0.0.0-20210616213533-5ff15b29337e
	golang.org/x/sys v0.0.0-20210616094352-59db8d763f22 // indirect
)
