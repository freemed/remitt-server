# REMITT pipeline wiring — plan (2026-09-15)

## Owner decisions (2026-09-15, all four answered)

1. **Execute this plan in order** — feeder, then translation resolution, then the
   translator input contract — each verified against the live database and the SSH
   harness, not against mocks.
2. **Feeder trigger: BOTH.** Poll like the Java *and* enqueue immediately on
   insert, with the poller skipping anything already in flight. Poll-only would
   make every submission wait up to 500 ms and would still be the only thing that
   picks up rows inserted by another path; enqueue-only would be blind to those
   rows and to work left waiting across a restart.
3. **Translation resolution: resolve in Go from the database's own mapping
   tables** (do what `p_ResolveTranslationPlugin` does), with the Go
   `InputFormat()` as a fallback when no row exists. The seeded formats are NOT to
   be edited — the database is the fixed 0.5.x contract.
4. **Promote the end-to-end driver** into the repo as an env-gated integration
   test, so the render→translate→transport claim is reproducible by anyone with a
   database. It is currently the only artifact that would tell us whether the
   feeder works, so it is being held until the feeder lands and can then act as
   the feeder's acceptance test (the same reasoning that had the SSH harness
   promoted before it was lost).

## Why this exists

Every part of the render → translate → transport chain was fixed and covered today,
and an end-to-end run against a real MySQL plus a real in-process SFTP server then
showed that **no job can run in production at all**, for four independent reasons.
They are not regressions from today's work; they are the wiring the Java original
had and the Go port never grew.

Evidence for each is in `.hermes/reports/e2e-live-run-4.log` and TODO.md.

## What the live run proved works

- The self-configuring transports: a job took its SFTP host/port/user/path purely
  from five `tUserConfig` rows under the seeded FQCN namespace, with a negative
  control (setting `sftpPort` to 1 in the database made it dial 127.0.0.1:1 and
  fail, and restoring the row delivered again).
- Host-key verification end to end: a real SSH+SFTP harness server with a real
  verified host key, configured through `paths.known-hosts`.
- A genuine render: `statement.xsl` produced 1175 bytes of fixed-form XML, and
  that document was **delivered** (name, size and md5 confirmed on both ends).
- The consuming half of the queue: with an item pushed onto `jobQueueChannel` by
  hand, worker[1] processed it to SUCCESS and the file landed.
- EXSLT/XSLT render of the shipped stylesheets through the in-process engine, with
  no xsltproc fallback.

## The four blockers, in dependency order

### 1. Nothing feeds the queue (no trigger) — BLOCKS EVERYTHING

`jobQueueChannel` is created (`jobqueue/jobqueue.go:42`) and read
(`:277`) and **never written**; no enqueue function exists and `api/` does not
import `jobqueue`, so `api.PayloadInsert` stores a row and returns its id. Live
proof: with `StartDispatcher(2)` running and a fresh `payloadState='valid'` row,
`tProcessor` rows stayed 0, `jobQueue` stayed empty, and no file and no connection
appeared. A hand-pushed item was processed normally.

Java did it by polling: `ControlThread.java:77-100` (500 ms loop, `work()`),
`ControlThread.java:557-583` (`getUnassignedPayloads`:
`SELECT a.id FROM tPayload a WHERE a.id NOT IN (SELECT b.payloadId FROM tProcessor b)
AND a.payloadState = 'valid' ORDER BY a.insert_stamp`), `work()` at `:587-655`
(takes a free worker, `migratePayloadToProcessor` at `:230-272` inserts the
`tProcessor` row), and each stage thread commits its output back
(`commitPayloadRun` `:276-300`, `pushToNextStage` `:722-760`).

Go equivalent needed:
1. a poll query (the dbgen layer has only GetPayloadById/InsertPayload/ResubmitPayload today);
2. skip payloads already in flight (`jobQueue` map);
3. build the item **with `lock: new(sync.RWMutex)`** — nothing initialises it today
   and the first `AppendLog`/`Fail` panics without it;
4. register under `jobQueueLock` in `jobQueue` AND send a copy on the channel (the
   worker looks the item up by ID and does `i.Lock()` with no nil check,
   `jobqueue.go:226-230`);
5. journal state into `tProcessor`/`tPayload` (today only a
   `// Journal update (sqlc migration pending)` comment).

### 2. Translation resolution ignores the database

`jobqueue/jobqueue.go:339` resolves with `translation.ResolveTranslator(w.RenderOption,
transportPlugin.InputFormat())` — the render **option** where the database resolves
by the option's declared **output format**. Observed: `unable to resolve translator
between '4010_837p' and 'x12'` for every shipped stylesheet. The Java resolved it in
the database (`p_ResolveTranslationPlugin`, `migrations/001_legacy.up.sql:327-338`,
using `renderPluginOutputFormat` `:276-291` and `transportPluginInputFormat`
`:293-310`; called from `ControlThread.java:667-686`). `model.GetPluginOptions`
exists but `executeJob` never calls it. When no translator resolves, Java marks the
payload failed (`RenderProcessorThread.java:100-118`) rather than continuing.

### 3. The translator is handed bytes where it wants a typed model

`jobqueue/jobqueue.go:356` passes `[]byte` to `Translate`, and `x12xml` wants
`model.X12Xml` → `jobqueue.go:356: executejob: translate: x12xml: translate: invalid datatype presented`. Java had ONE entry point for all three stages,
`PluginInterface.render(Integer jobId, byte[] input, String option)`
(`PluginInterface.java:41`), and each plugin parsed the bytes itself. Either give
the translator the bytes and let it parse (Java parity), or have the pipeline build
the typed model — decide once and note it, because `fixedformxml`/`fixedformpdf`
have the same shape.

### 4. `tFileStore` writes violate the foreign key — DISPATCHED

`storefile.go:49-59` and `storefilepdf.go:51-68` hardcode `PayloadID: 0` while
`tFileStore.payloadId → tPayload(id)`, so every write fails with error 1452.

## Sequencing

1. **Schema/query alignment** (`tUser.role`, `p_UserConfigUpdate`) — dispatched;
   without it even step 2's job fails at its first statement.
2. The FQCN registry aliases + the storefile payload identity — dispatched.
3. The feeder (blocker 1) with its five requirements above.
4. Translation resolution (blocker 2) + the translator input contract (blocker 3),
   with the fail-the-payload behaviour.

## How each step gets verified (do not skip)

- Against the **live MySQL** provisioned today (`docker exec remitt-mysql`,
  config `/tmp/remitt-e2e.yml`), by driving `executeJob` and reading back rows.
- The SFTP end through `test/harness/sshsftp` (promoted into the repo today), so a
  delivery is observed as real bytes on a real server.
- The gate for "the pipeline works" is: insert a payload **through the API path**
  (or the feeder's poll query), wait, and see the file land and the state move
  `valid` → the terminal state, with the row in `tProcessor`/`tFileStore`.
- A negative control for each claim, as the transports run used (a row pointed at
  a closed port, then restored).
