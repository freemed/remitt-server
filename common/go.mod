module github.com/freemed/remitt-server/common

go 1.22

replace (
	github.com/freemed/gokogiri => ../../gokogiri
	github.com/freemed/gokogiri/help => ../../gokogiri/help
	github.com/freemed/gokogiri/html => ../../gokogiri/html
	github.com/freemed/gokogiri/xml => ../../gokogiri/xml
	github.com/freemed/gokogiri/xpath => ../../gokogiri/xpath
	github.com/freemed/ratago/xslt => ../../ratago/xslt
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/config => ../config
	github.com/ugorji/go => github.com/ugorji/go/codec v1.1.7
)

require (
	github.com/freemed/gokogiri/xml v0.0.0-20250203225759-a4d8eb383f22
	github.com/freemed/ratago/xslt v0.0.0-20250203231425-016f1ea48158
	github.com/freemed/remitt-server/config v0.0.0-20250203225640-8b7510789299
	github.com/gin-gonic/gin v1.10.0
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df
)

require (
	github.com/bytedance/sonic v1.12.8 // indirect
	github.com/bytedance/sonic/loader v0.2.3 // indirect
	github.com/cloudwego/base64x v0.1.5 // indirect
	github.com/freemed/gokogiri/help v0.0.0-20250203225759-a4d8eb383f22 // indirect
	github.com/freemed/gokogiri/util v0.0.0-20250203225759-a4d8eb383f22 // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20250203225759-a4d8eb383f22 // indirect
	github.com/gabriel-vasile/mimetype v1.4.8 // indirect
	github.com/gin-contrib/sse v1.0.0 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.24.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	golang.org/x/arch v0.14.0 // indirect
	golang.org/x/crypto v0.33.0 // indirect
	golang.org/x/net v0.35.0 // indirect
	golang.org/x/sys v0.30.0 // indirect
	golang.org/x/text v0.22.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
