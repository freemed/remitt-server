# TODO

This "TODO" list covers migration from the 0.5.x J2EE backend for implementation.

## API

- [ ] addKeyToKeyring
- [ ] addRemittUser
- [ ] batchEligibilityCheck
- [X] changePassword
- [X] getBulkStatus
- [x] getConfigValues
- [x] getCurrentUsername
- [ ] getEligibility
- [x] getFile
- [x] getFileList
- [X] getOutputMonths
- [X] getOutputYears
- [x] getPlugins
- [ ] getPluginOptions
- [X] getProtocolVersion
- [x] getStatus
- [x] insertPayload
- [ ] listRemittUsers
- [ ] parseData
- [x] resubmitPayload
- [x] setConfigValue
- [ ] validatePayload

## BACKEND

- [ ] Access control roles
- [ ] Callback support
  - [ ] getProtocolVersion
  - [ ] sendRemittancePayload
- [ ] Eligibility plugins
  - [ ] Dummy
  - [ ] Gateway EDI
  - [ ] NC Medicaid
  - [ ] SFTP
- [X] Job queuing mechanism
- [ ] Migrate queue polling logic to go channel logic
- [ ] Parsing X12
- [ ] PGP/GPG armoring for payloads
- [ ] Render plugins
  - [ ] PreRenderedPlugin
  - [ ] XsltPlugin
- [ ] Scooper plugins
  - [ ] Gateway EDI
  - [ ] SFTP
- [ ] Task scheduler
  - [ ] Eligibility task
  - [ ] Scooper task
- [ ] Translation plugins
  - [X] Import PDF overlay logic from [go fpdf port](https://github.com/jung-kurt/gofpdf)
  - [X] FixedFormPdf
  - [ ] FixedFormXml
  - [ ] X12Passthrough
  - [X] X12Xml
- [ ] Transport plugins
  - [X] Javascript scripting with [otto](https://github.com/robertkrimen/otto) for scripting
  - [X] SFTP support with [sftp](https://github.com/pkg/sftp)
  - [ ] Web-scraping / automation with [goquery](https://github.com/PuerkitoBio/goquery)
  - [ ] ClaimLogic
  - [ ] Gateway EDI
  - [ ] StoreFile
  - [ ] StoreFilePdf
- [ ] Validation plugins
  - [ ] X12 validation
- [X] XSLT processing

