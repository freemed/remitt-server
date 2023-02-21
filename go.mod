module github.com/freemed/remitt-server

go 1.18

replace (
	github.com/freemed/gokogiri/help => ../gokogiri/help
	github.com/freemed/gokogiri/util => ../gokogiri/util
	github.com/freemed/gokogiri/xml => ../gokogiri/xml
	github.com/freemed/gokogiri/xpath => ../gokogiri/xpath
	github.com/freemed/ratago/xslt => ../ratago/xslt
	github.com/freemed/remitt-server/api => ./api
	github.com/freemed/remitt-server/client => ./client
	github.com/freemed/remitt-server/common => ./common
	github.com/freemed/remitt-server/config => ./config
	github.com/freemed/remitt-server/jobqueue => ./jobqueue
	github.com/freemed/remitt-server/model => ./model
	github.com/freemed/remitt-server/model/user => ./model/user
	github.com/freemed/remitt-server/translation => ./translation
	github.com/freemed/remitt-server/transport => ./transport
)

require (
	github.com/braintree/manners v0.0.0-20160418043613-82a8879fc5fd
	github.com/freemed/remitt-server/api v0.0.0-20220610145658-0d058ed2108f
	github.com/freemed/remitt-server/common v0.0.0-20220610145658-0d058ed2108f
	github.com/freemed/remitt-server/config v0.0.0-20220610164855-e7050fc6dac2
	github.com/freemed/remitt-server/jobqueue v0.0.0-20220610145658-0d058ed2108f
	github.com/freemed/remitt-server/model v0.0.0-20220610145658-0d058ed2108f
	github.com/gin-gonic/contrib v0.0.0-20201101042839-6a891bf89f19
	github.com/gin-gonic/gin v1.8.1
)

require (
	github.com/freemed/gokogiri/help v0.0.0-20220627154600-2acb041aa5ac // indirect
	github.com/freemed/gokogiri/util v0.0.0-20220627154600-2acb041aa5ac // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20220627154600-2acb041aa5ac // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20220627154600-2acb041aa5ac // indirect
	github.com/freemed/ratago v0.0.0-20191105200024-660929a3e119 // indirect
	github.com/freemed/remitt-server/model/user v0.0.0-20220610145658-0d058ed2108f // indirect
	github.com/freemed/remitt-server/translation v0.0.0-20220610145658-0d058ed2108f // indirect
	github.com/freemed/remitt-server/transport v0.0.0-20220610145658-0d058ed2108f // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-gorp/gorp v2.2.0+incompatible // indirect
	github.com/go-playground/locales v0.14.0 // indirect
	github.com/go-playground/universal-translator v0.18.0 // indirect
	github.com/go-playground/validator/v10 v10.11.0 // indirect
	github.com/go-sql-driver/mysql v1.6.0 // indirect
	github.com/goccy/go-json v0.9.7 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/leodido/go-urn v1.2.1 // indirect
	github.com/mattes/migrate v3.0.1+incompatible // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e // indirect
	github.com/pelletier/go-toml/v2 v2.0.2 // indirect
	github.com/phpdave11/gofpdf v1.4.2 // indirect
	github.com/phpdave11/gofpdi v1.0.13 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.4 // indirect
	github.com/robertkrimen/otto v0.0.0-20211024170158-b87d35c0b86f // indirect
	github.com/ugorji/go/codec v1.2.7 // indirect
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d // indirect
	golang.org/x/net v0.0.0-20220624214902-1bab6f366d9e // indirect
	golang.org/x/sys v0.0.0-20220624220833-87e55d714810 // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
