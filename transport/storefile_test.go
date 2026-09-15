package transport

// storefile_test.go pins the storefile transport contract
// (storefile.go): the payload is persisted into tFileStore through
// model.Queries with the category, filename, content and content size the
// legacy Java plugin promises (DbFileStore.putFile(userName, "output",
// tempPathName, input, jobId)).
//
// This file also hosts the tiny in-test database stub shared by
// storefilepdf_test.go and script_mail_test.go. The stub is a minimal
// database/sql driver that records the SQL and arguments it is handed and
// returns canned rows; no real database is contacted, and nothing outside this
// test file is modified.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

// ---------------------------------------------------------------------------
// Minimal stub database/sql driver (test-only)
// ---------------------------------------------------------------------------

type stubCall struct {
	Query string
	Args  []driver.Value
}

type stubDB struct {
	mu       sync.Mutex
	execs    []stubCall
	queries  []stubCall
	execErr  error
	queryErr error
	columns  []string
	rows     [][]driver.Value
}

func (d *stubDB) recordExec(query string, args []driver.Value) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.execs = append(d.execs, stubCall{Query: query, Args: args})
	return d.execErr
}

func (d *stubDB) recordQuery(query string, args []driver.Value) ([]string, [][]driver.Value, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queries = append(d.queries, stubCall{Query: query, Args: args})
	return d.columns, d.rows, d.queryErr
}

func (d *stubDB) execCalls() []stubCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]stubCall(nil), d.execs...)
}

func (d *stubDB) queryCalls() []stubCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]stubCall(nil), d.queries...)
}

type stubDriver struct{ db *stubDB }

func (d stubDriver) Open(string) (driver.Conn, error) { return stubConn{db: d.db}, nil }

type stubConn struct{ db *stubDB }

func (c stubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("stubConn: Prepare not implemented")
}
func (c stubConn) Close() error              { return nil }
func (c stubConn) Begin() (driver.Tx, error) { return nil, errors.New("stubConn: Begin not implemented") }

func namedValuesToValues(args []driver.NamedValue) []driver.Value {
	vals := make([]driver.Value, 0, len(args))
	for _, a := range args {
		vals = append(vals, a.Value)
	}
	return vals
}

func (c stubConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := c.db.recordExec(query, namedValuesToValues(args)); err != nil {
		return nil, err
	}
	return stubResult(1), nil
}

func (c stubConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	cols, data, err := c.db.recordQuery(query, namedValuesToValues(args))
	if err != nil {
		return nil, err
	}
	return &stubRows{columns: cols, data: data}, nil
}

type stubResult int64

func (r stubResult) LastInsertId() (int64, error) { return int64(r), nil }
func (r stubResult) RowsAffected() (int64, error) { return int64(r), nil }

type stubRows struct {
	columns []string
	data    [][]driver.Value
	i       int
}

func (r *stubRows) Columns() []string { return r.columns }
func (r *stubRows) Close() error      { return nil }

func (r *stubRows) Next(dest []driver.Value) error {
	if r.i >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.i])
	r.i++
	return nil
}

var stubDriverCounter int64

// errStubQuery is returned by the stub driver when a test wants the user lookup
// to fail.
var errStubQuery = errors.New("stub driver: query failed")

// withStubQueries installs a stub-backed model.Queries for the duration of the
// test and restores the previous value afterwards. columns/rows configure what
// QueryContext returns (use nil for tests that only execute statements).
func withStubQueries(t *testing.T, db *stubDB, columns []string, rows [][]driver.Value) *stubDB {
	t.Helper()
	if db == nil {
		db = &stubDB{}
	}
	db.columns, db.rows = columns, rows

	name := fmt.Sprintf("transport_stub_%d", atomic.AddInt64(&stubDriverCounter, 1))
	sql.Register(name, stubDriver{db: db})
	sqlDb, err := sql.Open(name, "stub")
	if err != nil {
		t.Fatalf("open stub driver: %v", err)
	}
	prevQueries, prevDb := model.Queries, model.SqlDb
	model.Queries = dbgen.New(sqlDb)
	model.SqlDb = sqlDb
	t.Cleanup(func() {
		model.Queries, model.SqlDb = prevQueries, prevDb
		_ = sqlDb.Close()
	})
	return db
}

// userRowColumns matches the SELECT in internal/dbgen/user.sql.go:getUserByName.
var userRowColumns = []string{
	"id", "username", "passhash", "contactemail",
	"callbackserviceuri", "callbackservicewsdluri",
	"callbackusername", "callbackpassword", "role",
}

func userRow(id int64, username, email string) []driver.Value {
	return []driver.Value{
		id, username, "$2a$10$notarealhash", email, nil, nil, nil, nil, "Administrator",
	}
}

// ---------------------------------------------------------------------------
// storefile transport
// ---------------------------------------------------------------------------

func ctxWithUser(username string) context.Context {
	return user.NewContext(context.Background(), &model.UserModel{Username: username, Id: 1})
}

func TestStoreFile_Transport_InsertsPayloadIntoFileStore(t *testing.T) {
	db := withStubQueries(t, nil, nil, nil)

	s := &StoreFile{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{}); err != nil {
		t.Fatal(err)
	}

	payload := "ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER       *240101*1200*^*00501*000000001*0*P*:~"
	if err := s.Transport("1700000000.x12", payload); err != nil {
		t.Fatalf("Transport() = %v; want nil", err)
	}

	calls := db.execCalls()
	if len(calls) != 1 {
		t.Fatalf("recorded %d ExecContext calls; want exactly 1 (the tFileStore insert)", len(calls))
	}
	call := calls[0]
	if !strings.Contains(call.Query, "INSERT INTO tFileStore") {
		t.Errorf("statement = %q; want it to insert into tFileStore", call.Query)
	}
	if !strings.Contains(call.Query, "content") || !strings.Contains(call.Query, "contentsize") {
		t.Errorf("statement = %q; want the content/contentsize columns to be populated", call.Query)
	}

	if len(call.Args) != 8 {
		t.Fatalf("statement has %d arguments; want 8 (user, stamp, category, filename, payloadId, processorId, content, contentsize)", len(call.Args))
	}
	if got, want := call.Args[0], driver.Value("alice"); got != want {
		t.Errorf("arg[0] (user) = %#v; want %#v (from user.FromContext)", got, want)
	}
	if _, ok := call.Args[1].(time.Time); !ok {
		t.Errorf("arg[1] (stamp) = %#v; want a time.Time", call.Args[1])
	}
	if got, want := call.Args[2], driver.Value("output"); got != want {
		t.Errorf("arg[2] (category) = %#v; want %#v", got, want)
	}
	if got, want := call.Args[3], driver.Value("1700000000.x12"); got != want {
		t.Errorf("arg[3] (filename) = %#v; want %#v (passed through unchanged)", got, want)
	}
	if got, want := call.Args[6], driver.Value(payload); got != want {
		t.Errorf("arg[6] (content) = %#v; want the payload %#v", got, want)
	}
	if got, want := call.Args[7], driver.Value(int64(len(payload))); got != want {
		t.Errorf("arg[7] (contentsize) = %#v; want %#v", got, want)
	}
}

func TestStoreFile_Transport_AcceptsStringAndBytePayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		want    string
	}{
		{"string", "hello world", "hello world"},
		{"bytes", []byte("hello world"), "hello world"},
		{"empty string", "", ""},
		{"binary bytes", []byte{0x00, 0x01, 0xfe, 0xff}, string([]byte{0x00, 0x01, 0xfe, 0xff})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := withStubQueries(t, nil, nil, nil)
			s := &StoreFile{}
			if err := s.SetContext(ctxWithUser("bob")); err != nil {
				t.Fatal(err)
			}
			if err := s.Transport("out.bin", tt.payload); err != nil {
				t.Fatalf("Transport(%T) = %v; want nil", tt.payload, err)
			}
			calls := db.execCalls()
			if len(calls) != 1 {
				t.Fatalf("recorded %d ExecContext calls; want 1", len(calls))
			}
			if got := calls[0].Args[6]; got != driver.Value(tt.want) {
				t.Errorf("content = %#v; want %#v", got, tt.want)
			}
			if got, want := calls[0].Args[7], driver.Value(int64(len(tt.want))); got != want {
				t.Errorf("contentsize = %#v; want %#v", got, want)
			}
		})
	}
}

func TestStoreFile_Transport_RejectsUnsupportedPayloadType(t *testing.T) {
	db := withStubQueries(t, nil, nil, nil)
	s := &StoreFile{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("out.bin", 12345)
	if err == nil {
		t.Fatal("Transport(int) = nil; want an error")
	}
	if !strings.Contains(err.Error(), "invalid data type") {
		t.Errorf("Transport(int) = %q; want it to report an invalid data type", err.Error())
	}
	if got := len(db.execCalls()); got != 0 {
		t.Errorf("recorded %d ExecContext calls for a rejected payload; want 0", got)
	}
}

func TestStoreFile_Transport_RequiresUserInContext(t *testing.T) {
	db := withStubQueries(t, nil, nil, nil)
	s := &StoreFile{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("out.bin", "payload")
	if err == nil {
		t.Fatal("Transport() without a user in context = nil; want an error")
	}
	if got := len(db.execCalls()); got != 0 {
		t.Errorf("recorded %d ExecContext calls without a user; want 0", got)
	}
}

func TestStoreFile_Transport_SurfacesDatabaseError(t *testing.T) {
	dbErr := errors.New("db is down")
	db := withStubQueries(t, &stubDB{execErr: dbErr}, nil, nil)
	s := &StoreFile{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("out.bin", "payload")
	if err == nil {
		t.Fatal("Transport() with a failing insert = nil; want the database error")
	}
	if !errors.Is(err, dbErr) {
		t.Errorf("Transport() = %v; want it to wrap %v", err, dbErr)
	}
	if got := len(db.execCalls()); got != 1 {
		t.Errorf("recorded %d ExecContext calls; want 1 attempt", got)
	}
}

// TestStoreFile_Transport_NilQueriesPanics documents CURRENT behaviour: the
// plugin dereferences the package-level model.Queries unconditionally, so a
// process that never ran model.InitDb() (or whose DB failed to initialise)
// panics inside the job worker instead of returning an error. Pinned, not
// fixed, because production code must not change here.
func TestStoreFile_Transport_NilQueriesPanics(t *testing.T) {
	prevQueries := model.Queries
	model.Queries = nil
	t.Cleanup(func() { model.Queries = prevQueries })

	s := &StoreFile{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}

	defer func() {
		caught := recover()
		if caught == nil {
			t.Fatalf("Transport() with model.Queries == nil did NOT panic; current behaviour changed (storefile.go:48 dereferences model.Queries)")
		}
		t.Logf("pinned behaviour: model.Queries == nil panics with %v (storefile.go:48)", caught)
	}()
	_ = s.Transport("out.bin", "payload")
}

func TestStoreFile_Contract_Surface(t *testing.T) {
	s := &StoreFile{}
	if got := s.InputFormat(); got != "*" {
		t.Errorf("InputFormat() = %q; want %q", got, "*")
	}
	if got := len(s.Options()); got != 0 {
		t.Errorf("Options() = %v; want an empty list (the Java plugin has no configuration options)", got)
	}
	m, err := InstantiateTransporter("storefile")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*StoreFile); !ok {
		t.Fatalf("InstantiateTransporter(\"storefile\") = %T; want *StoreFile", m)
	}
}
