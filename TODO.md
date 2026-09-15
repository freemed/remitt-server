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

## FOUND BY THAT COVERAGE — 8 of 9 FIXED, 1 AWAITING AN OWNER DECISION

Every one was a real code path, not a style preference. The suites pinned them as
current behaviour; the fix commits flipped those assertions to the corrected
contract, and each flipped test was RED-verified against the reverted code first.

**STILL OPEN — needs an owner decision, deliberately not made for you:**

1. **No SFTP transport can deliver a file.** `transport/sftp.go:37-41`,
   `transport/gatewayedi.go:66-70`, `transport/claimlogic.go:56-60` build an
   `ssh.ClientConfig` with User/Auth/Timeout but **no `HostKeyCallback`**, so
   `ssh.Dial` always returns `ssh: must specify HostKeyCallback`. Three of the
   seven transports are dead on arrival and the ZIP container built by
   gatewayedi/claimlogic is never uploaded. VERIFIED with a real SSH+SFTP server
   stood up in-process and the production transports driven against it, with
   differential controls (raw `ssh.Dial` with `InsecureIgnoreHostKey` succeeds on
   the same endpoint while the transports' dial fails). Fixing it requires a
   host-key VERIFICATION POLICY — known_hosts path vs a fingerprint pinning table
   vs an explicit insecure opt-in — which is the owner's call, not the
   implementer's. Recommendation on the table: known_hosts from a configurable
   path, with an explicit opt-in insecure bypass for first contact.

**FIXED 2026-09-15** (commits `acc4f65`, `96a6585`, `9394598`, `235e35a`, `e4406e7`):

2. Every job seeded from the legacy DB failed to resolve its transport: the
   registries were keyed by short names while `migrations/001_legacy.up.sql` and
   the UI store Java FQCNs. The six names the seed actually contains
   (`SftpTransport`, `ScriptedHttpTransport`, `ClaimLogicTransport`,
   `GatewayEdiTransport`, `StoreFile`, `StoreFilePdf` — note the last two are not
   suffixed `Transport`) are now registered alongside the short names.
3. GatewayEDI remittances were stored UNDECRYPTED: the value-embedded
   `SftpScooper` never dispatched the decrypting `PostProcess` override, and
   decryption was gated on `IsPGPEncrypted`, which matches ASCII armor only while
   this codebase's own `EncryptPGP` emits binary. Both fixed; the override is now
   exercised end-to-end through an SFTP session seam.
4. `model.NullString` voided non-NULL data (constructor never set `Valid`, so
   `nullStringFromSQL` reported every non-NULL column as unset; `UnmarshalJSON`
   was on a value receiver; `MarshalJSON` emitted invalid JSON for control
   characters). All three fixed; the SQL round trip is asserted.
5/9. Registry data races in `transport/map.go` and `scooper/map.go` — the lookup
   read the map without the lock the registrar takes, aborting the process.
   Locked (and released before the factory call). The scooper port parse error is
   no longer discarded, and the unguarded `model.SqlDb` dereferences now return
   errors instead of panicking the worker.
6. `client.PayloadInsert` sent no `Content-Type` and this server rejects that with
   415; `Ping` used the base URL as a printf format string; no method checked
   `resp.StatusCode` (a 500 body decoded as a success) or closed `resp.Body`; the
   JSON marshal error was discarded; a struct-literal client panicked. All fixed.
7. `script_http.go` had no client timeout (a hung payer blocked the worker
   forever), never inspected the status (a 404 body was a success), could not
   distinguish a connection failure from an empty body, and panicked on a
   malformed URL. All fixed.
8. `NullInt64.UnmarshalJSON` ignored the document's own `Valid` field; `NullTime`
   `Scan` never failed and its `UnmarshalJSON` errored on the JSON literal `null`
   while accepting short junk; `FromContext` reported a typed-nil user as found.
   All fixed.

### ALSO FOUND, NOT YET FIXED: nothing ever enqueues a job, so the pipeline cannot be triggered

- `jobQueueChannel` (`jobqueue/jobqueue.go:42`) is created and only ever READ
  (`jobqueue.go:266`); `grep -rn jobQueueChannel --include=*.go .` returns exactly
  those two lines. There is no enqueue function, no database poller, and `api/`
  does not import `jobqueue` at all — so `api.PayloadInsert` stores a payload row,
  returns its id, and **nothing ever processes it**. `executeJob`
  (`jobqueue.go:289`), the whole render → translate → transport chain, is
  unreachable in production however well its parts work.
- The Java original fed the queue from the database: `ControlThread` polls
  `tProcessor` for queued work (see `ControlThread.java:527-532`, which reasons
  about `processorId` being already defined or `-1` for a wait state) and
  dispatches it. The Go port has the workers, the dispatcher and the pipeline but
  none of the feeder.
- Consequence for the "do the transports work?" question: even with the registry
  names, host-key policy and option wiring all correct, no job is ever handed to a
  worker. Driving `executeJob` directly (done for the live-database run) is the
  only way to exercise the chain today.
- Two things the feeder must get right when it is written, both found on
  2026-09-15: `JobQueueItem.Fail` used to **self-deadlock** (it held the write lock
  and called `AppendLog`, which takes the same non-reentrant mutex — fixed, pinned
  by `TestFailDoesNotDeadlockOnTheJobLock`), and the per-item `lock` is a pointer
  that **nothing initialises**, so a freshly built item panics on the first
  `AppendLog`/`Fail`/`Finish`/`Cancel` (pinned by
  `TestJobQueueItemLockMustBeInitialised`). The feeder must set
  `lock: new(sync.RWMutex)`.
- Render and translation do NOT have the same options gap, and Java parity says
  they should not: the `Renderer` and `Translator` interfaces declare no
  `SetOptions` at all (`render/interface.go:6-12`, `translation/interface.go:6-16`),
  and the only Java `getPluginOption` callers are the transport and eligibility
  plugins — no render or translation plugin ever read per-user plugin config.
  Render's per-plugin rows in the database are named *choices*
  (`tPluginOptions`: `4010_837p` → `org.remitt.plugin.render.XsltPlugin`), surfaced
  through `api/plugins.go:53`, not user configuration.

### VERIFIED END-TO-END 2026-09-15: what the live run proved, and the four blockers

Run against a real MySQL (8.0.46, provisioned for this) plus a real in-process
SSH+SFTP server from `test/harness/sshsftp`. Full plan and sequencing:
`docs/pipeline-wiring-plan.md`; raw logs in `.hermes/reports/e2e-live-run-*.log`.

**Proved working:** the self-configuring transports (a job took its SFTP
host/port/user/path purely from five `tUserConfig` rows, with a negative control -
setting `sftpPort` to 1 made it dial `127.0.0.1:1` and fail, restoring the row
delivered again); host-key verification end to end through `paths.known-hosts`; a
real `statement.xsl` render (1175 bytes, md5 `16f9a6848644cb340a522020b1c32683`)
**delivered** to the SFTP server with the bytes compared on both ends; and the
consuming half of the queue (a hand-pushed item was processed to SUCCESS by
worker[1] and the file landed). The transport FQCN namespaces are confirmed
against live data, not assumed.

**Four blockers, none of them regressions - wiring the Java had and the Go port
never grew:**

1. **Nothing feeds the queue** (see above) - blocks everything else. The Java
   polled `tPayload` for `payloadState='valid'` rows with no `tProcessor` row
   (`ControlThread.java:557-583`) and journaled each stage; the Go side needs a
   poll query, in-flight skipping, the item built with an initialised lock, an
   entry in `jobQueue` plus a copy on the channel, and stage journaling.
2. **Translation resolution ignores the database**: `executeJob` passes the render
   *option* (`4010_837p`) where the database resolves by the option's declared
   *output format* (`p_ResolveTranslationPlugin`, `001_legacy.up.sql:327-338`), so
   every shipped stylesheet fails with `unable to resolve translator between
   '4010_837p' and 'x12'`. Java marked the payload failed when none resolved.
3. **The translator gets `[]byte` where `x12xml` wants `model.X12Xml`**
   (`invalid datatype presented`). Java had one entry point taking bytes
   (`PluginInterface.java:41`) and each plugin parsed them itself.
4. **`tFileStore` writes violate BOTH foreign keys** — FIXED (`06357c9`). The
   storefiles hardcoded `PayloadID: 0`/`ProcessorID: 0` while `tFileStore` has two
   not-null FKs (`payloadId → tPayload(id)`, `processorId → tProcessor(id)`), so
   every write failed with 1452 — and the payload id alone is not enough. The
   identity now travels in the context (`common.JobIdentity{PayloadID, ProcessorID,
   JobID}` / `NewJobContext` / `JobIdentityFromContext`) and a missing one is a
   clear error rather than an invalid row. **Still to wire:** `executeJob` builds
   its context at `jobqueue.go:308` without it, so the storefiles fail loudly until
   the feeder attaches it.

Also fixed from the same run: the sqlc layer now matches the migrated schema
(`f646af5` — the invented `tUser.role` plus `tUserRoles`, and the `pUserConfigUpdate`
name missing its underscore, which made config saves 500 from both interfaces), the
translation registry answers to the FQCNs the database stores (`06357c9`), and the
eligibility registry's seeded `org.remitt.plugin.eligibility.DummyEligibility`
resolves (`0221199`).

**New blocker found by the FQCN audit, NOT yet fixed** — it is the second half of
blocker 2: `tTranslation.outputFormat` is the *consuming transport's* declared input
format (`p_ResolveTranslationPlugin` compares it with `transportPluginInputFormat`),
and the seed says `x12xml → 'text'` while every Go transport declares `x12`/`pdf`/`*`
(pinned in `transport/map_test.go`). So `Resolver("x12xml", "text")` is false even
though the registry now resolves the names: the formats the Go port declares and the
formats the database's functions compare have to be reconciled before a translator
can resolve.

Also verified, for the record: `tTranslation` and `tPluginOptions` store plugin
names as Java FQCNs while the translation registry answered only to short names, so
`InstantiateTranslator('org.remitt.plugin.translation.X12Xml')` failed — fixed, with
the alias behavior pinned by a test; and the two seeded `ScriptedHttpTransport` rows
in `tUserConfig` are dead data — they are not options that plugin declares
(`transport/script.go:156`).

### FIXED 2026-09-15: the database bootstrap could not migrate

Found by standing up a real MySQL and running the application's own `InitDb`.
Three defects, in `model/db.go`:

- The migrate **file-source driver was never imported**, so `MigrateDb` failed
  with `source driver: unknown driver file (forgotton import?)` on every startup.
  Migrations had never been able to run; on a fresh database the server started
  against a schema it never created.
- `InitDb` **discarded `MigrateDb`'s error**, so a migration that never ran looked
  identical to one that succeeded. It is fatal now unless the error is
  `migrate.ErrNoChange`.
- `MigrateDb` applied `m.Steps(2)`, which coincidentally covered the two
  migrations that exist and would silently skip every future one. It applies all
  pending migrations now.
- Also fixed: **`database.host` was ignored entirely** when the DSN was built
  (`user:pass@/db?flags`), pinning every deployment to the driver's default
  address even though the sample config carries `tcp(127.0.0.1:3306)` — the
  driver's `network(addr)` form that belongs exactly there. `dataSourceName()`
  honours it; an empty host keeps the old behaviour.
- **Deployment requirement this exposed:** on MySQL 8 the legacy schema's stored
  functions are refused while binary logging is on unless
  `log_bin_trust_function_creators=1` is set (error 1418). Verified end to end
  against MySQL 8.0.46: fresh database → `version=2 dirty=0`, 19 tables, seed rows.

### ALSO FOUND, NOT YET FIXED: the transport plugins are never given their options

- `jobqueue.executeJob` instantiates the transporter, calls `SetContext(ctx)` and
  `InputFormat()`, and **never calls `SetOptions`** — the only non-test `SetOptions`
  callers in the whole tree are the scratch probe under `.hermes/probe-tmp/`. So
  every transport runs unconfigured: `Sftp.Transport` fails its own validation
  ("sftpscooper: host/port not configured" before any dial), and gatewayedi,
  claimlogic, storefile, storefilepdf and the script transports are in the same
  position. **This is why the transports still cannot deliver even after the
  registry and host-key fixes.**
- The values exist in the database and are already keyed the way a plugin needs
  them: `tUserConfig` is `(user, namespace, option, value)` and the seed rows are
  `('Administrator', 'org.remitt.plugin.transport.SftpTransport', 'sftpHost', '')`
  — i.e. namespace = the plugin's Java FQCN, which the registry now accepts.
- The precedent for fixing it is already in the codebase, and the transports are
  the outlier: **every eligibility plugin self-configures** by calling
  `model.GetConfigValues(username)` inside the plugin (`eligibility/optum.go:170`,
  `stedi.go:175`, `bcbs_fhir.go:277`, `sftp.go:67`, `ncmedicaid.go:121`,
  `medicare_hets.go:188`, `gatewayedi.go:375`), while the transport interface
  exposes `SetOptions` and nothing calls it. Either wire the pipeline (load,
  filter by namespace, `SetOptions` before `Transport`) or make the transports
  self-configure like their eligibility counterparts.

### FIXED: client/server surface mismatches

- `client.PayloadResubmit` called `GET /api/payload/resubmit/:id` but the server
  registered no resubmit route at all. Registered (it mutates state, so GET is a
  compatibility choice, documented in the code).
- `client.Ping` called `GET /api/ping/:text` while only POST was registered.
  GET is now registered; POST is kept because it was already published.
- `api/route_contract_test.go` drives both through the real router, and its five
  assertions were proven non-vacuous by running them against an isolated copy
  carrying the original registrations (all five failed there).

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

