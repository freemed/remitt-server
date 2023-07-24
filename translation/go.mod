module github.com/freemed/remitt-server/translation

go 1.20

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/api => ../api
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/model => ../model
)

require (
	github.com/freemed/remitt-server/model v0.0.0-20230221125127-71aa32d472a1
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e
	github.com/phpdave11/gofpdf v1.4.2
)

require (
	github.com/bytedance/sonic v1.10.0-rc2 // indirect
	github.com/chenzhuoyu/base64x v0.0.0-20230717121745-296ad89f973d // indirect
	github.com/chenzhuoyu/iasm v0.9.0 // indirect
	github.com/freemed/gokogiri/help v0.0.0-20230628164547-0f93de0487ac // indirect
	github.com/freemed/gokogiri/util v0.0.0-20230628164547-0f93de0487ac // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20230628164547-0f93de0487ac // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20230628164547-0f93de0487ac // indirect
	github.com/freemed/ratago/xslt v0.0.0-20230628164800-7a1df60588d2 // indirect
	github.com/freemed/remitt-server/common v0.0.0-20230221125127-71aa32d472a1 // indirect
	github.com/freemed/remitt-server/config v0.0.0-20230221125127-71aa32d472a1 // indirect
	github.com/gabriel-vasile/mimetype v1.4.2 // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/gin-gonic/gin v1.9.1 // indirect
	github.com/go-gorp/gorp v2.2.0+incompatible // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.14.1 // indirect
	github.com/go-sql-driver/mysql v1.7.1 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/leodido/go-urn v1.2.4 // indirect
	github.com/mattes/migrate v3.0.1+incompatible // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.0.9 // indirect
	github.com/phpdave11/gofpdi v1.0.13 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.11 // indirect
	golang.org/x/arch v0.4.0 // indirect
	golang.org/x/crypto v0.11.0 // indirect
	golang.org/x/net v0.12.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	golang.org/x/text v0.11.0 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
