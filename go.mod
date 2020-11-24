module github.com/freemed/remitt-server

go 1.15

replace (
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
	github.com/Microsoft/go-winio v0.4.14 // indirect
	github.com/boombuler/barcode v1.0.0 // indirect
	github.com/braintree/manners v0.0.0-20160418043613-82a8879fc5fd
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/docker/docker v1.13.1 // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/freemed/remitt-server/api v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/common v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/model v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/translation v0.0.0-00010101000000-000000000000
	github.com/freemed/remitt-server/transport v0.0.0-00010101000000-000000000000
	github.com/gin-gonic/contrib v0.0.0-20191209060500-d6e26eeaa607
	github.com/gin-gonic/gin v1.6.3
	github.com/go-gorp/gorp v2.2.0+incompatible
	github.com/go-sql-driver/mysql v1.5.0
	github.com/lib/pq v1.5.2 // indirect
	github.com/mattes/migrate v3.0.1+incompatible
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.12.0
	github.com/poy/onpar v1.0.0 // indirect
	github.com/unidoc/unidoc v2.2.0+incompatible
	github.com/ziutek/mymysql v1.5.4 // indirect
	golang.org/x/crypto v0.0.0-20201117144127-c1f2f97bffc9
	golang.org/x/image v0.0.0-20190910094157-69e4b8554b2a // indirect
	google.golang.org/appengine v1.6.2 // indirect
	gopkg.in/yaml.v2 v2.2.8
)
