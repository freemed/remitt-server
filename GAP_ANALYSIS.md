# REMITT Migration Gap Analysis & Implementation Plan

Generated: 2026-08-08

## Summary

The Go remitt-server is ~45% complete relative to the Java 0.5.x REMITT. Below
is a feature-by-feature gap analysis with the old Java source as ground truth,
followed by a phased implementation plan ordered by dependency chain.

---

## GAP ANALYSIS: API LAYER

### Already Implemented (14 of 20 endpoints)
| Endpoint | Go Handler | Status |
|---|---|---|
| changePassword | api/user.go:ChangePassword | DONE |
| getBulkStatus | api/status.go:GetBulkStatus | DONE |
| getConfigValues | api/config.go:ConfigGetAll | DONE |
| getCurrentUsername | api/user.go:GetUsername | DONE |
| getFile | api/file.go:GetFile | DONE |
| getFileList | api/file.go:GetFileList | DONE |
| getOutputMonths | api/file.go:GetOutputMonths | DONE |
| getOutputYears | api/file.go:GetOutputYears | DONE |
| getPlugins | api/plugins.go:PluginsGetAll | DONE |
| getPluginOptions | api/plugins.go:PluginGetOptions | DONE |
| getProtocolVersion | api/version.go:ProtocolVersion | DONE |
| getStatus | api/status.go:GetStatus | DONE |
| insertPayload | api/payload.go:PayloadInsert | DONE |
| resubmitPayload | api/payload.go:PayloadResubmit | DONE |
| setConfigValue | api/config.go:ConfigSetValue | DONE |

### Missing API Endpoints (6 of 20)

#### 1. addKeyToKeyring (P0 - Medium effort)
**Old Java:** ServiceImpl.java:602-610
**What it does:** Takes keyname, privatekey (byte[]), publickey (byte[]), stores
them in tKeyring table for the current user.
**Go state:** model/keyring.go exists with KeyringModel but no DB CRUD.
**Needs:**
- `model/keyring.go`: Add `AddKeyToKeyring(user, name, privKey, pubKey []byte) error`
- `api/keyring.go`: New handler file with `POST /add` route
- Admin role check (Java checks admin role, but looking at Service.java there's no
  admin check for addKey — it's per-user keys)

#### 2. addRemittUser (P0 - Medium effort)
**Old Java:** ServiceImpl.java:612-623
**What it does:** Admin-only endpoint to create a new user with full
UserDTO (username, password, callbackServiceUri, callbackServiceWsdlUri,
callbackUsername, callbackPassword).
**Go state:** model/user.go has UserModel, GetUserByName, CheckUserPassword.
No creation function.
**Needs:**
- `model/user.go`: Add `AddUser(u UserModel) error` with password hashing
- `api/user.go`: Add `POST /add` route with admin ACL check
- UserModel already has all the DTO fields

#### 3. batchEligibilityCheck (P1 - High effort, depends on eligibility plugins)
**Old Java:** ServiceImpl.java:561-572
**What it does:** Takes EligibilityRequest[], inserts into tEligibilityJobs for
each one, returns count.
**Go state:** model/eligibilityjobs.go exists with table mapping, but no CRUD.
No eligibility plugins exist at all.
**Needs:**
- Eligibility plugin interface + registry (new `eligibility/` package)
- `model/eligibilityjobs.go`: Add batch insert function
- `api/eligibility.go`: New handler

#### 4. getEligibility (P1 - High effort, depends on eligibility plugins)
**Old Java:** ServiceImpl.java:531-559
**What it does:** Instantiates an eligibility plugin by class name, calls
checkEligibility() synchronously, returns EligibilityResponse.
**Go state:** Nothing exists.
**Needs:**
- Eligibility plugin interface and registry
- `api/eligibility.go`: Handler for synchronous eligibility check

#### 5. parseData (P2 - Medium effort, depends on parser interface)
**Old Java:** ServiceImpl.java:574-600
**What it does:** Instantiates a ParserInterface plugin by class name, calls
parseData(data), returns parsed string.
**Go state:** No parser interface or implementations exist.
**Needs:**
- Parser interface definition (new `parser/` package, or extend translation package)
- `api/parser.go`: New handler
- Old Java had X12Message835, X12Message997, X12Message271 parsers in
  org.remitt.parser package with x12dto subpackage (22 DTO classes)

#### 6. validatePayload (P0 - High effort, depends on validation plugins)
**Old Java:** ServiceImpl.java:638-665
**What it does:** Takes validatorClass name and raw byte[] data, instantiates
ValidationInterface, calls validate(), returns ValidationResponse.
**Go state:** No validation infrastructure exists. Old Java had:
- X12Validator.java (uses Rhino JS engine to run Common.js and spec-specific
  scripts like 004010X098A1.js)
- Validation JS scripts in WEB-INF/scripts/org.remitt.plugin.validation.X12Validator/
**Needs:**
- Validation plugin interface + registry (new `validation/` package)
- `api/validation.go`: New handler
- Port X12 JS validation scripts (Common.js + 004010X098A1.js) to Otto

---

## GAP ANALYSIS: BACKEND PLUGIN SYSTEM

### Translation Plugins: 4 of 4 DONE
| Plugin | Go File | Status |
|---|---|---|
| FixedFormPdf | translation/fixedformpdf.go | DONE |
| FixedFormXml | translation/fixedformxml.go | DONE |
| X12Passthrough | translation/x12passthrough.go | DONE |
| X12Xml | translation/x12xml.go | DONE |

### Transport Plugins: 2 of 6 DONE
| Plugin | Go File | Status |
|---|---|---|
| Script (otto JS) | transport/script.go | DONE |
| SFTP | transport/sftp.go | DONE |
| ScriptedHttpTransport | transport/script_http.go | DONE (see script_http.go) |
| **StoreFile** | — | MISSING |
| **StoreFilePdf** | — | MISSING |
| **ClaimLogic** | — | MISSING |
| **GatewayEdiTransport** | — | MISSING |

#### StoreFile (P1 - Low effort)
**Old Java:** StoreFile.java - stores input bytes to DbFileStore with
category "output". Detects content type from magic bytes (%PDF, <?xml, ISA*).
Essentially does what the jobqueue's executeJob() does without the XSLT
render step — it's a simple file store transport.

#### StoreFilePdf (P1 - Low effort)
**Old Java:** StoreFilePdf.java - same as StoreFile but always sets extension
to .pdf. Input format is "pdf".

#### ClaimLogic (P1 - Medium effort)
**Old Java:** ClaimLogicTransport.java extends SftpTransport. Adds ZIP
compression wrapper (prepareInput zips the content) before SFTP upload,
using config keys for host/port/path.

#### GatewayEdiTransport (P1 - Medium effort)
**Old Java:** GatewayEdiTransport.java extends SftpTransport. Same as
ClaimLogic — ZIP wrapper + SFTP, different config keys.

### Scooper Plugins: 0 of 2 DONE
| Plugin | Go File | Status |
|---|---|---|
| **SftpScooper** | — | MISSING |
| **GatewayEdiSftpScooper** | — | MISSING |

#### SftpScooper (P1 - High effort)
**Old Java:** SftpScooper.java implements ScooperInterface. Connects to SFTP
server, lists files, downloads new ones (tracking previously scooped by
filename), stores to tScooper table. Base class with overridable
postprocess(), getHost(), getPort(), getPath().

#### GatewayEdiSftpScooper (P1 - Medium effort)
**Old Java:** GatewayEdiSftpScooper.java extends SftpScooper. Overrides
postprocess() to PGP-decrypt downloaded files using a key from tKeyring.
Hardcoded host: sftp.gatewayedi.com, path: remits.

### Eligibility Plugins: 0 of 4 DONE
| Plugin | Go File | Status |
|---|---|---|
| **DummyEligibility** | — | MISSING |
| **GatewayEDIEligibility** | — | MISSING |
| **NCMedicaidEligibility** | — | MISSING |
| **SftpEligibility** | — | MISSING |

All eligibility plugins implement EligibilityInterface.checkEligibility().
Gateway EDI hits a SOAP web service. NC Medicaid and SFTP are variants.

### Render Plugins: 0 of 2 DONE
| Plugin | Go File | Status |
|---|---|---|
| **PreRenderedPlugin** | — | MISSING |
| **XsltPlugin** | — | MISSING |

Note: The Go jobqueue already has XSLT rendering built into executeJob()
(lines 287-333), so XsltPlugin is partially covered. PreRenderedPlugin in
Java was a passthrough for pre-rendered content.

### Validation Plugins: 0 of 1 DONE
| Plugin | Go File | Status |
|---|---|---|
| **X12Validator** | — | MISSING |

---

## GAP ANALYSIS: INFRASTRUCTURE

### Task Scheduler (P0 - Critical)
**Old Java:** MasterControl.java + ControlThread.java (772 lines). The
ControlThread is the central orchestrator: it polls tPayload for new jobs,
assigns them to processor threads (validation -> render -> translation ->
transport pipeline), and manages task scheduling (ScooperTask,
EligibilityTask).

**Go state:** jobqueue/jobqueue.go has a worker pool with
StartDispatcher/Worker, and executeJob() does render -> translate ->
transport. But there is no scheduled task system for scoopers or eligibility
batch processing.

**Needed:**
- Task scheduler that runs ScooperTask and EligibilityTask on cron-like
  schedules (from tJobs table)
- Scooper polling loop
- Eligibility batch processing loop

### PGP/GPG Armoring (P1 - Medium effort)
**Old Java:** PGPProvider.java for encrypt/decrypt. Used by
GatewayEdiTransport (PGP encrypt with keyring) and GatewayEdiSftpScooper
(PGP decrypt).
**Go state:** Nothing exists.
**Needed:** `crypto/pgp.go` using Go's openpgp or a compatible library.

### Parsing X12 (P2 - High effort)
**Old Java:** org.remitt.parser package with X12Message835, X12Message997,
X12Message271 and 22 DTO classes in x12dto subpackage. This is a full EDI
X12 parser.
**Go state:** model/x12xml.go defines the XML-to-X12 serialization format
(used by X12Xml translator to generate X12 output), but no X12 *parser*
(to ingest raw X12 into structured data).
**Needed:** `parser/` package with X12 reading/parsing logic.

### Callback Support (P2 - Medium effort)
**Old Java:** RemittCallback SOAP client (client/RemittCallback/) for
calling back to originating systems.
**Go state:** UserModel has callback fields (CallbackServiceUri, etc.) but
no callback sending logic.
**Needed:** `callback/` package to send results back to originating systems.

### Channel-Based Queue (P2 - Low effort)
The TODO notes "Migrate queue polling logic to go channel logic." The
current jobqueue already uses Go channels (jobQueueChannel, WorkerQueue),
so this is partially done. The remaining work is replacing any remaining
polling loops with channel-based patterns.

---

## PRIORITIZED IMPLEMENTATION PLAN

### Phase 1: Foundation (~3-5 days estimated)
API completeness + core infrastructure needed by everything else.

1. **addRemittUser API** — new `api/user.go` POST /add route + `model/user.go` AddUser()
2. **addKeyToKeyring API** — new `api/keyring.go` + `model/keyring.go` CRUD
3. **validatePayload API** — scaffold validation plugin interface +
   port X12 JS validator scripts (Common.js, 004010X098A1.js)
4. **parseData API** — scaffold parser interface (stub; parsing X12 is Phase 4)

### Phase 2: Transport Completeness (~2-3 days)
Finishing the transport plugin set.

5. **StoreFile transport** — stores payload to DbFileStore (category "output")
6. **StoreFilePdf transport** — same but PDF-specific
7. **ClaimLogic transport** — SFTP + ZIP wrapper
8. **GatewayEdiTransport** — SFTP + ZIP wrapper (different config keys)

### Phase 3: Eligibility (~4-6 days)
The entire eligibility subsystem.

9. **Eligibility plugin interface + registry** — new `eligibility/` package
10. **DummyEligibility plugin** — random pass/fail for testing
11. **getEligibility API** — synchronous eligibility check
12. **batchEligibilityCheck API** — batch insert to tEligibilityJobs
13. **Task scheduler foundation** — read tJobs table, run on cron-like schedule
14. **EligibilityTask** — process tEligibilityJobs queue

### Phase 4: Scooper & Parser (~3-5 days)
15. **Scooper interface + registry** — new `scooper/` package
16. **SftpScooper plugin** — SFTP file polling/scraping
17. **GatewayEdiSftpScooper plugin** — SFTP + PGP decrypt
18. **PGP/GPG armoring** — new `crypto/pgp.go` for encrypt/decrypt
19. **ScooperTask** — scheduled scooper runs
20. **Parsing X12** — port parser/x12dto DTOs + X12Message* parsers

### Phase 5: Remaining (~3-4 days)
21. **Callback support** — send results to originating systems
22. **Channel-based queue refinement** — remove any remaining polling
23. **Gateway EDI eligibility** — SOAP-based eligibility plugin
24. **NC Medicaid eligibility** — eligibility plugin variant
25. **SftpEligibility** — SFTP-based eligibility plugin

---

## DEPENDENCY GRAPH

```
Phase 1 (Foundation)
  ├── addRemittUser ───────────────────────── independent
  ├── addKeyToKeyring ──────────────────────── independent
  ├── validatePayload ──────────────────────── depends on: validation interface
  └── parseData ────────────────────────────── depends on: parser interface (stub)
       │
Phase 2 (Transport) ────────────────────────── independent
  │
Phase 3 (Eligibility)
  ├── Eligibility interface ────────────────── independent
  ├── DummyEligibility ─────────────────────── depends on: eligibility interface
  ├── getEligibility API ───────────────────── depends on: eligibility interface
  ├── batchEligibilityCheck API ────────────── depends on: eligibility interface
  ├── Task scheduler ───────────────────────── independent (reads tJobs)
  └── EligibilityTask ──────────────────────── depends on: task scheduler + eligibility plugins
       │
Phase 4 (Scooper + Parser)
  ├── Scooper interface ────────────────────── independent
  ├── SftpScooper ──────────────────────────── depends on: scooper interface
  ├── GatewayEdiSftpScooper ────────────────── depends on: SftpScooper + PGP
  ├── PGP/GPG ──────────────────────────────── independent
  ├── ScooperTask ──────────────────────────── depends on: task scheduler + scooper plugins
  └── X12 Parser ───────────────────────────── independent
       │
Phase 5 (Remaining) ────────────────────────── depends on: Phase 1-4 completion
```

Parallelizable work: Phase 1 items (all independent), Phase 2 items (all
independent), Eligibility interface + DummyEligibility (independent from
task scheduler).

---

## KEY ARCHITECTURAL NOTES

1. **DB schema is stable.** The Go code runs against the same MySQL schema as
   the 0.5.x Java version. Model structs map existing tables — do not change
   table schemas.

2. **Plugin registry pattern** is already established for translators
   (translation/map.go) and transporters (transport/map.go). Follow the same
   pattern for eligibility, scooper, parser, and validation plugins:
   var registry = map[string]func() Interface{} with RegisterX/InstantiateX.

3. **Context passing** is done via `context.Context` with user model stored
   using model/user.NewContext(). Follow this pattern in new plugins.

4. **Old Java has ~22 x12dto DTO classes** for full X12 835/997/271 parsing.
   This is the most complex single item. A pragmatic middle ground would be
   to port the X12-to-x12xml direction first (which already exists in Go)
   and implement the reverse (raw X12 parsing) incrementally.
