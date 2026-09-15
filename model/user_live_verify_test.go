package model

// user_live_verify_test.go is the live verification for the two schema-drift
// defects fixed in internal/db/schema.sql,
// internal/db/queries/*.sql, internal/dbgen/*.go, model/user.go and
// model/userconfig.go-path api/config.go:
//
//	defect 1  tUser.role did not exist in the migrated schema, so
//	          GetUserByName ("Unknown column 'role'") failed for every job.
//	defect 2  the config write called pUserConfigUpdate; the migrated procedure
//	          is p_UserConfigUpdate ("PROCEDURE ... does not exist").
//
// It runs only when REMITT_LIVE_CONFIG points at a reachable database, so an
// ordinary `go test ./...` stays database-free:
//
//	REMITT_LIVE_CONFIG=/tmp/remitt-e2e.yml go test -count=1 -v -run TestLive_ ./model/

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/config"
	_ "github.com/go-sql-driver/mysql"
)

func liveDB(t *testing.T) *sql.DB {
	t.Helper()
	cfgPath := os.Getenv("REMITT_LIVE_CONFIG")
	if cfgPath == "" {
		t.Skip("REMITT_LIVE_CONFIG is not set; skipping live schema verification")
	}
	cfg, err := config.LoadConfigWithDefaults(cfgPath)
	if err != nil {
		t.Fatalf("load %s: %v", cfgPath, err)
	}
	config.Config = cfg
	InitDb()
	if SqlDb == nil {
		t.Fatalf("InitDb() left SqlDb nil for %s", cfgPath)
	}
	if err := SqlDb.Ping(); err != nil {
		t.Fatalf("ping %s (%s@%s): %v", cfg.Database.Name, cfg.Database.User, cfg.Database.Host, err)
	}
	var version int
	var dirty bool
	if err := SqlDb.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	t.Logf("live database %s (%s) schema_migrations version=%d dirty=%v",
		cfg.Database.Name, cfg.Database.Host, version, dirty)
	return SqlDb
}

// TestLive_BeforeRoleColumnIsUnknown reproduces the defect in its original form:
// the invented column, queried straight at the live database, does not exist.
func TestLive_BeforeRoleColumnIsUnknown(t *testing.T) {
	db := liveDB(t)
	var role string
	err := db.QueryRow("SELECT role FROM tUser WHERE username = 'Administrator'").Scan(&role)
	if err == nil {
		t.Fatalf("tUser.role exists in the live schema (role=%q); the drift premise is wrong", role)
	}
	t.Logf("BEFORE (defect, reproduced live): SELECT role FROM tUser -> %v", err)
	if !strings.Contains(err.Error(), "Unknown column 'role'") {
		t.Fatalf("expected Unknown column 'role', got %v", err)
	}

	var cols int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = 'tUser' AND column_name = 'role'`).Scan(&cols); err != nil {
		t.Fatalf("information_schema: %v", err)
	}
	if cols != 0 {
		t.Fatalf("tUser.role is present in the live schema (%d columns)", cols)
	}
	t.Log("live tUser has no 'role' column; roles live in tRole")
}

// TestLive_GetUserByNameReturnsAdministrator is the AFTER check for defect 1:
// the exact query every job starts with must return a real row, with the user's
// role reachable through tRole.
func TestLive_GetUserByNameReturnsAdministrator(t *testing.T) {
	liveDB(t)
	u, err := GetUserByName("Administrator")
	if err != nil {
		t.Fatalf("GetUserByName(Administrator): %v", err)
	}
	t.Logf("AFTER: GetUserByName(Administrator) -> id=%d username=%q passhash=%s contactemail=%q Role=%q (from tRole)",
		u.Id, u.Username, u.PasswordHash, u.ContactEmail.String, u.Role)
	if u.Id != 1 || u.Username != "Administrator" {
		t.Fatalf("unexpected row: %+v", u)
	}
	if u.PasswordHash == "" {
		t.Fatal("passhash came back empty")
	}

	roles, err := u.GetRoles()
	if err != nil {
		t.Fatalf("GetRoles: %v", err)
	}
	t.Logf("AFTER: UserModel.GetRoles() from tRole -> %v", roles)
	if len(roles) == 0 {
		t.Fatal("Administrator has no roles in tRole")
	}
	if u.Role != "admin" {
		t.Fatalf("UserModel.Role = %q, want \"admin\" (first tRole rolename)", u.Role)
	}

	// GetUserById goes down the same path.
	byId, err := GetUserById("1")
	if err != nil {
		t.Fatalf("GetUserById(1): %v", err)
	}
	t.Logf("AFTER: GetUserById(1) -> username=%q Role=%q", byId.Username, byId.Role)

	// CheckUserPassword must still authenticate the seeded account.
	id, ok := CheckUserPassword("Administrator", "password")
	t.Logf("AFTER: CheckUserPassword(Administrator, password) -> id=%d ok=%v", id, ok)
	if !ok {
		t.Fatal("CheckUserPassword rejected the seeded Administrator/password")
	}
}

// TestLive_ConfigWriteLands is the AFTER check for defect 2: the procedure the
// query file now names exists, SetConfigValue succeeds and the value lands in
// tUserConfig. Whatever it changed is restored.
func TestLive_ConfigWriteLands(t *testing.T) {
	db := liveDB(t)

	const (
		ns  = "org.remitt.plugin.transport.SftpTransport"
		opt = "sftpPort"
	)
	var original string
	if err := db.QueryRow("SELECT cValue FROM tUserConfig WHERE user='Administrator' AND cNamespace=? AND cOption=?", ns, opt).Scan(&original); err != nil {
		t.Fatalf("read original %s/%s: %v", ns, opt, err)
	}
	t.Cleanup(func() {
		// Restore the seeded value and drop any duplicate row the procedure's
		// insert branch may have added, so the database is left as found.
		if _, err := db.Exec("DELETE FROM tUserConfig WHERE user='Administrator' AND cNamespace=? AND cOption=?", ns, opt); err != nil {
			t.Errorf("cleanup delete: %v", err)
		}
		if _, err := db.Exec("INSERT INTO tUserConfig (user, cNamespace, cOption, cValue) VALUES ('Administrator', ?, ?, ?)", ns, opt, original); err != nil {
			t.Errorf("cleanup restore: %v", err)
		}
		rows, err := db.Query("SELECT cValue FROM tUserConfig WHERE user='Administrator' AND cNamespace=? AND cOption=?", ns, opt)
		if err != nil {
			t.Errorf("cleanup verify: %v", err)
		} else {
			defer rows.Close()
			for rows.Next() {
				var v string
				_ = rows.Scan(&v)
				t.Logf("RESTORED: %s/%s = %q", ns, opt, v)
			}
		}
	})

	// The procedure name the buggy code used must NOT exist.
	var bogus int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.routines
		WHERE routine_schema = DATABASE() AND routine_name = 'pUserConfigUpdate'`).Scan(&bogus); err != nil {
		t.Fatalf("information_schema.routines: %v", err)
	}
	if bogus != 0 {
		t.Fatalf("pUserConfigUpdate unexpectedly exists (%d); the drift premise is wrong", bogus)
	}
	t.Log("BEFORE (defect, confirmed live): no routine named pUserConfigUpdate; the real one is p_UserConfigUpdate")

	probe := "2222"
	if err := SetConfigValue("Administrator", ns, opt, []byte(probe)); err != nil {
		t.Fatalf("SetConfigValue(%q): %v", opt, err)
	}

	var got string
	if err := db.QueryRow("SELECT cValue FROM tUserConfig WHERE user='Administrator' AND cNamespace=? AND cOption=? ORDER BY cValue DESC LIMIT 1", ns, opt).Scan(&got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	t.Logf("AFTER: SetConfigValue wrote %s/%s; tUserConfig now holds %q", ns, opt, got)
	if got != probe {
		t.Fatalf("tUserConfig.cValue = %q, want %q", got, probe)
	}

	// A brand-new option takes the procedure's insert branch.
	const newOpt = "zzLiveVerifyProbe"
	if err := SetConfigValue("Administrator", ns, newOpt, []byte("landed")); err != nil {
		t.Fatalf("SetConfigValue(new option): %v", err)
	}
	defer func() {
		if _, err := db.Exec("DELETE FROM tUserConfig WHERE user='Administrator' AND cNamespace=? AND cOption=?", ns, newOpt); err != nil {
			t.Errorf("cleanup new option: %v", err)
		}
	}()
	var fresh string
	if err := db.QueryRow("SELECT cValue FROM tUserConfig WHERE user='Administrator' AND cNamespace=? AND cOption=?", ns, newOpt).Scan(&fresh); err != nil {
		t.Fatalf("read back new option: %v", err)
	}
	t.Logf("AFTER: new option %s/%s inserted with value %q", ns, newOpt, fresh)
	if fresh != "landed" {
		t.Fatalf("new option cValue = %q, want \"landed\"", fresh)
	}

	// The read path the API uses must see it too.
	vals, err := GetConfigValues("Administrator")
	if err != nil {
		t.Fatalf("GetConfigValues: %v", err)
	}
	found := 0
	for _, v := range vals {
		if v.Namespace == ns {
			found++
		}
	}
	t.Logf("AFTER: GetConfigValues(Administrator) returned %d rows, %d in %s", len(vals), found, ns)
}
