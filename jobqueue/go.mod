module github.com/freemed/remitt-server/jobqueue

go 1.16

replace (
	github.com/freemed/remitt-server/client => ../client
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/model => ../model
	github.com/freemed/remitt-server/translation => ../translation
	github.com/freemed/remitt-server/transport => ../transport
)

require (
	github.com/freemed/remitt-server/common v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/config v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/model v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/translation v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/transport v0.0.0-00010101000000-000000000000
)
