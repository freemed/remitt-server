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

### Render Plugins: 2 of 2, and the in-process engine is now equivalent
`PreRenderedPlugin` works. `XsltPlugin` is correct on both paths as of
2026-09-15: the in-process engine (ratago + the forked xpath, with gokogiri's
libxslt-compatible serializer) transforms all four shipped stylesheets to output
**byte-identical to `xsltproc`** — 15,287 / 15,607 / 9,698 / 2,048 bytes — and
does so deterministically across runs. The per-construct regression net
(`common/xsl_micro_test.go`) is 14/14 and the four-stylesheet comparison
(`TestXslTransform_Compare`) passes 4/4, in CI as well as locally.

Getting there fixed, in order: node-set variable resolution and iteration, the
EXSLT set functions (`set:distinct` by string-value on live DOM nodes),
carriage-return escaping and text-node indentation in serialization, XPath 1.0
`number()` semantics in `format-number`, context position for
`position()`/`last()`, and — the defect that had been masquerading as a content
difference — global variables being evaluated in map order, so a global whose
dependency had not been evaluated yet silently resolved to an empty node-set and
the wrong value was frozen forever. That last one made output non-deterministic
run to run.

`InternalXslt` now defaults to **true**: the in-process engine is the production
path. `common.XslTransform` is the single dispatcher both call sites use, and it
falls back to the `xsltproc` binary (still shipped) if the in-process engine
returns an error — skipped when no binary path is configured, and if both fail the
error names both. Setting `internal-xslt: false` selects the external engine
outright.

### Validation Plugins: 1 of 1 DONE, and it no longer lies
X12Validator (otto JS engine; scripts in resources/scripts/validation/). Until
2026-09-15 it hardcoded `Status: "success"` regardless of the script's verdict,
so every invalid payload — empty input, binary garbage, truncated envelopes —
was reported valid. It now rolls the status up from the script's verdict using
the Java `ValidationStatus` vocabulary and severity precedence, emits the
script's real messages, rejects delimiter inconsistencies before the script runs
(as the Java does), and reports a nil config instead of panicking. Deferred:
spec-script selection by GS08 (the port hardcodes the 837P script).

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
