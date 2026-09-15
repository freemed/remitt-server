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

- [X] **In-process XSLT engine now matches `xsltproc` byte-for-byte.** All four
      shipped stylesheets transform to identical bytes (15,287 / 15,607 / 9,698 /
      2,048) and produce identical bytes on every run — verified with every engine
      module resolved from the module cache, five consecutive fresh-process runs,
      and confirmed green in CI. `common/xsl_micro_test.go` is 14/14 and
      `TestXslTransform_Compare` passes 4/4. `internal-xslt` now defaults to
      **true** (commit `7afeae9`), so the in-process engine is the production
      default; `xsltproc` is retained in the image as the fallback that
      `common.XslTransform` uses if the in-process engine errors, and a
      deployment can still select it with `internal-xslt: false`. Dropping
      libxslt from the image is the remaining deployment decision.
- [ ] **`/metrics` requires credentials.** In echo v5 `e.Use(...)` builds one global
      chain, so registering `e.GET("/metrics", ...)` before the BasicAuth group does
      **not** exempt it — measured: unauthenticated `GET /metrics` returns 401 in all
      three registration orders. If scrapers are expected to reach it anonymously, this
      needs a `BasicAuthConfig` Skipper; that is an auth decision, so it is left to the
      owner. (Prometheus is now registered outermost, so rejected scrapes at least show
      up as `status="401"` instead of being invisible.)
- [ ] **Validator: spec-script selection deferred.** The port hardcodes
      `004010X098A1.js` (837P) and applies it to 835/271 payloads too, where the Java
      derives the script from GS08 (`X12Validator.java:56,139-145`). Needs a decision
      on which scripts ship and whether an unknown GS08 is rejected or falls back.
- [ ] **GatewayEDI eligibility request envelope** is still not the vendor's request:
      it PGP-encrypts a made-up `<eligibilityRequest>` body and POSTs it to a
      configured URI with no Authorization header, while the real API
      (`https://services.gatewayedi.com/eligibility/service.asmx?WSDL`) is plain SOAP
      for `DoInquiry` taking a `WSEligibilityInquiry` (MyNameValue parameters +
      `ResponseDataType=Xml`) with HTTP Basic auth and no PGP. Decision pending.
- [ ] **Medicare HETS eligibility** returns a canned X12 271 success without any HTTP
      call (it needs CMS credentials). Decision pending.
- [X] **CI does not test the sub-modules** — fixed.
      `.github/workflows/go.yml` now runs each of the ten sub-modules explicitly
      after the root-module tests. It was only safe to do once `common` went
      green; the run on `8b4558b` passed (actions/runs/35001572893).
- [X] **Test coverage for `client`, `model`, `model/user`, `scooper` and `transport`** —
      added 2026-09-15 (30 tests in scooper, 62 in transport, 19 in client, 60 in
      `model`, 4 in `model/user`). The whole workspace is green in CI. The DB-bound
      `model` functions remain untested by design (`model.InitDb` and everything it
      guards needs a live MySQL server); they are listed in
      `model/models_test.go` under `TestDatabaseBoundAPIRequiresDatabase`.

## FOUND BY THAT COVERAGE (tests pin these; production code NOT yet changed)

Ordered by how much damage each does in production. Every one is a real code
path, not a style preference — the tests exercise it and assert current behaviour.

1. **No SFTP transport can ever deliver a file.** `transport/sftp.go:37-41`,
   `transport/gatewayedi.go:66-70`, `transport/claimlogic.go:56-60` build an
   `ssh.ClientConfig` with User/Auth/Timeout but **no `HostKeyCallback`**, so
   `ssh.Dial` always returns `ssh: must specify HostKeyCallback`. Three of the
   seven transports are therefore dead on arrival, and the ZIP container built by
   gatewayedi/claimlogic is never uploaded.
2. **Every job seeded from the legacy DB fails to resolve its transport.**
   `transport/map.go` keys the registry by short names (`sftp`, `claimlogic`, …)
   but `migrations/001_legacy.up.sql` and the UI store Java FQCNs
   (`org.remitt.plugin.transport.SftpTransport`), and `jobqueue.go:307` passes the
   DB value straight into `InstantiateTransporter`. `ScriptedHttpTransport` is
   advertised by the seed with no such plugin registered.
3. **GatewayEDI scooper stores remittances UNDECRYPTED.** `scooper/gatewayedi.go`
   embeds `SftpScooper` by value, so `SftpScooper.Scoop` calls its own
   `PostProcess` (Go has no virtual dispatch for embedded structs) and the
   decrypting override is never reached — Java relies on exactly that override.
   Separately its decryption is gated on `crypto.IsPGPEncrypted`, which matches
   only ASCII armor while this codebase's own `EncryptPGP` emits binary.
4. **`model.NullString` silently voids non-NULL data.** `NewNullStringValue`
   never sets `Valid`, so `nullStringFromSQL` reports every non-NULL column as
   unset: plugin `inputformat`/`outputformat` and a user's `contactEmail`,
   callback username and password all marshal as `null`. `UnmarshalJSON` is also
   on a VALUE receiver (a silent no-op), and `MarshalJSON` uses `QuoteToASCII`,
   which emits invalid JSON for control characters and makes `json.Marshal` fail
   outright for strings MySQL stores happily.
5. **Registry data races (fatal, not recoverable).** `transport/map.go:21-27` and
   `scooper/map.go:22` read the registry map without the lock that `Register*`
   takes; a concurrent run aborts the process with "concurrent map read and map
   write". `transport/registry_race_probe_test.go` is build-tag gated because the
   runtime fatal error cannot be caught by a test.
6. **`client.PayloadInsert` is rejected by this server.** It sends no
   `Content-Type`, and echo's binder answers 415 — the api tests never caught it
   because they set the header themselves. `client.Ping` also passes the base URL
   as a printf FORMAT string (any `%` in the URL breaks it), no method checks
   `resp.StatusCode` (a 500 body is decoded as a success) and none closes
   `resp.Body`.
7. **`script_http.go` can hang or crash a job.** No client timeout (a hung payer
   blocks the worker forever), the HTTP status is never inspected (a 404 body is
   returned as a success), a connection failure is indistinguishable from an empty
   body, and a malformed URL panics through `Script.RunUnsafe` into the job worker.
8. **`model.NullInt.UnmarshalJSON` ignores the document's own `Valid` field**
   (`Valid = err == nil`), so `{"Int64":0,"Valid":false}` decodes as a valid zero;
   `model.NullTime.Scan` never fails (a string date silently clears the field) and
   `UnmarshalJSON` errors on `null` while accepting short junk; `model/user/user.go`
   reports a typed-nil user as found.
9. **`scooper` port parsing is lossy and unvalidated** (`fmt.Sscanf` error
   discarded: `22xyz`→22, `abc`→0, `-1` passes the guard), and `model.SqlDb` is
   dereferenced with no nil guard in `scooper/sftp.go:42`,
   `scooper/gatewayedi.go:33`, `transport/storefile.go:48` and
   `transport/storefilepdf.go:48` — an uninitialised database panics the worker
   instead of returning an error. The Java `GatewayEdiSftpScooper` also hardcoded
   its vendor endpoint; the Go port has no defaults, so a registry-built
   GatewayEDI scooper is always unconfigured.

## FIXED on 2026-09-15 (this file previously claimed these worked)

- [X] **X12 validator reported every payload as valid.** `x12validator.go` hardcoded
      `Status: "success"` while the JS script's verdict sat unused inside a JSON blob
      in `Messages`, so empty input, binary garbage and truncated envelopes were all
      reported valid to REST and SOAP callers. Now the status is rolled up from the
      script verdict with the Java `ValidationStatus` vocabulary and severity
      precedence, and messages are the real messages.
- [X] **Wrong-delimiter payloads accepted.** A payload declaring `*` and using `|` was
      reported `OK`; the structural check now runs before the script, where the Java
      does it.
- [X] **GatewayEDI eligibility returned success without contacting anyone** (it was a
      stub). It now performs the real POST, maps all 8 `SuccessCode` values per the
      Java plugin, and fails closed.
- [X] **`eligibility` was missing 4 of 5 status values and 6 of 8 success codes**,
      so no plugin could report what real payers return.
- [X] **The task scheduler could not parse the schedules its own database stores.**
      `jobSchedule` holds cron4j patterns and the Go scheduler only understood Go
      durations, so both seeded jobs were rejected and never ran on any install. Cron
      patterns are now parsed (with the next fire time computed from the pattern) and
      migration 002 corrects the seeded `EligibiltyTask` class typo in data. Also
      fixed: `Stop()` panicking on a second call, a stop request dropped while the
      runner was busy (busy tasks ran forever), a non-positive interval panicking
      inside a goroutine (killed the process), an unsynchronised `s.ticker`,
      stopped tasks being resurrected by `refreshJobs`, and nil-DB panics.
- [X] **Prometheus recorded the wrong status and missed whole classes of traffic.**
      Handler-returned errors and all 404/405s were recorded as `status="200"`, a
      gzip-wrapped writer (every browser) forced 200, panics were recorded nowhere,
      and BasicAuth 401s were not counted at all. Prometheus now registers outermost,
      reads status through `echo.UnwrapResponse`, and records post-chain statuses via
      `resp.Before` plus a deferred recover; metric names and labels are unchanged.

