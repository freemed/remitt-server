package transport

// storefile_live_test.go is the LIVE verification for the tFileStore identity
// fix (storefile.go, storefilepdf.go, common/jobcontext.go): it writes to a real
// MySQL, in a real tPayload/tProcessor/tFileStore, and reads the row back.
//
// It is skipped unless REMITT_LIVE_CONFIG points at a remitt-server
// configuration file whose database is reachable, so an ordinary `go test ./...`
// never touches a database:
//
//	REMITT_LIVE_CONFIG=/tmp/remitt-e2e.yml go test -count=1 -v \
//	    -run TestLive_StoreFileWritesItsPayloadIdentity ./...
//
// What it establishes, in order, against the live database:
//
//  1. BEFORE: an insert with the identity the plugins used to write
//     (payloadId 0, processorId 0) is rejected by the foreign key - the live
//     reproduction of error 1452, both through the plugin's own query path and
//     as raw SQL.
//  2. BEFORE: the plugin with no identity in its context fails with the reason
//     and writes NO row (the fix for the defect is a loud failure, not a
//     silently invalid row).
//  3. AFTER: with the job's real tPayload.id and tProcessor.id in the context,
//     the plugin writes the row and the row's payloadId/processorId are the ids
//     that were carried.
//  4. Everything it created is removed again (tFileStore, tProcessor, tPayload)
//     - the test leaves the database as it found it and says so.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

// TestLive_StoreFileWritesItsPayloadIdentity is the acceptance test for the
// second defect of the 2026-09-15 end-to-end run: "tFileStore writes violate the
// payload foreign key".
func TestLive_StoreFileWritesItsPayloadIdentity(t *testing.T) {
	db := liveDatabase(t)

	// The database's own record of which payload/tProcessor rows may be
	// referenced. Administrator is the user the legacy seed creates
	// (migrations/001_legacy.up.sql) and the one the SFTP run used.
	const liveUser = "Administrator"
	var userID int64
	if err := db.QueryRow(`SELECT id FROM tUser WHERE username = ?`, liveUser).Scan(&userID); err != nil {
		t.Fatalf("live database has no user %q (tPayload.user is a foreign key to tUser.username): %v", liveUser, err)
	}
	t.Logf("live database: user %q has id %d", liveUser, userID)

	plugins := []struct {
		name      string
		plugin    func() Transporter
		extension string
		payload   string
	}{
		{"storefile", func() Transporter { return &StoreFile{} }, "x12",
			"ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER       *240101*1200*^*00501*000000001*0*P*:~"},
		{"storefilepdf", func() Transporter { return &StoreFilePdf{} }, "pdf",
			"%PDF-1.7\n... a real storefilepdf output ...\n%%EOF"},
	}

	for _, p := range plugins {
		t.Run(p.name, func(t *testing.T) {
			// ------------------------------------------------ live fixtures
			// A real tPayload row, written through the application's own
			// query layer, and a real tProcessor row for the transport stage
			// - the row the Java ControlThread.migratePayloadToProcessor
			// wrote (and the feeder is being built to write) and the only
			// valid referent for tFileStore.processorId.
			payloadID := liveInsertPayload(t, db, liveUser, p.name)
			processorID := liveInsertProcessor(t, db, payloadID, p.name)
			t.Logf("live fixtures: tPayload.id=%d, tProcessor.id=%d (payloadId=%d, stage=transport, plugin=%s)",
				payloadID, processorID, payloadID, p.name)

			// ------------------------------------------------- 1. BEFORE
			// The identity the plugins wrote before the fix: zeros. MySQL
			// rejects the row (tFileStore_ibfk_1 for payloadId, ibfk_2 for
			// processorId), which is why every storefile job failed.
			zeroErr := insertFileStoreZeros(t, db, liveUser, p.name+"-zero.txt")
			t.Logf("BEFORE (payloadId=0, processorId=0): %v", zeroErr)
			if zeroErr == nil {
				t.Fatalf("BEFORE: an insert with payloadId=0/processorId=0 succeeded; the database no longer proves the defect")
			}
			if !strings.Contains(zeroErr.Error(), "1452") {
				t.Errorf("BEFORE: error = %v; want the foreign-key rejection (1452) the live run reported", zeroErr)
			}

			// A real payload id with the processor id still 0 isolates the
			// second key: fixing payloadId alone is not enough.
			halfErr := insertFileStoreHalf(t, db, liveUser, p.name+"-half.txt", payloadID)
			t.Logf("BEFORE (payloadId=%d, processorId=0): %v", payloadID, halfErr)
			if halfErr == nil {
				t.Errorf("BEFORE: an insert with processorId=0 succeeded; tFileStore_ibfk_2 was not enforced")
			} else if !strings.Contains(halfErr.Error(), "ibfk_2") {
				t.Errorf("BEFORE: error = %v; want it to be tFileStore_ibfk_2 (processorId -> tProcessor(id))", halfErr)
			}

			// --------------------------------- 2. the plugin without identity
			filename := fmt.Sprintf("%d-live-%s.%s", time.Now().UnixNano(), p.name, p.extension)
			plugin := p.plugin()
			if err := plugin.SetContext(ctxWithUser(liveUser)); err != nil { // user only, as jobqueue attaches it today
				t.Fatal(err)
			}
			err := plugin.Transport(filename, p.payload)
			t.Logf("plugin with a user-only context: %v", err)
			if err == nil {
				t.Fatalf("Transport() without a job identity returned a nil error; want a clear failure instead of an invalid row")
			}
			if !strings.Contains(err.Error(), p.name+": no job identity in context") {
				t.Errorf("Transport() = %q; want %q", err.Error(), p.name+": no job identity in context")
			}
			if got := liveCountFileStore(t, db, liveUser, filename); got != 0 {
				t.Fatalf("Transport() without a job identity left %d rows in tFileStore; want 0", got)
			}

			// -------------------------------------------------- 3. AFTER
			// The same plugin, with the identity the pipeline must attach.
			identity := common.JobIdentity{PayloadID: uint64(payloadID), ProcessorID: uint64(processorID), JobID: 1}
			plugin = p.plugin()
			ctx := common.NewJobContext(user.NewContext(context.Background(), &model.UserModel{Username: liveUser, Id: userID}), identity)
			if err := plugin.SetContext(ctx); err != nil {
				t.Fatal(err)
			}
			if err := plugin.Transport(filename, p.payload); err != nil {
				t.Fatalf("AFTER: Transport() with the job identity = %v; want nil", err)
			}

			row := liveReadFileStore(t, db, liveUser, filename)
			t.Logf("AFTER: tFileStore row id=%d user=%s category=%s filename=%s payloadId=%d processorId=%d contentsize=%d",
				row.id, row.user, row.category, row.filename, row.payloadID, row.processorID, row.contentsize)
			if row.payloadID != payloadID {
				t.Errorf("AFTER: row payloadId = %d; want %d (the tPayload.id carried in the context)", row.payloadID, payloadID)
			}
			if row.processorID != processorID {
				t.Errorf("AFTER: row processorId = %d; want %d (the tProcessor.id carried in the context)", row.processorID, processorID)
			}
			if row.category != "output" {
				t.Errorf("AFTER: row category = %q; want %q (the Java literal)", row.category, "output")
			}
			if row.contentsize != int64(len(p.payload)) {
				t.Errorf("AFTER: row contentsize = %d; want %d", row.contentsize, len(p.payload))
			}
		})
	}

	// ---------------------------------------------------------- 4. cleanup
	liveCleanup(t, db)
	t.Log("cleanup: tFileStore, tProcessor and tPayload rows created by this test were removed")
	liveReportLeftovers(t, db)
}

// ---------------------------------------------------------------------------
// live database plumbing
// ---------------------------------------------------------------------------

// liveDatabase loads REMITT_LIVE_CONFIG, initialises the application's own
// database handle (model.InitDb, exactly as cmd/remitt-server/main.go and the
// e2e driver do) and returns it. Without the variable the test is skipped.
func liveDatabase(t *testing.T) *sql.DB {
	t.Helper()
	cfgPath := os.Getenv("REMITT_LIVE_CONFIG")
	if cfgPath == "" {
		t.Skip("REMITT_LIVE_CONFIG is not set; skipping the live tFileStore verification (its evidence needs a real MySQL)")
	}
	cfg, err := config.LoadConfigWithDefaults(cfgPath)
	if err != nil {
		t.Fatalf("load %s: %v", cfgPath, err)
	}
	config.Config = cfg
	model.InitDb()
	if model.SqlDb == nil {
		t.Fatalf("model.InitDb() left model.SqlDb nil for %s", cfgPath)
	}
	if err := model.SqlDb.Ping(); err != nil {
		t.Fatalf("ping %s (%s@%s): %v", cfg.Database.Name, cfg.Database.User, cfg.Database.Host, err)
	}

	var version int
	var dirty bool
	if err := model.SqlDb.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	host, _ := os.Hostname()
	t.Logf("live database %s on %s (%s) schema_migrations version=%d dirty=%v",
		cfg.Database.Name, cfg.Database.Host, host, version, dirty)
	return model.SqlDb
}

// liveFixtures records what a run created so cleanup can remove exactly that.
var (
	livePayloadIDs   []int64
	liveProcessorIDs []int64
	liveFileNames    []string
)

func liveInsertPayload(t *testing.T, db *sql.DB, username, transportPlugin string) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO tPayload (user, payload, renderPlugin, renderOption, transportPlugin, payloadState) VALUES (?, ?, ?, ?, ?, 'valid')`,
		username, "live-verification", "org.remitt.plugin.render.XsltPlugin", "cms1500", transportPlugin)
	if err != nil {
		t.Fatalf("insert tPayload: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("tPayload id: %v", err)
	}
	livePayloadIDs = append(livePayloadIDs, id)
	return id
}

func liveInsertProcessor(t *testing.T, db *sql.DB, payloadID int64, plugin string) int64 {
	t.Helper()
	res, err := db.Exec(
		`INSERT INTO tProcessor (threadId, payloadId, stage, plugin) VALUES (?, ?, 'transport', ?)`,
		0, payloadID, plugin)
	if err != nil {
		t.Fatalf("insert tProcessor: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("tProcessor id: %v", err)
	}
	liveProcessorIDs = append(liveProcessorIDs, id)
	return id
}

// insertFileStoreZeros writes the row the plugin used to write (the identity is
// hardcoded 0), through the same query the plugin uses. It must fail: that is
// the defect, reproduced live.
func insertFileStoreZeros(t *testing.T, db *sql.DB, username, filename string) error {
	t.Helper()
	liveFileNames = append(liveFileNames, filename)
	_, err := db.Exec(
		`INSERT INTO tFileStore (user, stamp, category, filename, payloadId, processorId, content, contentsize) VALUES (?, NOW(), 'output', ?, 0, 0, 'x', 1)`,
		username, filename)
	return err
}

// insertFileStoreHalf isolates the second foreign key: a real payload id with
// the processor id still 0.
func insertFileStoreHalf(t *testing.T, db *sql.DB, username, filename string, payloadID int64) error {
	t.Helper()
	liveFileNames = append(liveFileNames, filename)
	_, err := db.Exec(
		`INSERT INTO tFileStore (user, stamp, category, filename, payloadId, processorId, content, contentsize) VALUES (?, NOW(), 'output', ?, ?, 0, 'x', 1)`,
		username, filename, payloadID)
	return err
}

type liveFileStoreRow struct {
	id          int64
	user        string
	category    string
	filename    string
	payloadID   int64
	processorID int64
	contentsize int64
}

func liveReadFileStore(t *testing.T, db *sql.DB, username, filename string) liveFileStoreRow {
	t.Helper()
	var row liveFileStoreRow
	err := db.QueryRow(
		`SELECT id, user, category, filename, payloadId, processorId, contentsize FROM tFileStore WHERE user = ? AND filename = ?`,
		username, filename).
		Scan(&row.id, &row.user, &row.category, &row.filename, &row.payloadID, &row.processorID, &row.contentsize)
	if err != nil {
		t.Fatalf("read back tFileStore row (user=%s filename=%s): %v", username, filename, err)
	}
	return row
}

func liveCountFileStore(t *testing.T, db *sql.DB, username, filename string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tFileStore WHERE user = ? AND filename = ?`, username, filename).Scan(&n); err != nil {
		t.Fatalf("count tFileStore rows: %v", err)
	}
	return n
}

// liveCleanup removes every row this test created, in foreign-key order.
func liveCleanup(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, name := range liveFileNames {
		if _, err := db.Exec(`DELETE FROM tFileStore WHERE filename = ?`, name); err != nil {
			t.Errorf("cleanup tFileStore %q: %v", name, err)
		}
	}
	for _, id := range liveProcessorIDs {
		if _, err := db.Exec(`DELETE FROM tProcessor WHERE id = ?`, id); err != nil {
			t.Errorf("cleanup tProcessor %d: %v", id, err)
		}
	}
	for _, id := range livePayloadIDs {
		if _, err := db.Exec(`DELETE FROM tPayload WHERE id = ?`, id); err != nil {
			t.Errorf("cleanup tPayload %d: %v", id, err)
		}
	}
}

// liveReportLeftovers proves the cleanup rather than asserting it: the count of
// rows this test's fixtures would have left behind.
func liveReportLeftovers(t *testing.T, db *sql.DB) {
	t.Helper()
	var rows, processors, payloads int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tFileStore WHERE filename LIKE '%-live-%' OR filename LIKE '%-zero.txt' OR filename LIKE '%-half.txt'`).Scan(&rows); err != nil {
		t.Errorf("leftover tFileStore check: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM tProcessor WHERE tsStart IS NULL AND plugin IN ('storefile','storefilepdf')`).Scan(&processors); err != nil {
		t.Errorf("leftover tProcessor check: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM tPayload WHERE payload = 'live-verification'`).Scan(&payloads); err != nil {
		t.Errorf("leftover tPayload check: %v", err)
	}
	t.Logf("cleanup check: leftover tFileStore rows=%d, tProcessor rows=%d, tPayload rows=%d (all 0 means the test left nothing behind)",
		rows, processors, payloads)
	if rows != 0 || processors != 0 || payloads != 0 {
		t.Errorf("cleanup left rows behind: tFileStore=%d tProcessor=%d tPayload=%d", rows, processors, payloads)
	}
}
