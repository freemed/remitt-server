# REMITT Migration Gap Analysis & Implementation Plan

Generated: 2026-08-08 | Corrected: 2026-09-15

## Summary

**Breadth is complete; correctness is not.** All 20 API endpoints, all plugin
categories and all infrastructure pieces exist — and as of the 2026-09-15 audit,
several of them were claiming success without doing the work. This file
previously said "100% complete / Gap closed", which was wrong.

The authoritative, evidence-backed status is
`.hermes/plans/2026-09-15_101754-remitt-unfinished-work.md`. `TODO.md` carries a
matching REMAINING section. Every claim below was reproduced with a command; the
corrections are tracked in git (`ed0d3ba`, `0e3964f`, `6f08ad0`, `faf9d69`).

---

## GAP ANALYSIS: API LAYER

### All 20 of 20 endpoints DONE

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
| addRemittUser | api/user.go:UserAdd | DONE |
| addKeyToKeyring | api/keyring.go:KeyringAdd | DONE |
| parseData | api/parser.go:ParseData | DONE |
| validatePayload | api/validation.go:ValidatePayload | DONE |

All of the above are also exposed through the SOAP 1.1 compatibility layer
(`soap/soap.go` dispatchTable + `soap/adapters.go`), covered by
`soap/soap_test.go`.

---

## GAP ANALYSIS: BACKEND PLUGIN SYSTEM

### Translation Plugins: 4 of 4 DONE
FixedFormPdf, FixedFormXml, X12Passthrough, X12Xml

### Transport Plugins: 7 of 7 DONE
Script, SFTP, ScriptedHttpTransport, StoreFile, StoreFilePdf, ClaimLogic, GatewayEdiTransport

### Scooper Plugins: 2 of 2 DONE
SftpScooper, GatewayEdiSftpScooper

### Eligibility Plugins: 8 present, 2 NOT FUNCTIONAL
DummyEligibility, NCMedicaidEligibility, SftpEligibility, StediEligibility,
OptumEligibility, BCBSFhirEligibility — these six make real calls.

- **GatewayEDIEligibility** — as of 2026-09-15 it performs a real POST and maps
  the response exactly as the Java plugin does (all 8 `SuccessCode` values to the
  Java `(Status, SuccessCode)` pairs, via `gatewayEdiSuccessCodeMappings`). It
  still cannot interoperate with the real endpoint: the request envelope is not
  the vendor's `WSEligibilityInquiry` and no HTTP Basic auth is sent. This was
  previously a stub that reported success with no network call at all.
- **MedicareHETSEligibility** — still returns a canned X12 271 success with no
  HTTP call (it needs CMS credentials). Treat its output as unverified.

The Go `EligibilityStatus`/`EligibilitySuccessCode` values were also incomplete
(1 of 5 statuses, 2 of 8 success codes defined) until 2026-09-15, which is why a
plugin could not have reported what real payers return.

### Render Plugins: 2 present, 1 effectively unusable in-process
PreRenderedPlugin works. XsltPlugin is correct only when `InternalXslt` is false
and it shells out to `xsltproc`: the in-process engine produces
6,425 / 6,396 / 34 / 34 bytes where xsltproc produces 15,287 / 15,607 / 9,698 /
2,048 for the four shipped stylesheets. `xsltproc` is REQUIRED in production
today; `common/xsl_micro_test.go` pins the 7 broken constructs and
`TestXslTransform_Compare` fails 4/4.

### Validation Plugins: 1 of 1 DONE
X12Validator (otto JS engine; scripts in resources/scripts/validation/)

### Parser Plugins: 4 of 4 DONE
X12 (envelope), X12835 (835 remittance), X12997 (997 acknowledgment), X12271 (271 eligibility)

---

## GAP ANALYSIS: INFRASTRUCTURE

### Task Scheduler — DONE
`task/scheduler.go` — cron-like scheduling from tJobs, EligibilityTask + ScooperTask

### PGP/GPG Armoring — DONE
`crypto/pgp.go` — EncryptPGP, DecryptPGP, IsPGPEncrypted. Covered by
`crypto/pgp_test.go` (64 subtests). Known asymmetry, pinned by test: EncryptPGP
emits binary OpenPGP, which IsPGPEncrypted does not detect (it matches only the
ASCII armor prefix), so callers that must branch on encryption need a binary
fallback.

### X12 Parsing — DONE
X12 envelope, 835 remittance, 997 acknowledgment, 271 eligibility response

### Callback Support — DONE
`callback/` package — SOAP client notifying originating systems on job completion

### Job Queue — DONE
`jobqueue/jobqueue.go` — worker pool with render → translate → transport → callback pipeline

### Keyring — DONE
`model/keyring.go` — AddKeyToKeyring, GetKeyringEntry with sqlc queries

### CI — green, but blind to the sub-modules
`.github/workflows/go.yml` had been failing on master since commit `71c029d`
because the committed `go.mod` replaced sibling checkouts that only exist on a
developer workstation. Fixed in `ed0d3ba`; CI is green. The workflow's
`go test -v ./...` still covers root-module packages only (workspace-mode `./...`
does not descend into the sub-modules), so a failing `common` test is invisible —
see TODO.md REMAINING.

---

## Checked, and NOT a gap

- `render` is legitimately its own module; it was simply missing from `go.work`,
  which excluded it from every workspace build and test. Fixed in `ed0d3ba`.
- `github.com/freemed/xpath v1.3.9` was pinned while the fork carried an untagged
  follow-up fix; that fix is now released as `v1.3.10` and required.
- MD5 password hashing, the `t`-prefixed table names and the MySQL stored
  procedures are deliberate 0.5.x schema compatibility, not debt.

---

## Correction log

| Was claimed | Actual, verified 2026-09-15 |
|---|---|
| "100% complete / Gap closed" | 2 eligibility plugins non-functional or fake, XSLT engine not equivalent to xsltproc, CI red on master |
| "Eligibility 8 of 8 DONE" | 6 of 8 real; GatewayEDI could not interoperate; HETS returned a canned success |
| "Render 2 of 2 DONE" | XsltPlugin correct only via the external `xsltproc` binary |
| "callback/ ... complete" (no change) | accurate |
