module github.com/freemed/remitt-server/common

go 1.18

replace (
	github.com/freemed/gokogiri => ../../gokogiri
	github.com/freemed/gokogiri/help => ../../gokogiri/help
	github.com/freemed/gokogiri/html => ../../gokogiri/html
	github.com/freemed/gokogiri/xml => ../../gokogiri/xml
	github.com/freemed/gokogiri/xpath => ../../gokogiri/xpath
	github.com/freemed/ratago => ../../ratago
	github.com/freemed/ratago/xslt => ../../ratago/xslt
	github.com/freemed/remitt-server/config => ../config
)

require (
	github.com/freemed/gokogiri/xml v0.0.0-20220627154600-2acb041aa5ac
	github.com/freemed/ratago/xslt v0.0.0-20220610164841-0ab820da5118
	github.com/freemed/remitt-server/config v0.0.0-20220610164855-e7050fc6dac2
	github.com/gin-gonic/gin v1.8.1
	gopkg.in/gomail.v2 v2.0.0-20160411212932-81ebce5c23df
)

require (
	github.com/freemed/gokogiri/help v0.0.0-20220627154600-2acb041aa5ac // indirect
	github.com/freemed/gokogiri/util v0.0.0-20220627154600-2acb041aa5ac // indirect
	github.com/freemed/gokogiri/xpath v0.0.0-20220627154600-2acb041aa5ac // indirect
	github.com/gin-contrib/sse v0.1.0 // indirect
	github.com/go-playground/locales v0.14.0 // indirect
	github.com/go-playground/universal-translator v0.18.0 // indirect
	github.com/go-playground/validator/v10 v10.11.0 // indirect
	github.com/goccy/go-json v0.9.7 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/leodido/go-urn v1.2.1 // indirect
	github.com/mattn/go-isatty v0.0.14 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.0.2 // indirect
	github.com/ugorji/go/codec v1.2.7 // indirect
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d // indirect
	golang.org/x/net v0.0.0-20220624214902-1bab6f366d9e // indirect
	golang.org/x/sys v0.0.0-20220624220833-87e55d714810 // indirect
	golang.org/x/text v0.3.7 // indirect
	google.golang.org/protobuf v1.28.0 // indirect
	gopkg.in/alexcesaro/quotedprintable.v3 v3.0.0-20150716171945-2caba252f4dc // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
)
