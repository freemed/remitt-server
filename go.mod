module github.com/freemed/remitt-server

go 1.16

replace (
	github.com/freemed/remitt-server => ./
	github.com/freemed/remitt-server/api => ./api
	github.com/freemed/remitt-server/client => ./client
	github.com/freemed/remitt-server/common => ./common
	github.com/freemed/remitt-server/config => ./config
	github.com/freemed/remitt-server/jobqueue => ./jobqueue
	github.com/freemed/remitt-server/model => ./model
	github.com/freemed/remitt-server/translation => ./translation
	github.com/freemed/remitt-server/transport => ./transport
)

require (
	github.com/braintree/manners v0.0.0-20160418043613-82a8879fc5fd
	github.com/freemed/remitt-server/api v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/common v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/config v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/model v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/translation v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/transport v0.0.0-00010101000000-000000000000
	github.com/gin-gonic/contrib v0.0.0-20191209060500-d6e26eeaa607
	github.com/gin-gonic/gin v1.6.3
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
)
