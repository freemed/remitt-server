# TODO

This "TODO" list covers migration from the 0.5.x J2EE backend for implementation.

## API

- [X] addKeyToKeyring
- [X] addRemittUser
- [X] batchEligibilityCheck
- [X] changePassword
- [X] getBulkStatus
- [x] getConfigValues
- [x] getCurrentUsername
- [X] getEligibility
- [x] getFile
- [x] getFileList
- [X] getOutputMonths
- [X] getOutputYears
- [x] getPlugins
- [X] getPluginOptions
- [X] getProtocolVersion
- [x] getStatus
- [x] insertPayload
- [X] listRemittUsers
- [X] parseData [interface + API + X12 envelope parser done]
- [x] resubmitPayload
- [x] setConfigValue
- [X] validatePayload

## BACKEND

- [X] Access control roles
- [ ] Callback support
  - [ ] getProtocolVersion
  - [ ] sendRemittancePayload
- [X] Eligibility plugins
  - [X] Dummy
  - [ ] Gateway EDI
  - [ ] NC Medicaid
  - [ ] SFTP
- [X] Job queuing mechanism
- [X] Migrate queue polling logic to go channel logic [already channel-based]
- [X] Parsing X12 [envelope + 835 parser + 14 DTOs done]
- [X] PGP/GPG armoring for payloads
- [X] Render plugins
  - [X] PreRenderedPlugin
  - [X] XsltPlugin
- [X] Scooper plugins
  - [X] Gateway EDI
  - [X] SFTP
- [X] Task scheduler
  - [X] Eligibility task
  - [X] Scooper task
- [ ] Translation plugins
  - [X] Import PDF overlay logic from [go fpdf port](https://github.com/jung-kurt/gofpdf)
  - [X] FixedFormPdf
  - [X] FixedFormXml
  - [X] X12Passthrough
  - [X] X12Xml
- [ ] Transport plugins
  - [X] Javascript scripting with [otto](https://github.com/robertkrimen/otto) for scripting
  - [X] SFTP support with [sftp](https://github.com/pkg/sftp)
  - [X] Web-scraping / automation with [goquery](https://github.com/PuerkitoBio/goquery)
  - [X] ClaimLogic
  - [X] Gateway EDI
  - [X] StoreFile
  - [X] StoreFilePdf
- [ ] Validation plugins
  - [X] X12 validation
- [X] XSLT processing

