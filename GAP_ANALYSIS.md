# REMITT Migration Gap Analysis & Implementation Plan

Generated: 2026-08-08 | Updated: 2026-08-10

## Summary

The Go remitt-server is ~70% complete relative to the Java 0.5.x REMITT. Below
is a feature-by-feature gap analysis with the old Java source as ground truth,
followed by a phased implementation plan ordered by dependency chain.

---

## GAP ANALYSIS: API LAYER

### Already Implemented (16 of 20 endpoints)
| Endpoint | Go Handler | Status |
|---|---|---|
| changePassword | api/user.go:ChangePassword | DONE |
| getBulkStatus | api/status.go:GetBulkStatus | DONE |
| getConfigValues | api/config.go:ConfigGetAll | DONE |
| getCurrentUsername | api/user.go:GetUsername | DONE |
| getEligibility | api/eligibility.go:GetEligibility | DONE |
| batchEligibilityCheck | api/eligibility.go:BatchEligibilityCheck | DONE |
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

### Missing API Endpoints (4 of 20)

#### 1. addKeyToKeyring (P0 - Medium effort)
**Old Java:** ServiceImpl.java:602-610
**Go state:** model/keyring.go has KeyringModel + AddKeyToKeyring + GetKeyringEntry.
API handler still missing.
**Needs:** `api/keyring.go`: New handler file with `POST /add` route.

#### 2. addRemittUser (P0 - Medium effort)
**Old Java:** ServiceImpl.java:612-623
**Go state:** model/user.go has UserModel + AddUser(). API handler still missing.
**Needs:** `api/user.go`: Add `POST /add` route with admin ACL check.

#### 3. parseData (P2 - Medium effort)
**Old Java:** ServiceImpl.java:574-600
**Go state:** Parser interface exists (parser/interface.go), X12 parsers partially done
(parser/x12.go, parser/x12835.go). API handler missing.
**Needs:** `api/parser.go`: New handler.

#### 4. validatePayload (P0 - High effort)
**Old Java:** ServiceImpl.java:638-665
**Go state:** Validation interface + registry + X12Validator exist (validation/).
API handler missing.
**Needs:** `api/validation.go`: New handler.

---

## GAP ANALYSIS: BACKEND PLUGIN SYSTEM

### Translation Plugins: 4 of 4 DONE
| Plugin | Go File | Status |
|---|---|---|
| FixedFormPdf | translation/fixedformpdf.go | DONE |
| FixedFormXml | translation/fixedformxml.go | DONE |
| X12Passthrough | translation/x12passthrough.go | DONE |
| X12Xml | translation/x12xml.go | DONE |

### Transport Plugins: 7 of 7 DONE
| Plugin | Go File | Status |
|---|---|---|
| Script (otto JS) | transport/script.go | DONE |
| SFTP | transport/sftp.go | DONE |
| ScriptedHttpTransport | transport/script_http.go | DONE |
| StoreFile | transport/storefile.go | DONE |
| StoreFilePdf | transport/storefilepdf.go | DONE |
| ClaimLogic | transport/claimlogic.go | DONE |
| GatewayEdiTransport | transport/gatewayedi.go | DONE |

### Scooper Plugins: 2 of 2 DONE
| Plugin | Go File | Status |
|---|---|---|
| SftpScooper | scooper/sftp.go | DONE |
| GatewayEdiSftpScooper | scooper/gatewayedi.go | DONE |

### Eligibility Plugins: 8 of 8 DONE
| Plugin | Go File | Status |
|---|---|---|
| DummyEligibility | eligibility/dummy.go | DONE |
| GatewayEDIEligibility | eligibility/gatewayedi.go | DONE |
| NCMedicaidEligibility | eligibility/ncmedicaid.go | DONE |
| SftpEligibility | eligibility/sftp.go | DONE |
| StediEligibility | eligibility/stedi.go | DONE |
| OptumEligibility | eligibility/optum.go | DONE |
| MedicareHETSEligibility | eligibility/medicare_hets.go | DONE |
| BCBSFhirEligibility | eligibility/bcbs_fhir.go | DONE |

All plugins implement the EligibilityChecker interface and self-register
via init() + RegisterChecker(). GatewayEDIEligibility uses PGP-encrypted
SOAP calls. NCMedicaidEligibility and SftpEligibility use SFTP with
optional/always-on PGP encryption. StediEligibility and OptumEligibility
use clearinghouse REST JSON APIs covering all major commercial payers
(BCBS, United, Aetna, Cigna, Humana) plus Medicare/Medicaid.
MedicareHETSEligibility connects directly to CMS HETS via SOAP+MIME.
BCBSFhirEligibility uses FHIR R4 via 1upHealth for BCBS plans.

### Render Plugins: 2 of 2 DONE
| Plugin | Go File | Status |
|---|---|---|
| PreRenderedPlugin | render/prerendered.go | DONE |
| XsltPlugin | render/xslt.go | DONE |

Both self-register via init() + RegisterRenderer(). XsltPlugin supports
both internal (ratago) and external (xsltproc) XSLT processing.

### Validation Plugins: 1 of 1 DONE
| Plugin | Go File | Status |
|---|---|---|
| X12Validator | validation/x12validator.go | DONE |

---

## GAP ANALYSIS: INFRASTRUCTURE

### Task Scheduler (P0 — DONE)
**Go state:** `task/scheduler.go` — reads tJobs table, starts goroutines on
cron-like schedules. Resolves `EligibilityJobClass` → `RunEligibilityTask` and
`ScooperJobClass` → `RunScooperTask`. Job queue: `jobqueue/jobqueue.go` with
worker pool doing render → translate → transport.

### PGP/GPG Armoring (P1 — DONE)
**Go state:** `crypto/pgp.go` — `EncryptPGP`, `DecryptPGP`, `IsPGPEncrypted`
implemented using `golang.org/x/crypto/openpgp`.

### Parsing X12 (P2 - High effort)
**Go state:** model/x12xml.go defines the XML-to-X12 serialization format.
X12 parsers exist at parser/x12.go (envelope) and parser/x12835.go (835).
Remaining: 997 and 271 parsers + 22 x12dto DTO classes from old Java.

### Callback Support (P2 - Medium effort)
**Go state:** UserModel has callback fields but no callback sending logic.
**Needed:** `callback/` package.

---

## PRIORITIZED IMPLEMENTATION PLAN

### Phase 1: Foundation (~2 days)
1. **addRemittUser API** — POST /add route in api/user.go
2. **addKeyToKeyring API** — POST /add route in api/keyring.go
3. **parseData API** — handler in api/parser.go
4. **validatePayload API** — handler in api/validation.go

### Phase 2: X12 Parser Completion (~3-5 days)
5. **Port x12dto DTOs** — 22 DTO classes from org.remitt.parser.x12dto
6. **X12 997 parser** — functional acknowledgment parsing
7. **X12 271 parser** — eligibility response parsing

### Phase 3: Callback (~2-3 days)
8. **Callback support** — SOAP client for notifying originating systems

---

## DEPENDENCY GRAPH

```
Phase 1 (API handlers) ─── independent (all 4 are separate files)
Phase 2 (X12 parsers) ──── depends on: parser interface (exists)
Phase 3 (Callbacks) ────── independent
```

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
