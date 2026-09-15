# TODO

This "TODO" list covers migration from the 0.5.x J2EE backend for implementation.

Status note (2026-09-15): this file previously used `[X]` for anything that had
*code*, including two eligibility plugins that returned fake success without
contacting anything, and an XSLT engine that cannot reproduce the stylesheets it
is wired to run. Checkmarks below now mean **verified working**. The
authoritative, evidence-backed status is
`.hermes/plans/2026-09-15_101754-remitt-unfinished-work.md`; the REMAINING
section at the bottom is the honest list of what is still broken.

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
- [X] insertPayload
- [X] listRemittUsers
- [X] parseData (envelope + 835 + 997 + 271 parsers)
- [x] resubmitPayload
- [x] setConfigValue
- [X] validatePayload

All 20 endpoints are also reachable through the SOAP 1.1 compatibility layer
(`soap/`, `dispatchTable`), covered by `soap/soap_test.go`.

## BACKEND

- [X] Access control roles
- [X] Callback support (post-pipeline SOAP callback in `callback/`)
- [X] Eligibility plugins
  - [X] Dummy
  - [ ] Gateway EDI — the response/status contract now matches the Java plugin, but
        the request envelope is not a `WSEligibilityInquiry` and no HTTP Basic auth
        is sent, so it still cannot talk to the real endpoint. See REMAINING.
  - [X] NC Medicaid
  - [X] SFTP
  - [X] Optum, Stedi, BCBS FHIR (via 1upHealth)
  - [ ] Medicare HETS — returns a canned X12 271 success with no HTTP call. See REMAINING.
- [X] Job queuing mechanism
- [X] Migrate queue polling logic to go channel logic [already channel-based]
- [X] Parsing X12 [envelope + 835 + 997 + 271 + DTOs]
- [X] PGP/GPG armoring for payloads (`crypto/`, now covered by tests)
- [X] Render plugins
  - [X] PreRenderedPlugin
  - [X] XsltPlugin (wired, but only correct when it shells out to `xsltproc` — see REMAINING)
- [X] Scooper plugins
  - [X] Gateway EDI
  - [X] SFTP
- [X] Task scheduler
  - [X] Eligibility task
  - [X] Scooper task
- [X] Translation plugins
  - [X] Import PDF overlay logic from [go fpdf port](https://github.com/jung-kurt/gofpdf)
  - [X] FixedFormPdf
  - [X] FixedFormXml
  - [X] X12Passthrough
  - [X] X12Xml
- [X] Transport plugins
  - [X] Javascript scripting with [otto](https://github.com/robertkrimen/otto) for scripting
  - [X] SFTP support with [sftp](https://github.com/pkg/sftp)
  - [X] Web-scraping / automation with [goquery](https://github.com/PuerkitoBio/goquery)
  - [X] ClaimLogic
  - [X] Gateway EDI
  - [X] StoreFile
  - [X] StoreFilePdf
- [X] Validation plugins
  - [X] X12 validation (otto JS engine)
- [ ] XSLT processing — see REMAINING

## REMAINING (verified broken or missing as of 2026-09-15)

- [ ] **In-process XSLT engine is not equivalent to `xsltproc`.** `xsltproc` is
      still REQUIRED in production. `common/xsl_micro_test.go` pins 14 constructs:
      7 pass, 7 fail. The failures are node-set-valued variables (in `for-each`,
      `with-param`, global scope and inside predicates) and `set:distinct()`, which
      returns an empty node set for every argument. Against the real stylesheets,
      `TestXslTransform_Compare` fails 4/4: 4010_837p 6,425 vs 15,287 B, 5010_837p
      6,396 vs 15,607 B, cms1500 34 vs 9,698 B, statement 34 vs 2,048 B.
- [ ] **GatewayEDI eligibility request envelope** is not the vendor's request: it
      PGP-encrypts a made-up `<eligibilityRequest>` body and POSTs it to a
      configured URI with no Authorization header, while the real API
      (`https://services.gatewayedi.com/eligibility/service.asmx?WSDL`) is plain
      SOAP for `DoInquiry` taking a `WSEligibilityInquiry` (MyNameValue parameters
      + `ResponseDataType=Xml`) with HTTP Basic auth and no PGP. Decision pending.
- [ ] **Medicare HETS eligibility** returns a canned X12 271 success without any
      HTTP call (it needs CMS credentials). The real `EligibilityStatus` values now
      exist, so it can report the failure honestly instead. Decision pending.
- [ ] **CI does not test the sub-modules.** `.github/workflows/go.yml` runs
      `go test -v ./...` from the root, which in workspace mode covers only
      root-module packages, so `api`, `client`, `common`, `config`, `jobqueue`,
      `model`, `model/user`, `render`, `translation` and `transport` are never
      tested. Deliberately deferred until the XSLT engine work lands, so CI is not
      knowingly red in the meantime.
- [ ] **Test coverage still missing** for `client`, `model`, `model/user`,
      `scooper`, and 6 of the 7 transport plugins.
