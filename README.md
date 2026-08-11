# REMITT SERVER

![](ui/img/remitt.jpg)

[![Build Status](https://github.com/freemed/remitt-server/actions/workflows/go.yml/badge.svg)](https://github.com/freemed/remitt-server/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/freemed/remitt-server)](https://goreportcard.com/report/github.com/freemed/remitt-server)
[![GoDoc](https://godoc.org/github.com/freemed/remitt-server?status.png)](https://godoc.org/github.com/freemed/remitt-server)
[![Join the chat at https://gitter.im/freemed/remitt-server](https://badges.gitter.im/Join%20Chat.svg)](https://gitter.im/freemed/remitt-server?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)

**REMITT** (Electronic Medical Information Translation and Transmission) —
Go rewrite of the J2EE 0.5.x SOAP backend, targeting 100% feature parity against
the same MySQL schema. Runs against a valid REMITT 0.5.x database with no
modifications.

## Architecture

| Layer | Technology |
|---|---|
| Language | Go 1.25 |
| Web framework | [Echo v5](https://github.com/labstack/echo) (migrated from Gin) |
| ORM | [GORP](https://github.com/go-gorp/gorp) + supplementary [sqlc](https://sqlc.dev) query layer |
| Database | MySQL (same schema as REMITT 0.5.x — tables, stored procedures, column names are a fixed contract) |
| Auth | Stateless HTTP BasicAuth (MD5 hashes — legacy 0.5.x compatibility) |
| Config | YAML (`config.yaml`) |
| Plugin model | Registry + factory pattern (`init()` self-registration) |

## Plugin Systems (all complete)

Every plugin type follows a uniform registry/factory pattern with `init()`
self-registration:

| System | Interface | Implementations | Status |
|---|---|---|---|
| **Eligibility** | `EligibilityChecker` | Dummy, Gateway EDI (SOAP+PGP), NC Medicaid (SFTP+PGP), SFTP (optional PGP), Optum (Change Healthcare OAuth2 REST), Stedi (OAuth2 REST), BCBS FHIR (1upHealth), Medicare HETS (CMS SOAP/X12 270/271) | 8/8 done |
| **Transport** | `Transporter` | Script (otto JS), SFTP, StoreFile, StoreFilePdf, ClaimLogic, Gateway EDI, ScriptedHttp | 7/7 done |
| **Parser** | `Parser` | X12 envelope (ISA/GS/ST/SE/GE/IEA), X12 835 remittance, X12 271 eligibility response, X12 997 functional ack | 4/4 done |
| **Translation** | `Translator` | X12Xml (XML→EDI text), X12Passthrough (raw bytes), FixedFormXml (XML→fixed-width), FixedFormPdf (XML→PDF via gofpdf+gofpdi) | 4/4 done |
| **Render** | `Renderer` | XsltPlugin (ratago or xsltproc), PreRenderedPlugin (passthrough) | 2/2 done |
| **Callback** | `CallbackSender` | SOAP callback to originating systems (WS-Security UsernameToken, fires non-blocking post-pipeline) | 1/1 done |
| **Validation** | `Validator` | X12Validator (otto JS engine, scripts in `resources/scripts/validation/`) | 1/1 done |
| **Scooper** | `Scooper` | SftpScooper, GatewayEdiSftpScooper (SFTP + PGP decrypt) | 2/2 done |

## EDI Pipeline

```
Render → Translation → Transport → Callback
```

1. **Render** — produces intermediate output (XSLT transform or passthrough)
2. **Translation** — resolves format bridge (e.g. X12Xml → X12 EDI text, FixedFormXml → PDF)
3. **Transport** — delivers the result (SFTP, script, HTTP, local file store)
4. **Callback** — non-blocking SOAP notification to originating systems with job result

Jobs are dispatched through a channel-based worker pool (`jobqueue/`). Scheduled
tasks (eligibility checks, scooper runs) are driven by `task/scheduler.go`.

## API Endpoints (20/20 complete)

All 20 endpoints from the Java 0.5.x API surface are implemented:

- Authentication & users: `changePassword`, `getCurrentUsername`, `addRemittUser`, `listRemittUsers`
- Config: `getConfigValues`, `setConfigValue`
- Payloads: `insertPayload`, `resubmitPayload`, `getStatus`, `getBulkStatus`
- Files: `getFile`, `getFileList`, `getOutputMonths`, `getOutputYears`
- Eligibility: `getEligibility`, `batchEligibilityCheck`
- Keyring: `addKeyToKeyring`
- Plugins: `getPlugins`, `getPluginOptions`
- Protocol: `getProtocolVersion`
- Validation: `validatePayload`
- Parsing: `parseData`

Access control is role-based (`admin` / `user`) via BasicAuth middleware.

## Architectural Changes from REMITT 0.5.x

- **Language:** Java/J2EE → Go with Echo v5 web framework
- **Framework:** Apache CXF SOAP → RESTful JSON API + SOAP 1.1 compatibility layer (`soap/`) for legacy Java clients
- **ORM:** Hibernate → GORP with supplementary sqlc query layer for type-safe generated queries
- **Plugin model:** Spring beans → registry/factory pattern with `init()` self-registration
- **Job execution:** Thread-per-job → channel-based worker pool (`jobqueue/`)
- **Scheduling:** Quartz → lightweight in-process scheduler (`task/scheduler.go`)
- **Auth:** Container-managed security → stateless HTTP BasicAuth with MD5 password hashing (legacy DB compatibility)
- **Eligibility:** Expanded from 4 to 8 checkers — added OAuth2 REST (Optum, Stedi), FHIR R4 (BCBS via 1upHealth), and CMS HETS SOAP/X12 270/271 (Medicare)
- **X12:** Full envelope + 835/271/997 parsers with 22 DTO structs
- **PGP/GPG:** Native Go armoring (`crypto/pgp.go`) — encrypt, decrypt, detect
- **Testing:** E2E pipeline test suite (`test/e2e/`) with testdata fixtures + per-plugin unit tests

## Dependencies

- [Echo v5](https://github.com/labstack/echo) — web framework
- [GORP](https://github.com/go-gorp/gorp) — database access layer
- [sqlc](https://sqlc.dev) — type-safe generated queries (`internal/db/`)
- [Go-MySQL-Driver](https://github.com/go-sql-driver/mysql) — MySQL driver
- [otto](https://github.com/robertkrimen/otto) — JavaScript engine (script transports, X12 validation)
- [goquery](https://github.com/PuerkitoBio/goquery) — HTML scraping (scripted HTTP transport)
- [pkg/sftp](https://github.com/pkg/sftp) — SFTP client
- [gofpdf](https://github.com/phpdave11/gofpdf) + [gofpdi](https://github.com/phpdave11/gofpdi) — PDF generation (FixedFormPdf translation)
- [ratago](https://github.com/freemed/ratago) — native Go XSLT processor
- [gokogiri](https://github.com/freemed/gokogiri) — native Go XML/DOM/XPath support
- [golang-migrate](https://github.com/golang-migrate/migrate) — database migrations
- [prometheus/client_golang](https://github.com/prometheus/client_golang) — metrics instrumentation

## Operations

### Prometheus Metrics

The server exposes standard HTTP metrics at `/metrics` in Prometheus text format:

- `http_requests_total` — counter by method, path, status
- `http_request_duration_seconds` — histogram by method, path, status
- `http_requests_in_flight` — gauge of concurrent requests

### Docker

```bash
docker build -t remitt-server .
docker run -p 3000:3000 -v ./remitt.yml:/etc/remitt/remitt.yml remitt-server
```

The multi-stage Dockerfile produces a minimal Debian-slim image with xsltproc
for external XSLT transforms. All SSH/SFTP connections use Go's
`golang.org/x/crypto/ssh` — no system SSH client required.

## Quick Start

```bash
cp config.yaml.example config.yaml
# edit config.yaml with your MySQL credentials
go run ./cmd/remitt-server/
```

The server starts on the port configured in `config.yaml` (default: 8080).

Code in this repository runs against a valid REMITT 0.5.x series database with
no modifications.

Full migration task list: [TODO.md](TODO.md)
