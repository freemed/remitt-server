module github.com/freemed/remitt-server

go 1.24.0

replace (
	//github.com/freemed/gokogiri/help => ../gokogiri/help
	//github.com/freemed/gokogiri/util => ../gokogiri/util
	//github.com/freemed/gokogiri/xml => ../gokogiri/xml
	//github.com/freemed/gokogiri/xpath => ../gokogiri/xpath
	//github.com/freemed/ratago => ../ratago
	//github.com/freemed/ratago/xslt => ../ratago/xslt
	github.com/freemed/remitt-server => ./
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
	github.com/freemed/remitt-server/api v0.0.0-20251105151329-d7601a90921e
	github.com/freemed/remitt-server/common v0.0.0-20251105151329-d7601a90921e
	github.com/freemed/remitt-server/config v0.0.0-20251105151329-d7601a90921e
	github.com/freemed/remitt-server/jobqueue v0.0.0-20251105151329-d7601a90921e
	github.com/freemed/remitt-server/model v0.0.0-20251105151329-d7601a90921e
	github.com/gin-gonic/contrib v0.0.0-20250521004450-2b1292699c15
	github.com/gin-gonic/gin v1.11.0
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/PuerkitoBio/goquery v1.11.0 // indirect
	github.com/andybalholm/cascadia v1.3.3 // indirect
	github.com/bytedance/gopkg v0.1.3 // indirect
	github.com/bytedance/sonic v1.14.2 // indirect
	github.com/bytedance/sonic/loader v0.4.0 // indirect
	github.com/chenzhuoyu/base64x v0.0.0-20230717121745-296ad89f973d // indirect
	github.com/chenzhuoyu/iasm v0.9.1 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/cloudwego/iasm v0.2.0 // indirect
	github.com/freemed/gokogiri/help v0.0.0-20250831182455-de8ad4878374 // indirect
	github.com/freemed/gokogiri/util v0.0.0-20250831182455-de8ad4878374 // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20250831182455-de8ad4878374 // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20250831182455-de8ad4878374 // indirect
	github.com/freemed/ratago/xslt v0.0.0-20251105151549-80fda038dff4 // indirect
	github.com/freemed/remitt-server/model/user v0.0.0-20251105151329-d7601a90921e // indirect
	github.com/freemed/remitt-server/translation v0.0.0-20251105151329-d7601a90921e // indirect
	github.com/freemed/remitt-server/transport v0.0.0-20251105151329-d7601a90921e // indirect
	github.com/gabriel-vasile/mimetype v1.4.11 // indirect
	github.com/gin-contrib/sse v1.1.0 // indirect
	github.com/go-gorp/gorp v2.2.0+incompatible // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.28.0 // indirect
	github.com/go-sql-driver/mysql v1.9.3 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/goccy/go-yaml v1.19.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattes/migrate v3.0.1+incompatible // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-sqlite3 v2.0.3+incompatible // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/phpdave11/gofpdf v1.4.3 // indirect
	github.com/phpdave11/gofpdi v1.0.15 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.10 // indirect
	github.com/quic-go/qpack v0.6.0 // indirect
	github.com/quic-go/quic-go v0.57.1 // indirect
	github.com/robertkrimen/otto v0.5.1 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.3.1 // indirect
	go.uber.org/mock v0.6.0 // indirect
	golang.org/x/arch v0.23.0 // indirect
	golang.org/x/crypto v0.45.0 // indirect
	golang.org/x/mod v0.30.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/tools v0.39.0 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
