# REMITT Migration Gap Analysis & Implementation Plan

Generated: 2026-08-08 | Completed: 2026-08-10

## Summary

The Go remitt-server is **100% complete** relative to the Java 0.5.x REMITT.
All 20 API endpoints, all plugin categories, all infrastructure pieces
are implemented and tested.

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

---

## GAP ANALYSIS: BACKEND PLUGIN SYSTEM

### Translation Plugins: 4 of 4 DONE
FixedFormPdf, FixedFormXml, X12Passthrough, X12Xml

### Transport Plugins: 7 of 7 DONE
Script, SFTP, ScriptedHttpTransport, StoreFile, StoreFilePdf, ClaimLogic, GatewayEdiTransport

### Scooper Plugins: 2 of 2 DONE
SftpScooper, GatewayEdiSftpScooper

### Eligibility Plugins: 8 of 8 DONE
DummyEligibility, GatewayEDIEligibility, NCMedicaidEligibility, SftpEligibility,
StediEligibility, OptumEligibility, MedicareHETSEligibility, BCBSFhirEligibility

### Render Plugins: 2 of 2 DONE
PreRenderedPlugin, XsltPlugin

### Validation Plugins: 1 of 1 DONE
X12Validator

### Parser Plugins: 4 of 4 DONE
X12 (envelope), X12835 (835 remittance), X12997 (997 acknowledgment), X12271 (271 eligibility)

---

## GAP ANALYSIS: INFRASTRUCTURE

### Task Scheduler — DONE
`task/scheduler.go` — cron-like scheduling from tJobs, EligibilityTask + ScooperTask

### PGP/GPG Armoring — DONE
`crypto/pgp.go` — EncryptPGP, DecryptPGP, IsPGPEncrypted

### X12 Parsing — DONE
X12 envelope, 835 remittance, 997 acknowledgment, 271 eligibility response

### Callback Support — DONE
`callback/` package — SOAP client notifying originating systems on job completion

### Job Queue — DONE
`jobqueue/jobqueue.go` — worker pool with render → translate → transport → callback pipeline

### Keyring — DONE
`model/keyring.go` — AddKeyToKeyring, GetKeyringEntry with sqlc queries

---

**Gap closed. All 20 API endpoints, 28 plugins across 6 categories, 5 infrastructure pieces — complete.**
