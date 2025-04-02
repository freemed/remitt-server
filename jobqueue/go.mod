module github.com/freemed/remitt-server/jobqueue

go 1.24

toolchain go1.24.0

replace (
	github.com/freemed/remitt-server => ../
	github.com/freemed/remitt-server/client => ../client
	github.com/freemed/remitt-server/common => ../common
	github.com/freemed/remitt-server/config => ../config
	github.com/freemed/remitt-server/model => ../model
	github.com/freemed/remitt-server/model/user => ../model/user
	github.com/freemed/remitt-server/translation => ../translation
	github.com/freemed/remitt-server/transport => ../transport
)

require (
	github.com/freemed/remitt-server/common v0.0.0-20250331145336-3d6a47d26eae
	github.com/freemed/remitt-server/config v0.0.0-20250331145336-3d6a47d26eae
	github.com/freemed/remitt-server/model v0.0.0-20250331145336-3d6a47d26eae
	github.com/freemed/remitt-server/model/user v0.0.0-20250331145336-3d6a47d26eae
	github.com/freemed/remitt-server/translation v0.0.0-20250331145336-3d6a47d26eae
	github.com/freemed/remitt-server/transport v0.0.0-20250331145336-3d6a47d26eae
)

require (
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/bytedance/sonic v1.13.2 // indirect
	github.com/bytedance/sonic/loader v0.2.4 // indirect
	github.com/cloudwego/base64x v0.1.5 // indirect
	github.com/freemed/gokogiri/help v0.0.0-20250203225759-a4d8eb383f22 // indirect
	github.com/freemed/gokogiri/util v0.0.0-20250203225759-a4d8eb383f22 // indirect
	github.com/freemed/gokogiri/xml v0.0.0-20250203225759-a4d8eb383f22 // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20250203225759-a4d8eb383f22 // indirect
	github.com/freemed/ratago/xslt v0.0.0-20250203231425-016f1ea48158 // indirect
	github.com/gabriel-vasile/mimetype v1.4.8 // indirect
	github.com/gin-contrib/sse v1.0.0 // indirect
	github.com/gin-gonic/gin v1.10.0 // indirect
	github.com/go-gorp/gorp v2.2.0+incompatible // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/go-playground/validator/v10 v10.26.0 // indirect
	github.com/go-sql-driver/mysql v1.9.1 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/leodido/go-urn v1.4.0 // indirect
	github.com/mattes/migrate v3.0.1+incompatible // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/orcaman/writerseeker v0.0.0-20200621085525-1d3f536ff85e // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/phpdave11/gofpdf v1.4.2 // indirect
	github.com/phpdave11/gofpdi v1.0.14 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pkg/sftp v1.13.9 // indirect
	github.com/robertkrimen/otto v0.5.1 // indirect
	github.com/rogpeppe/go-internal v1.12.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/ugorji/go/codec v1.2.12 // indirect
	golang.org/x/arch v0.15.0 // indirect
	golang.org/x/crypto v0.36.0 // indirect
	golang.org/x/net v0.38.0 // indirect
	golang.org/x/sys v0.31.0 // indirect
	golang.org/x/text v0.23.0 // indirect
	google.golang.org/protobuf v1.36.6 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df // indirect
	gopkg.in/sourcemap.v1 v1.0.5 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
