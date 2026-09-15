package scooper

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/freemed/remitt-server/model"
)

// ---------------------------------------------------------------------------
// In-memory stand-in for the MySQL handle the package reaches through the
// model.SqlDb package variable.
//
// This follows the fake database/sql driver pattern already used by
// task/scheduler_test.go. It deliberately answers only the three statements
// the scooper package issues, matched by substring:
//
//	"... FROM tScooper ..."  -> previously scooped files (sftp.go:43)
//	"... FROM tKeyring ..."  -> GatewayEDI private key (gatewayedi.go:34)
//	"INSERT INTO tScooper"   -> recorded scoops (sftp.go:133)
//
// Any other statement is an error, so a newly added query cannot silently
// return an empty result set and pass a test that should have failed.
// ---------------------------------------------------------------------------

var (
	fakeScoopedColumns = []string{"id", "scooperClass", "user", "stamp", "host", "path", "filename", "content"}
	fakeKeyringColumns = []string{"id", "user", "keyname", "privatekey", "publickey"}
)

// scooperFakeDB is a hermetic replacement for the tScooper / tKeyring tables.
type scooperFakeDB struct {
	mu sync.Mutex

	scoopedQueries int
	scoopedArgs    []any
	scoopedRows    [][]driver.Value
	scoopedErr     error

	keyringQueries int
	keyringArgs    []any
	keyringRows    [][]driver.Value
	keyringErr     error

	insertQueries int
	insertArgs    []any
	insertErr     error
}

// fakeScooperRecord builds a tScooper row in the column order the
// previously-scooped query in sftp.go:54 scans.
func fakeScooperRecord(id int64, class, user string, stamp time.Time, host, path, filename string, content []byte) []driver.Value {
	return []driver.Value{id, class, user, stamp, host, path, filename, content}
}

// fakeKeyringRecord builds a tKeyring row in the column order gatewayedi.go:37
// scans.
func fakeKeyringRecord(id int64, user, keyname string, privateKey, publicKey []byte) []driver.Value {
	return []driver.Value{id, user, keyname, privateKey, publicKey}
}

func (f *scooperFakeDB) setScoopedRows(rows ...[]driver.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scoopedRows = rows
}

func (f *scooperFakeDB) setScoopedQueryError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scoopedErr = err
}

func (f *scooperFakeDB) setKeyringRows(rows ...[]driver.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyringRows = rows
}

func (f *scooperFakeDB) setKeyringQueryError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keyringErr = err
}

func (f *scooperFakeDB) setInsertError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.insertErr = err
}

func (f *scooperFakeDB) scoopedQueryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scoopedQueries
}

func (f *scooperFakeDB) scoopedQueryArgs() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.scoopedArgs...)
}

func (f *scooperFakeDB) keyringQueryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keyringQueries
}

func (f *scooperFakeDB) keyringQueryArgs() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.keyringArgs...)
}

func (f *scooperFakeDB) insertCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.insertQueries
}

// insertedArgs returns the arguments of the most recent tScooper insert:
// (scooperClass, user, stamp, host, path, filename, content).
func (f *scooperFakeDB) insertedArgs() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.insertArgs...)
}

func (f *scooperFakeDB) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case strings.Contains(query, "tKeyring"):
		f.keyringQueries++
		f.keyringArgs = namedValueValues(args)
		if f.keyringErr != nil {
			return nil, f.keyringErr
		}
		return &scooperFakeRows{cols: fakeKeyringColumns, rows: f.keyringRows}, nil
	case strings.Contains(query, "tScooper"):
		f.scoopedQueries++
		f.scoopedArgs = namedValueValues(args)
		if f.scoopedErr != nil {
			return nil, f.scoopedErr
		}
		return &scooperFakeRows{cols: fakeScoopedColumns, rows: f.scoopedRows}, nil
	default:
		return nil, fmt.Errorf("scooperFakeDB: unexpected query %q", query)
	}
}

func (f *scooperFakeDB) exec(query string, args []driver.NamedValue) (driver.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case strings.Contains(query, "INSERT INTO tScooper"):
		f.insertQueries++
		f.insertArgs = namedValueValues(args)
		if f.insertErr != nil {
			return nil, f.insertErr
		}
		return fakeScooperResult(1), nil
	default:
		return nil, fmt.Errorf("scooperFakeDB: unexpected statement %q", query)
	}
}

func namedValueValues(args []driver.NamedValue) []any {
	if args == nil {
		return nil
	}
	out := make([]any, 0, len(args))
	for _, a := range args {
		out = append(out, a.Value)
	}
	return out
}

type fakeScooperResult int64

func (r fakeScooperResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r fakeScooperResult) RowsAffected() (int64, error) { return 1, nil }

var scooperFakeDriverSeq atomic.Int64

type scooperFakeDriver struct{ db *scooperFakeDB }

func (d *scooperFakeDriver) Open(string) (driver.Conn, error) {
	return &scooperFakeConn{db: d.db}, nil
}

type scooperFakeConn struct{ db *scooperFakeDB }

func (c *scooperFakeConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *scooperFakeConn) Close() error                        { return nil }
func (c *scooperFakeConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (c *scooperFakeConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.db.query(query, args)
}

func (c *scooperFakeConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.db.exec(query, args)
}

type scooperFakeRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func (r *scooperFakeRows) Columns() []string { return r.cols }
func (r *scooperFakeRows) Close() error      { return nil }

func (r *scooperFakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	if len(row) != len(dest) {
		return fmt.Errorf("scooperFakeDB: row has %d values but %d columns were declared", len(row), len(dest))
	}
	copy(dest, row)
	r.idx++
	return nil
}

// installFakeScooperDB points model.SqlDb at a hermetic in-memory database and
// restores the previous handle when the test ends.
func installFakeScooperDB(t *testing.T) *scooperFakeDB {
	t.Helper()

	fake := &scooperFakeDB{}
	name := fmt.Sprintf("remitt-scooper-fake-%d", scooperFakeDriverSeq.Add(1))
	sql.Register(name, &scooperFakeDriver{db: fake})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", name, err)
	}
	db.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = db.Close() })

	saved := model.SqlDb
	model.SqlDb = db
	t.Cleanup(func() { model.SqlDb = saved })

	return fake
}

// withNilSqlDb simulates a process where model.InitDb never ran. It makes
// accidental database access fail loudly instead of silently succeeding.
func withNilSqlDb(t *testing.T) {
	t.Helper()

	saved := model.SqlDb
	model.SqlDb = nil
	t.Cleanup(func() { model.SqlDb = saved })
}
