// live_e2e_test.go is the repository's end-to-end acceptance test for the whole
// pipeline: render -> translate -> transport, driven through the REAL trigger.
//
// # WHAT THIS FILE IS
//
// It is the promoted form of the throwaway driver that lived in
// .hermes/e2e-driver/zz_e2e_live_test.go (a git-ignored directory, so the only
// artifact that ever reproduced the full claim died on clone). Unlike that
// driver it drives the real trigger path and asserts outcomes instead of
// printing them: it inserts a tPayload row exactly the way api.PayloadInsert
// does - the same dbgen.InsertPayloadParams, then the same
// jobqueue.EnqueuePayload call (api/payload.go:66-96) - and starts the workers
// through the production entry point, jobqueue.StartDispatcher.
//
// It does NOT call executeJob, does NOT write jobQueueChannel, and carries no
// ALTER TABLE. All three were workarounds for defects that have since been
// fixed: the queue now has a trigger (api.PayloadInsert enqueues on insert and
// the poller is the safety net, both through EnqueuePayload), and the tUser.role
// schema defect is fixed, so model.GetUserByName works against the real schema.
// A test that hand-feeds the queue cannot show that the trigger works - which is
// the one thing this test exists to show.
//
// THE GATE, AND WHY THIS ONE
//
//	REMITT_LIVE_CONFIG=/tmp/remitt-verify.yml go test -count=1 -v -run TestLive_E2E ./...
//
// REMITT_LIVE_CONFIG is the gate every other live test in this repository uses
// (transport/storefile_live_test.go, translation/registry_live_test.go,
// model/user_live_verify_test.go), and a config FILE - not a bare DSN - is what
// this test needs: paths.base locates resources/xsl and migrations, paths.temp
// is where the render engine writes, and the database block is the database to
// run against. (/tmp/remitt-verify.yml and /tmp/remitt-e2e.yml from the
// 2026-09-15 live runs both work; they point at remitt/remitt@tcp(127.0.0.1:3307),
// this project's own remitt-mysql container - never another project's.)
//
// With the variable unset the test SKIPS, so a plain `go test ./...` never
// touches a database; the skip message names the variable and the config file to
// write. With it set but the database unreachable it also skip: an absent
// database is an environment fact, not a pipeline defect, and the skip message
// names the endpoint and the failure. The trade-off is deliberate and stated:
// a job that sets the gate and loses its database goes green with a visible
// SKIP rather than red.
//
// WHAT "THE PIPELINE WORKS" MEANS HERE - the acceptance bar, as assertions
//
//  1. TRIGGER (on insert). A tPayload row inserted through the API's insert
//     path and enqueued by the API's own enqueue call is processed by a worker
//     of a dispatcher started through jobqueue.StartDispatcher - with the
//     poller OFF, so nothing but the on-insert trigger can have processed it.
//  2. TERMINAL STATE. tPayload.payloadState reaches 'completed'.
//  3. JOURNAL. Exactly ONE tProcessor row exists for that payload (no second
//     trigger claimed it), tsStart set by the enqueue, threadId set to the
//     worker that ran it, tsEnd stamped by the worker.
//  4. DELIVERY. The translated bytes leave the process: the SFTP transport
//     delivers them to a real SSH/SFTP server (test/harness/sshsftp) as
//     <nanoseconds>.x12, and the file's content is compared byte-for-byte (md5)
//     against the bytes the render stage produced; the StoreFile transport
//     writes a tFileStore row whose content md5, contentsize and
//     payloadId/processorId are the job's own - which can only hold if the
//     enqueue path attached the job identity (common/jobcontext.go).
//  5. TRIGGER (poll). A row inserted by a path that enqueues nothing is
//     untouched while only the workers run, and once the poller runs it reaches
//     the same terminal state and the same delivery.
//
// A run that cannot show all five fails, and the failure carries the job's own
// log lines (read from the queue map) so the stage that broke is named.
//
// THE PAYLOAD/OPTION PAIR THAT IS USED, AND THE CONSTRAINT BEHIND IT
//
// renderPlugin "org.remitt.plugin.render.PreRenderedPlugin" with renderOption
// "x12" and an x12-consuming transport resolves to a real translator - the x12
// passthrough - both through the Go registries and through the resolution the
// pipeline now uses first (ResolveTranslatorForJob: the render plugin/option
// and transport plugin/option looked up in tTranslation, falling back to the Go
// registries when the database has no row), so this pair is what the delivery
// cases submit.
//
// What this test deliberately does NOT depend on is a SHIPPED STYLESHEET option
// resolving (4010_837p, 5010_837p, cms1500, statement). Two reasons, both
// reasons to keep it out of an acceptance test rather than in it:
//
//   - Translator resolution is a concurrent workstream. When this test was
//     promoted the shipped options resolved to nothing (the 2026-09-15 runs
//     recorded "unable to resolve translator between '4010_837p' and 'x12'":
//     .hermes/reports/e2e-live-run-4.log), and ResolveTranslatorForJob - which
//     makes them resolve - landed in the tree alongside it. A test that passes
//     only if a neighbour's in-flight work is complete is not an acceptance
//     test; it is a race.
//   - Even resolved, the pair is not a working chain yet: the render plugins
//     return raw bytes (render/xslt.go:22, render/prerendered.go:14) while the
//     translators those options resolve to assert a typed value and reject
//     anything else (translation/x12xml.go:32 and translation/fixedformxml.go:35
//     both do `src, ok := source.(model.X12Xml)` / `model.FixedFormXml`), so the
//     stage fails at the translator's own type check. That render/translator
//     interface gap is a third workstream, and asserting around it from here
//     would hide it.
//
// So the render stage is exercised directly through the render plugin the
// pipeline itself uses (render.InstantiateRenderer(XsltPlugin).Render(<remitt
// input>, "statement"): a real shipped stylesheet, a real engine), and the bytes
// it produces are what gets submitted as the pre-rendered payload - the
// delivered file's md5 is the md5 of a real render rather than of a literal in
// this file. Anyone adding a trigger-path case for a stylesheet option should do
// it when that option's whole chain (render output type -> translator ->
// transport) works, and should keep this pair's cases as well.
//
// WHAT IT TOUCHES, AND HOW IT IS UNDONE
//
//   - tUserConfig: the pipeline path never calls SetOptions, so a transport
//     resolves its endpoint from the caller's own rows (transport/selfconfig.go,
//     read through tUserConfig). The test therefore writes the five sftp* rows
//     for the test user under the Java FQCN namespace the legacy seed uses
//     (migrations/001_legacy.up.sql:62-70), pointing at the harness. The rows
//     are replaced, not updated: tUserConfig has no key on
//     (user, cNamespace, cOption) at all, so duplicates are possible and an
//     UPDATE would leave them behind. All rows under that namespace are
//     snapshotted first and rewritten verbatim at the end.
//   - tPayload/tProcessor/tFileStore: the test records the high-water mark of
//     each table before it runs and deletes everything above it afterwards, in
//     foreign-key order. That covers both the rows it created and any row the
//     poller picked up on its own: the poll query is database-wide by design
//     (GetUnassignedPayloadIds has no owner), so a row another run left
//     'valid' is enqueued too - the test restores those rows' payloadState to
//     what it recorded and says so. The leftovers are then counted and asserted
//     to be zero, so the cleanup is proved, not claimed.
//   - The SFTP root is a temporary directory the harness owns and removes.
//   - The dispatcher: StartDispatcher has no shutdown, so the test starts the
//     workers with the poller disabled in memory and starts the production
//     poller separately with a context it cancels; after that nothing enqueues
//     any more work and the workers idle. The queue is drained to terminal
//     before cleanup, so nothing writes to the rows cleanup is deleting.
package jobqueue

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/render"
	"github.com/freemed/remitt-server/test/harness/sshsftp"
)

const (
	// liveConfigEnv is the gate. See the header for why it is this one.
	liveConfigEnv = "REMITT_LIVE_CONFIG"

	// liveUser is the user the legacy seed creates
	// (migrations/001_legacy.up.sql, tUser 'Administrator') and the one the
	// tUserConfig rows belong to.
	liveUser = "Administrator"

	// The plugin names exactly as the legacy database stores them (Java
	// FQCNs, see migrations/001_legacy.up.sql and the registry aliases in
	// render/map.go, translation/map.go and transport/map.go).
	liveRendererXslt        = "org.remitt.plugin.render.XsltPlugin"
	liveRendererPreRendered = "org.remitt.plugin.render.PreRenderedPlugin"
	liveTransportSftp       = "org.remitt.plugin.transport.SftpTransport"
	liveTransportStoreFile  = "org.remitt.plugin.transport.StoreFile"

	// liveRenderOption is the one option whose output format a translator
	// resolves for today; see the header's constraint section.
	liveRenderOption = "x12"

	// livePollInterval is the interval the poller under test is started with.
	// The production default (500 ms, queue.poll-interval-ms) is fine, but an
	// acceptance run should not spend whole seconds waiting for a tick.
	livePollInterval = 150 * time.Millisecond

	// liveDeadline bounds every wait. A delivery is a millisecond-scale
	// operation; this is generous enough for a busy machine and short enough
	// that a hung pipeline fails the run instead of blocking it.
	liveDeadline = 60 * time.Second
)

// ---------------------------------------------------------------------------
// the test
// ---------------------------------------------------------------------------

// TestLive_E2E_PayloadReachesTheTransport is the acceptance test described in
// this file's header. It skips without REMITT_LIVE_CONFIG.
func TestLive_E2E_PayloadReachesTheTransport(t *testing.T) {
	db := liveDatabase(t)
	cfg := config.Config

	// ---------------------------------------------------------------- preconditions
	// The tUser.role schema defect that forced the old driver to ALTER TABLE is
	// fixed; assert it, because executeJob's very first statement is this call
	// (jobqueue/jobqueue.go:356) and a regression there would fail every job
	// with a message that never mentions the schema.
	user, err := model.GetUserByName(liveUser)
	if err != nil {
		t.Fatalf("model.GetUserByName(%q) = %v; the pipeline's first step is this call, and it must work against the real schema (no ALTER TABLE is applied by this test)", liveUser, err)
	}
	roles, err := user.GetRoles()
	if err != nil {
		t.Fatalf("UserModel.GetRoles() = %v; roles live in tRole (tUser has no role column)", err)
	}
	t.Logf("precondition: model.GetUserByName(%q) -> tUser.id %d, role %q, tRole rows %v", liveUser, user.Id, user.Role, roles)

	// -------------------------------------------------------------- the harness
	srv := sshsftp.Start(t)
	knownHosts, err := srv.WriteKnownHosts(filepath.Join(t.TempDir(), "known-hosts"))
	if err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	// The harness generates its host key per run, so no config file can carry
	// it: the path is overridden in memory and host key verification stays ON
	// (sftp-insecure-ignore-hostkey is not used).
	cfg.Paths.KnownHostsPath = knownHosts
	t.Logf("sftp harness: %s (%s) root %s, known_hosts %s", srv.Address(), srv.Fingerprint(), srv.Dir, knownHosts)
	if cfg.Paths.KnownHostsPath != knownHosts || cfg.SftpInsecureIgnoreHostKey {
		t.Fatalf("host key verification is not in effect (known-hosts %q, insecure %v)", cfg.Paths.KnownHostsPath, cfg.SftpInsecureIgnoreHostKey)
	}

	// --------------------------------------------------------- what this touches
	mark := liveTakeWatermark(t, db)
	liveInstallSftpUserConfig(t, db, srv)

	// ------------------------------------------------------------- the fixtures
	// The stylesheet the render stage will run, and the X12-ish fixture the
	// poller case submits. Both are located from the configured base path, so
	// this test has no absolute path to this machine in it.
	statementXsl := filepath.Join(cfg.Paths.BasePath, "resources", "xsl", "statement.xsl")
	if _, err := os.Stat(statementXsl); err != nil {
		t.Fatalf("the configured paths.base (%q) does not hold resources/xsl/statement.xsl: %v", cfg.Paths.BasePath, err)
	}
	fixturePath := filepath.Join(cfg.Paths.BasePath, "test", "testdata", "x12_intermediate.xml")
	fixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read %s: %v", fixturePath, err)
	}
	t.Logf("fixture %s: %d bytes md5 %s", fixturePath, len(fixture), liveMD5(fixture))

	// ------------------------------------------------------- the dispatcher (1/2)
	// StartDispatcher is the production entry point (cmd/remitt-server/main.go).
	// It starts the workers and, unless disabled, the poller with
	// context.Background() - a poller nothing can stop. The poller's lifetime
	// has to belong to this test (it queries tPayload database-wide), so it is
	// held for the poll case below and started through the same production
	// function with a context this test cancels.
	cfg.Queue.PollEnabled = false
	workers := cfg.TimingIterations.NumWorkerThreads
	t.Logf("configuration: %d worker thread(s); queue.poll-enabled forced to false in memory so nothing but the on-insert trigger "+
		"can process a payload during the delivery cases. The poller case starts the same production poller (StartPollerWithInterval) "+
		"with a context this test cancels, interval %s.", workers, livePollInterval)
	StartDispatcher(workers)

	t.Cleanup(func() {
		mark.restore(t, db)
	})

	// =====================================================================
	// 1. The render stage, through the render plugin the pipeline uses.
	// =====================================================================
	var rendered []byte
	t.Run("render_stage_produces_the_delivered_bytes", func(t *testing.T) {
		renderer, err := render.InstantiateRenderer(liveRendererXslt)
		if err != nil {
			t.Fatalf("render.InstantiateRenderer(%q) = %v", liveRendererXslt, err)
		}
		rendered, err = renderer.Render(liveStatementInput, "statement")
		if err != nil {
			t.Fatalf("render(XsltPlugin, statement) on the <remitt> input = %v; the render leg of the pipeline does not work", err)
		}
		if len(rendered) == 0 {
			t.Fatal("render(XsltPlugin, statement) produced 0 bytes")
		}
		if bytes.Equal(rendered, liveStatementInput) {
			t.Error("the statement stylesheet returned its input unchanged; that is not a render")
		}
		if !bytes.Contains(rendered, []byte("<fixedform")) {
			t.Errorf("the statement render has no <fixedform root (first 200 bytes: %q); the stylesheet's output shape changed", liveFirst(rendered, 200))
		}
		t.Logf("render(XsltPlugin, statement): %d bytes md5 %s, first 160 bytes %q",
			len(rendered), liveMD5(rendered), liveFirst(rendered, 160))
	})
	if t.Failed() {
		t.Fatal("the render stage failed; the delivery cases below would be meaningless")
	}

	// =====================================================================
	// 2. Trigger 1: the API's insert path, delivered over a real SFTP server.
	// =====================================================================
	t.Run("api_insert_is_enqueued_and_delivered_over_sftp", func(t *testing.T) {
		before := liveUploaded(t, srv)
		connsBefore := srv.Connections()

		id := liveInsertPayloadTheApiWay(t, rendered, liveRendererPreRendered, liveRenderOption, liveTransportSftp,
			"live-e2e-sftp-"+time.Now().Format("150405"))

		row := liveAwaitTerminal(t, id)
		liveAssertCompleted(t, "sftp delivery", row)

		file := liveAssertOneNewFileWithContent(t, srv, before, rendered)
		t.Logf("delivered: %s (%d bytes, md5 %s) - identical to the submitted payload byte-for-byte", file.name, len(file.content), liveMD5(file.content))
		if !strings.HasSuffix(file.name, ".x12") {
			t.Errorf("the delivered file is named %q; executeJob derives the extension from the transport's InputFormat (x12 for the SFTP transport), so it must end in .x12", file.name)
		}
		if got := srv.Connections() - connsBefore; got < 1 {
			t.Errorf("the SFTP server accepted %d new connections during the delivery; want at least 1 - the file cannot have arrived over a real SSH session otherwise", got)
		} else {
			t.Logf("the SFTP server accepted %d new connection(s) during the delivery", got)
		}
	})

	// =====================================================================
	// 3. Trigger 1 again, through the transport that writes a row.
	// =====================================================================
	t.Run("api_insert_writes_the_tfilestore_row_via_storefile", func(t *testing.T) {
		id := liveInsertPayloadTheApiWay(t, rendered, liveRendererPreRendered, liveRenderOption, liveTransportStoreFile,
			"live-e2e-storefile-"+time.Now().Format("150405"))

		row := liveAwaitTerminal(t, id)
		liveAssertCompleted(t, "storefile delivery", row)

		// The row the transport wrote, found by the payload it belongs to.
		var (
			fileID, contentSize, payloadID, processorID int64
			username, category, filename                string
			content                                     []byte
		)
		err := db.QueryRow(
			`SELECT id, user, category, filename, payloadId, processorId, contentsize, content
			   FROM tFileStore WHERE payloadId = ?`,
			id).Scan(&fileID, &username, &category, &filename, &payloadID, &processorID, &contentSize, &content)
		if err != nil {
			t.Fatalf("no tFileStore row for tPayload %d: %v (the storefile transport did not persist the delivery)", id, err)
		}
		t.Logf("tFileStore id=%d user=%s category=%s filename=%s payloadId=%d processorId=%d contentsize=%d md5=%s",
			fileID, username, category, filename, payloadID, processorID, contentSize, liveMD5(content))

		if username != liveUser {
			t.Errorf("tFileStore.user = %q; want %q (the user carried in the job context)", username, liveUser)
		}
		if category != "output" {
			t.Errorf("tFileStore.category = %q; want %q (the Java literal)", category, "output")
		}
		if payloadID != id {
			t.Errorf("tFileStore.payloadId = %d; want %d (the tPayload row the job was enqueued for)", payloadID, id)
		}
		if processorID != row.processorID {
			t.Errorf("tFileStore.processorId = %d; want %d (the tProcessor row the enqueue journaled - this is what proves the enqueue path attached the job identity)", processorID, row.processorID)
		}
		if contentSize != int64(len(content)) {
			t.Errorf("tFileStore.contentsize = %d; want %d (the length of the row's own content)", contentSize, len(content))
		}
		if liveMD5(content) != liveMD5(rendered) {
			t.Errorf("tFileStore.content md5 = %s; want %s (the bytes the render stage produced, byte-for-byte)", liveMD5(content), liveMD5(rendered))
		}
		if !strings.HasSuffix(filename, ".txt") {
			t.Errorf("tFileStore.filename = %q; the storefile transport's InputFormat is %q, so executeJob's extension mapping must produce .txt", filename, "*")
		}
	})

	// =====================================================================
	// 4. Trigger 2: a row inserted by another path, picked up by the poller.
	// =====================================================================
	t.Run("a_row_inserted_by_another_path_is_delivered_by_the_poller", func(t *testing.T) {
		// A payload the API never saw: the row goes in with SQL, and nothing
		// enqueues it. This is the case the poller exists for (the soap
		// adapter, an import script, a direct INSERT, work waiting across a
		// restart).
		id := liveInsertPayloadRowOnly(t, fixture, liveRendererPreRendered, liveRenderOption, liveTransportSftp,
			"live-e2e-poll-"+time.Now().Format("150405"))

		// BEFORE: only the workers are running, so the row must sit untouched.
		// A short, bounded observation window - not a race with anything: no
		// poller exists in this process yet.
		time.Sleep(2 * time.Second)
		pending := liveReadPayload(t, id)
		if pending.state != payloadStateValid || pending.processorRows != 0 {
			t.Fatalf("with only the workers running, tPayload %d is state=%q with %d tProcessor row(s); want %q and 0 - something other than the poller processed a row that was never enqueued",
				id, pending.state, pending.processorRows, payloadStateValid)
		}
		t.Logf("BEFORE the poller: tPayload %d state=%q, tProcessor rows=%d, queue: %s",
			id, pending.state, pending.processorRows, liveQueueStatus(id))

		// AFTER: start the poller on the production code path, with a context
		// this test cancels so no poller outlives the run.
		before := liveUploaded(t, srv)
		pollCtx, stopPolling := context.WithCancel(context.Background())
		defer stopPolling()
		StartPollerWithInterval(pollCtx, livePollInterval)
		t.Logf("StartPollerWithInterval(%s) started; queue.poll-interval-ms in the config is %d", livePollInterval, cfg.Queue.PollIntervalMs)

		row := liveAwaitTerminal(t, id)
		liveAssertCompleted(t, "poller delivery", row)

		file := liveAssertOneNewFileWithContent(t, srv, before, fixture)
		t.Logf("the poller delivered %s (%d bytes, md5 %s) for a payload nothing ever enqueued", file.name, len(file.content), liveMD5(file.content))

		// Stop the poller before cleanup reads and deletes the rows, then wait
		// for the jobs it already enqueued (this payload, and any row another
		// run left 'valid') to reach a terminal state.
		stopPolling()
		liveWaitForQueueToDrain(t, 30*time.Second)
		t.Logf("poller stopped and the queue drained; queue: %s", liveQueueStatus(id))
	})

	// The sub-tests are done; the remaining work is t.Cleanup's, which restores
	// the watermark and asserts it left nothing behind.
}

// ---------------------------------------------------------------------------
// the gate and the database
// ---------------------------------------------------------------------------

// liveDatabase enforces the gate and returns the application's own database
// handle.
//
// It loads REMITT_LIVE_CONFIG, checks the endpoint is reachable BEFORE calling
// model.InitDb - which log.Fatalln()s on an unreachable database, killing the
// test binary instead of reporting a skip - and then initialises the database
// exactly as cmd/remitt-server/main.go does, migrations included.
func liveDatabase(t *testing.T) *sql.DB {
	t.Helper()

	cfgPath := os.Getenv(liveConfigEnv)
	if cfgPath == "" {
		t.Skipf("set %s to a remitt-server configuration file whose database is reachable to run the end-to-end pipeline test "+
			"(e.g. %s=/tmp/remitt-verify.yml; the file needs database.*, paths.base, paths.temp and paths.db-migrations - "+
			"see config/config.go). Without it this test would need a live MySQL and a real SSH/SFTP endpoint, so it is skipped.",
			liveConfigEnv, liveConfigEnv)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("%s=%q: %v", liveConfigEnv, cfgPath, err)
	}
	cfg, err := config.LoadConfigWithDefaults(cfgPath)
	if err != nil {
		t.Fatalf("load %s: %v", cfgPath, err)
	}
	config.Config = cfg

	// The same DSN model.InitDb builds (model/db.go:28-32). The driver is
	// registered by model's own blank import, which this package already
	// pulls in.
	dsn := cfg.Database.User + ":" + cfg.Database.Pass + "@" + cfg.Database.Host + "/" + cfg.Database.Name + "?" + model.DbFlags
	probe, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open %s: %v", dsn, err)
	}
	defer probe.Close()
	if err := probe.Ping(); err != nil {
		t.Skipf("%s=%q names a database that is not reachable (%s/%s@%s): %v. Start it (this repository's is the remitt-mysql "+
			"container on 127.0.0.1:3307) and re-run with the same %s.",
			liveConfigEnv, cfgPath, cfg.Database.Name, cfg.Database.User, cfg.Database.Host, err, liveConfigEnv)
	}

	migrations := filepath.Join(cfg.Paths.BasePath, cfg.Paths.DbMigrationsPath)
	if _, err := os.Stat(migrations); err != nil {
		t.Fatalf("the configured paths.base/db-migrations (%q) does not exist: %v - model.InitDb migrates from there and would "+
			"fail the process outright", migrations, err)
	}
	model.InitDb()
	if model.SqlDb == nil || model.Queries == nil {
		t.Fatal("model.InitDb() left the database handle or the query set nil")
	}

	var version int
	var dirty bool
	if err := model.SqlDb.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	t.Logf("database %s@%s (%s): schema_migrations version=%d dirty=%v, base path %s",
		cfg.Database.Name, cfg.Database.Host, cfgPath, version, dirty, cfg.Paths.BasePath)
	return model.SqlDb
}

// ---------------------------------------------------------------------------
// the payload insert, exactly as the API does it
// ---------------------------------------------------------------------------

// liveInsertPayloadTheApiWay performs what api.PayloadInsert performs, step for
// step: the dbgen.InsertPayloadParams of api/payload.go:66-74, the LastInsertId
// of :80, and the jobqueue.EnqueuePayload call of :94 that is trigger 1 of 2.
//
// It is written out rather than called because api imports this package: the
// handler cannot be imported here without an import cycle. Every parameter and
// the order of the two database calls are the handler's, so a change to the
// insert path that this test does not follow is visible as a difference between
// this function and api/payload.go.
func liveInsertPayloadTheApiWay(t *testing.T, payload []byte, renderPlugin, renderOption, transportPlugin, originalID string) int64 {
	t.Helper()
	id := liveInsertPayloadRowOnly(t, payload, renderPlugin, renderOption, transportPlugin, originalID)

	jobID, err := EnqueuePayload(context.Background(), id)
	if err != nil {
		t.Fatalf("EnqueuePayload(%d) = %v; that is exactly what api.PayloadInsert calls (api/payload.go:94), so the insert path is broken: %s",
			id, err, liveQueueStatus(id))
	}
	t.Logf("tPayload %d inserted and enqueued as job %d; queue: %s", id, jobID, liveQueueStatus(id))
	return id
}

// liveInsertPayloadRowOnly writes the row and nothing else - the "another path"
// case: the soap adapter, an import script, a direct INSERT by an operator, or a
// row left waiting across a restart. Only the poller can pick this up.
func liveInsertPayloadRowOnly(t *testing.T, payload []byte, renderPlugin, renderOption, transportPlugin, originalID string) int64 {
	t.Helper()
	result, err := model.Queries.InsertPayload(context.Background(), dbgen.InsertPayloadParams{
		User:            liveUser,
		Payload:         sql.NullString{String: string(payload), Valid: true},
		RenderPlugin:    renderPlugin,
		RenderOption:    renderOption,
		TransportPlugin: transportPlugin,
		TransportOption: sql.NullString{},
		OriginalID:      sql.NullString{String: originalID, Valid: true},
	})
	if err != nil {
		t.Fatalf("InsertPayload(%s) = %v", originalID, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("InsertPayload(%s): LastInsertId = %v", originalID, err)
	}
	t.Logf("tPayload %d inserted (originalId %q, render %s/%s, transport %s, payload %d bytes md5 %s)",
		id, originalID, renderPlugin, renderOption, transportPlugin, len(payload), liveMD5(payload))
	return id
}

// ---------------------------------------------------------------------------
// reading what the pipeline did
// ---------------------------------------------------------------------------

// livePayloadState is the observable outcome of one payload: the two fields of
// its own row and the journal row the enqueue path wrote.
type livePayloadState struct {
	id            int64
	state         string
	processorRows int
	processorID   int64
	threadID      int64
	tsStart       sql.NullTime
	tsEnd         sql.NullTime
}

// liveReadPayload reads the payload's state and its journal row.
func liveReadPayload(t *testing.T, id int64) livePayloadState {
	t.Helper()
	row := livePayloadState{id: id}
	if err := model.SqlDb.QueryRow("SELECT COALESCE(payloadState, '') FROM tPayload WHERE id = ?", id).Scan(&row.state); err != nil {
		t.Fatalf("read tPayload %d: %v", id, err)
	}
	if err := model.SqlDb.QueryRow("SELECT COUNT(*) FROM tProcessor WHERE payloadId = ?", id).Scan(&row.processorRows); err != nil {
		t.Fatalf("count tProcessor for tPayload %d: %v", id, err)
	}
	if row.processorRows == 0 {
		return row
	}
	err := model.SqlDb.QueryRow(
		`SELECT id, threadId, tsStart, tsEnd FROM tProcessor WHERE payloadId = ? ORDER BY id DESC LIMIT 1`, id).
		Scan(&row.processorID, &row.threadID, &row.tsStart, &row.tsEnd)
	if err != nil {
		t.Fatalf("read the tProcessor row for tPayload %d: %v", id, err)
	}
	return row
}

// liveAwaitTerminal waits for the payload to stop being 'valid', which is when
// the payload state write at the end of Finish/Fail has landed (enqueue.go's
// journalTerminal writes the tProcessor tsEnd first and the payload state
// second, so a non-'valid' state means the journal is closed too).
//
// A timeout is a failure with the job's own log lines attached: the queue map
// holds the item the worker ran, so the stage that broke is named rather than
// guessed.
func liveAwaitTerminal(t *testing.T, id int64) livePayloadState {
	t.Helper()
	deadline := time.Now().Add(liveDeadline)
	last := liveReadPayload(t, id)
	for time.Now().Before(deadline) {
		if last.state != payloadStateValid {
			return last
		}
		time.Sleep(50 * time.Millisecond)
		last = liveReadPayload(t, id)
	}
	t.Fatalf("tPayload %d is still payloadState=%q after %s; the pipeline never reached a terminal state. queue: %s",
		id, last.state, liveDeadline, liveQueueStatus(id))
	return last
}

// liveAssertCompleted is the acceptance assertion for one processed payload:
// the terminal state, the journal row, and that exactly one trigger claimed it.
func liveAssertCompleted(t *testing.T, label string, row livePayloadState) {
	t.Helper()
	if row.state != payloadStateCompleted {
		t.Fatalf("%s: tPayload %d payloadState = %q; want %q. queue: %s",
			label, row.id, row.state, payloadStateCompleted, liveQueueStatus(row.id))
	}
	if row.processorRows != 1 {
		t.Errorf("%s: tPayload %d has %d tProcessor row(s); want exactly 1 - a second row means a second trigger enqueued the same payload",
			label, row.id, row.processorRows)
	}
	if row.threadID < 1 {
		t.Errorf("%s: tProcessor %d threadId = %d; want the id of the worker that ran the job (0 is the value the enqueue writes and the worker replaces)",
			label, row.processorID, row.threadID)
	}
	if !row.tsStart.Valid {
		t.Errorf("%s: tProcessor %d tsStart is NULL; the enqueue's journal INSERT sets it", label, row.processorID)
	}
	if !row.tsEnd.Valid {
		t.Errorf("%s: tProcessor %d tsEnd is NULL; the worker never closed the journal row", label, row.processorID)
	}
	t.Logf("%s: tPayload %d -> %q; tProcessor %d (threadId %d, stage render, tsStart %s, tsEnd %s)",
		label, row.id, row.state, row.processorID, row.threadID,
		row.tsStart.Time.Format(time.RFC3339), row.tsEnd.Time.Format(time.RFC3339))
}

// liveWaitForQueueToDrain waits until no queued or running job is left, so that
// cleanup never deletes a row a worker is still writing to.
func liveWaitForQueueToDrain(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		inFlight := liveInFlightJobs()
		if len(inFlight) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Errorf("jobs still in flight after %s: %v - cleanup would race with them", timeout, inFlight)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// liveInFlightJobs returns the ids of the queue entries a worker still owns.
func liveInFlightJobs() []int64 {
	jobQueueLock.RLock()
	defer jobQueueLock.RUnlock()
	var ids []int64
	for id, item := range jobQueue {
		item.ReadLock()
		status := item.Status
		item.ReadUnlock()
		if status == jobStatusMap[JobStatusQueued] || status == jobStatusMap[JobStatusRunning] {
			ids = append(ids, id)
		}
	}
	return ids
}

// liveQueueStatus renders what the in-memory queue knows about a payload's job,
// including its log lines. It is what a failure message carries so the stage
// that broke is named: the log holds the wrap chain ("executejob: render: ...",
// "executejob: reseolve..translator: ...", "executejob: transport: ...").
func liveQueueStatus(payloadID int64) string {
	jobQueueLock.RLock()
	defer jobQueueLock.RUnlock()
	for _, item := range jobQueue {
		if item.PayloadID != payloadID {
			continue
		}
		item.ReadLock()
		out := fmt.Sprintf("job %d status=%s message=%q log=%v", item.ID, item.Status, item.Message, item.Log)
		item.ReadUnlock()
		return out
	}
	return "(no job in the queue for this payload)"
}

// ---------------------------------------------------------------------------
// the SFTP server
// ---------------------------------------------------------------------------

// liveUploaded is the set of files under the harness's SFTP root, keyed by name.
func liveUploaded(t *testing.T, srv *sshsftp.Server) map[string][]byte {
	t.Helper()
	files, err := srv.UploadedFiles()
	if err != nil {
		t.Fatalf("list the SFTP root %s: %v", srv.Dir, err)
	}
	return files
}

// liveAssertOneNewFileWithContent asserts the delivery: exactly one file
// appeared on the server since before, and its content is the expected bytes
// compared byte-for-byte (md5).
//
// "Exactly one" is the strong form and it is what a double delivery would
// break. It is counted over the files whose content MATCHES, not over all new
// files, because the poll query is database-wide: a row another run left 'valid'
// may legitimately be delivered to the same server during this test, and that is
// logged rather than failed here.
func liveAssertOneNewFileWithContent(t *testing.T, srv *sshsftp.Server, before map[string][]byte, want []byte) struct {
	name    string
	content []byte
} {
	t.Helper()
	after := liveUploaded(t, srv)
	wantMD5 := liveMD5(want)
	var matches []string
	var extra []string
	for name, content := range after {
		if _, existed := before[name]; existed {
			continue
		}
		if liveMD5(content) == wantMD5 {
			matches = append(matches, name)
			continue
		}
		extra = append(extra, fmt.Sprintf("%s (%d bytes, md5 %s)", name, len(content), liveMD5(content)))
	}
	if len(matches) != 1 {
		t.Fatalf("the SFTP server got %d new file(s) with content md5 %s; want exactly 1 (new files with other content: %v)",
			len(matches), wantMD5, extra)
	}
	if len(extra) > 0 {
		t.Logf("note: %d other new file(s) also landed (%v) - other payloads the poll query also saw", len(extra), extra)
	}
	return struct {
		name    string
		content []byte
	}{name: matches[0], content: after[matches[0]]}
}

// ---------------------------------------------------------------------------
// tUserConfig: the endpoint the transport resolves for itself
// ---------------------------------------------------------------------------

// liveConfigRow is one tUserConfig row as it was before the test wrote it.
type liveConfigRow struct {
	option string
	value  string
}

// liveInstallSftpUserConfig points the SFTP transport at the harness for the
// test user, under the Java FQCN namespace the legacy seed uses
// (migrations/001_legacy.up.sql:63-67), and returns nothing: the pre-test rows
// are recorded by liveTakeWatermark, which must run first.
//
// Replace, not update: tUserConfig carries no key at all (the migration creates
// it without one, migrations/001_legacy.up.sql:53-60), so the same
// (user, namespace, option) can appear more than once - and does, after the
// 2026-09-15 runs. Updating would leave the duplicates behind, and
// transport/selfconfig.go merges them in read order, so which value won would
// depend on row order.
func liveInstallSftpUserConfig(t *testing.T, db *sql.DB, srv *sshsftp.Server) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM tUserConfig WHERE user = ? AND cNamespace = ?`, liveUser, liveTransportSftp); err != nil {
		t.Fatalf("clear the previous tUserConfig rows for %s/%s: %v", liveUser, liveTransportSftp, err)
	}
	rows := [][2]string{
		{"sftpHost", srv.Host},
		{"sftpPort", strconv.Itoa(srv.Port)},
		{"sftpUsername", srv.User},
		{"sftpPassword", srv.Password},
		{"sftpPath", srv.Dir},
	}
	for _, row := range rows {
		if _, err := db.Exec(
			`INSERT INTO tUserConfig (user, cNamespace, cOption, cValue) VALUES (?, ?, ?, ?)`,
			liveUser, liveTransportSftp, row[0], []byte(row[1])); err != nil {
			t.Fatalf("write tUserConfig %s.%s: %v", liveTransportSftp, row[0], err)
		}
	}
	// model.SetConfigValue is the application's own write path (api/config.go),
	// but it calls the stored procedure p_UserConfigUpdate, which this database
	// does not have (the 2026-09-15 runs reported "PROCEDURE
	// remitt.pUserConfigUpdate does not exist"). The rows above are the same
	// rows that procedure would have written; nothing here works around the
	// pipeline, only around a missing procedure.
	t.Logf("tUserConfig: %s/%s now points the SFTP transport at %s (dir %s)", liveUser, liveTransportSftp, srv.Address(), srv.Dir)

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tUserConfig WHERE user = ? AND cNamespace = ?`, liveUser, liveTransportSftp).Scan(&n); err != nil {
		t.Fatalf("count tUserConfig rows: %v", err)
	}
	if n != len(rows) {
		t.Fatalf("tUserConfig holds %d row(s) for %s/%s after the write; want %d", n, liveUser, liveTransportSftp, len(rows))
	}
}

// ---------------------------------------------------------------------------
// cleanup: the high-water mark
// ---------------------------------------------------------------------------

// liveWatermark is the pre-test state of everything this test can disturb.
type liveWatermark struct {
	maxPayloadID   int64
	maxProcessorID int64
	maxFileStoreID int64
	payloadStates  map[int64]string
	userConfig     []liveConfigRow
}

// liveTakeWatermark records the ids above which every row is this run's, the
// state of every payload that already exists, and the tUserConfig rows the test
// is about to replace.
func liveTakeWatermark(t *testing.T, db *sql.DB) liveWatermark {
	t.Helper()
	mark := liveWatermark{payloadStates: map[int64]string{}}
	for table, into := range map[string]*int64{
		"tPayload":   &mark.maxPayloadID,
		"tProcessor": &mark.maxProcessorID,
		"tFileStore": &mark.maxFileStoreID,
	} {
		if err := db.QueryRow("SELECT COALESCE(MAX(id), 0) FROM " + table).Scan(into); err != nil {
			t.Fatalf("high-water mark of %s: %v", table, err)
		}
	}

	rows, err := db.Query("SELECT id, COALESCE(payloadState, '') FROM tPayload")
	if err != nil {
		t.Fatalf("snapshot tPayload: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var state string
		if err := rows.Scan(&id, &state); err != nil {
			t.Fatalf("scan tPayload snapshot: %v", err)
		}
		mark.payloadStates[id] = state
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("snapshot tPayload: %v", err)
	}

	cfgRows, err := db.Query(`SELECT cOption, COALESCE(CONVERT(cValue USING utf8mb4), '') FROM tUserConfig WHERE user = ? AND cNamespace = ?`, liveUser, liveTransportSftp)
	if err != nil {
		t.Fatalf("snapshot tUserConfig: %v", err)
	}
	defer cfgRows.Close()
	for cfgRows.Next() {
		var row liveConfigRow
		if err := cfgRows.Scan(&row.option, &row.value); err != nil {
			t.Fatalf("scan tUserConfig snapshot: %v", err)
		}
		mark.userConfig = append(mark.userConfig, row)
	}
	if err := cfgRows.Err(); err != nil {
		t.Fatalf("snapshot tUserConfig: %v", err)
	}

	t.Logf("watermark: tPayload id>%d, tProcessor id>%d, tFileStore id>%d are this run's; %d pre-existing payload state(s); "+
		"%d tUserConfig row(s) under %s will be restored", mark.maxPayloadID, mark.maxProcessorID, mark.maxFileStoreID,
		len(mark.payloadStates), len(mark.userConfig), liveTransportSftp)
	return mark
}

// restore undoes everything the run did, in foreign-key order, and then PROVES
// it: the leftover counts are asserted to be zero, not merely reported.
func (m liveWatermark) restore(t *testing.T, db *sql.DB) {
	t.Helper()

	// 1. Everything this run created, leaf first.
	for _, del := range []struct {
		table string
		mark  int64
	}{
		{"tFileStore", m.maxFileStoreID},
		{"tProcessor", m.maxProcessorID},
		{"tPayload", m.maxPayloadID},
	} {
		res, err := db.Exec("DELETE FROM "+del.table+" WHERE id > ?", del.mark)
		if err != nil {
			t.Errorf("cleanup %s: %v", del.table, err)
			continue
		}
		n, _ := res.RowsAffected()
		t.Logf("cleanup: deleted %d %s row(s) above id %d", n, del.table, del.mark)
	}

	// 2. The rows that already existed. The poll query is database-wide, so a
	// payload another run left 'valid' is enqueued and its state moves; put
	// back exactly what was recorded.
	restored := 0
	for id, was := range m.payloadStates {
		var now string
		if err := db.QueryRow("SELECT COALESCE(payloadState, '') FROM tPayload WHERE id = ?", id).Scan(&now); err != nil {
			if err == sql.ErrNoRows {
				continue // the row was this run's and has been deleted
			}
			t.Errorf("cleanup: read tPayload %d: %v", id, err)
			continue
		}
		if now == was {
			continue
		}
		if _, err := db.Exec("UPDATE tPayload SET payloadState = ? WHERE id = ?", was, id); err != nil {
			t.Errorf("cleanup: restore tPayload %d to payloadState %q: %v", id, was, err)
			continue
		}
		t.Logf("cleanup: restored tPayload %d payloadState %q -> %q (a row that pre-dated this run and the poller also picked up)", id, now, was)
		restored++
	}
	if restored > 0 {
		t.Logf("cleanup: restored %d pre-existing payload state(s) the poller's database-wide query also reached", restored)
	}

	// 3. tUserConfig, back to the exact rows that were there.
	if _, err := db.Exec(`DELETE FROM tUserConfig WHERE user = ? AND cNamespace = ?`, liveUser, liveTransportSftp); err != nil {
		t.Errorf("cleanup: clear tUserConfig %s/%s: %v", liveUser, liveTransportSftp, err)
	}
	for _, row := range m.userConfig {
		if _, err := db.Exec(`INSERT INTO tUserConfig (user, cNamespace, cOption, cValue) VALUES (?, ?, ?, ?)`,
			liveUser, liveTransportSftp, row.option, []byte(row.value)); err != nil {
			t.Errorf("cleanup: restore tUserConfig %s.%s: %v", liveTransportSftp, row.option, err)
		}
	}
	t.Logf("cleanup: rewrote the %d pre-test tUserConfig row(s) for %s/%s", len(m.userConfig), liveUser, liveTransportSftp)

	// 4. Prove it.
	for _, check := range []struct {
		table string
		mark  int64
	}{
		{"tFileStore", m.maxFileStoreID},
		{"tProcessor", m.maxProcessorID},
		{"tPayload", m.maxPayloadID},
	} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM "+check.table+" WHERE id > ?", check.mark).Scan(&n); err != nil {
			t.Errorf("cleanup check %s: %v", check.table, err)
			continue
		}
		if n != 0 {
			t.Errorf("cleanup left %d %s row(s) above id %d; the run did not clean up after itself", n, check.table, check.mark)
		}
	}
	var cfgRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tUserConfig WHERE user = ? AND cNamespace = ?`, liveUser, liveTransportSftp).Scan(&cfgRows); err != nil {
		t.Errorf("cleanup check tUserConfig: %v", err)
	} else if cfgRows != len(m.userConfig) {
		t.Errorf("cleanup left %d tUserConfig row(s) for %s/%s; want the %d that were there before", cfgRows, liveUser, liveTransportSftp, len(m.userConfig))
	}
	for id, was := range m.payloadStates {
		var now string
		if err := db.QueryRow("SELECT COALESCE(payloadState, '') FROM tPayload WHERE id = ?", id).Scan(&now); err == nil && now != was {
			t.Errorf("cleanup left tPayload %d at payloadState %q; want the pre-test %q", id, now, was)
		}
	}
	t.Logf("cleanup: verified - no tPayload/tProcessor/tFileStore row above the watermark (%d/%d/%d), tUserConfig back to %d row(s), "+
		"every pre-existing payload state restored. The SFTP root is a temporary directory the harness removes itself.",
		m.maxPayloadID, m.maxProcessorID, m.maxFileStoreID, len(m.userConfig))
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

// liveMD5 is the content comparison used for both the file on the SFTP server
// and the tFileStore row.
func liveMD5(b []byte) string {
	sum := md5.Sum(b)
	return hex.EncodeToString(sum[:])
}

// liveFirst renders at most n bytes for a log line.
func liveFirst(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}

// liveStatementInput is the input the shipped statement stylesheet is written
// for: every stylesheet in resources/xsl matches on a <remitt> root, and the
// values are the ones in test/testdata/fixedform_simple.xml. It exists so a real
// stylesheet can be shown to produce a real render; it is not a repository
// fixture and nothing else reads it.
var liveStatementInput = []byte(`<?xml version="1.0" encoding="UTF-8"?>
<remitt>
	<global>
		<currentdate><year>2026</year><month>09</month><day>15</day></currentdate>
	</global>
	<patient id="1">
		<name><first>JOHN Q</first><last>DOE</last></name>
		<address>
			<streetaddress>123 TEST STREET</streetaddress>
			<city>HARTFORD</city>
			<state>CT</state>
			<zipcode>06103</zipcode>
		</address>
		<account>CLM-2024-001234</account>
	</patient>
	<diagnosis id="1"><icd9code>J45.909</icd9code></diagnosis>
	<procedure patientkey="1">
		<diagnosiskey>1</diagnosiskey>
		<dateofservicestart><year>2026</year><month>08</month><day>01</day></dateofservicestart>
		<cpt4code>99213</cpt4code>
		<cptdescription>OFFICE VISIT ESTABLISHED</cptdescription>
		<cptcharges>150.00</cptcharges>
		<amountpaid>100.00</amountpaid>
	</procedure>
</remitt>
`)
