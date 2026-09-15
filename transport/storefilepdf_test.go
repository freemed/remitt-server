package transport

// storefilepdf_test.go pins the storefilepdf transport contract
// (storefilepdf.go): the rendered PDF is persisted into tFileStore through
// model.Queries, exactly like storefile but declaring the "pdf" input format
// (which is what makes jobqueue pick the pdf translator, see
// translation.ResolveTranslator(jobqueue.go:316)).
//
// storefilepdf.go is a byte-for-byte copy of storefile.go apart from
// InputFormat(), so these tests assert the same persistence contract and would
// catch the two drifting apart.

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/model"
)

func TestStoreFilePdf_Transport_InsertsPayloadIntoFileStore(t *testing.T) {
	db := withStubQueries(t, nil, nil, nil)

	s := &StoreFilePdf{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{}); err != nil {
		t.Fatal(err)
	}

	payload := []byte("%PDF-1.7\n...rendered claim form...\n%%EOF")
	if err := s.Transport("1700000001.pdf", payload); err != nil {
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
	if len(call.Args) != 8 {
		t.Fatalf("statement has %d arguments; want 8", len(call.Args))
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
	// The filename is stored exactly as passed and no ".pdf" is appended by the
	// plugin: jobqueue already names the file <nano>.<ext>.
	if got, want := call.Args[3], driver.Value("1700000001.pdf"); got != want {
		t.Errorf("arg[3] (filename) = %#v; want %#v", got, want)
	}
	if got, want := call.Args[6], driver.Value(string(payload)); got != want {
		t.Errorf("arg[6] (content) = %#v; want the PDF bytes %#v", got, want)
	}
	if got, want := call.Args[7], driver.Value(int64(len(payload))); got != want {
		t.Errorf("arg[7] (contentsize) = %#v; want %#v", got, want)
	}
}

func TestStoreFilePdf_Transport_RejectsUnsupportedPayloadType(t *testing.T) {
	db := withStubQueries(t, nil, nil, nil)
	s := &StoreFilePdf{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("out.pdf", []string{"not", "bytes"})
	if err == nil {
		t.Fatal("Transport([]string) = nil; want an error")
	}
	if !strings.Contains(err.Error(), "invalid data type") {
		t.Errorf("Transport([]string) = %q; want it to report an invalid data type", err.Error())
	}
	if got := len(db.execCalls()); got != 0 {
		t.Errorf("recorded %d ExecContext calls for a rejected payload; want 0", got)
	}
}

func TestStoreFilePdf_Transport_RequiresUserInContext(t *testing.T) {
	db := withStubQueries(t, nil, nil, nil)
	s := &StoreFilePdf{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("out.pdf", "%PDF-1.7")
	if err == nil {
		t.Fatal("Transport() without a user in context = nil; want an error")
	}
	if got := len(db.execCalls()); got != 0 {
		t.Errorf("recorded %d ExecContext calls without a user; want 0", got)
	}
}

func TestStoreFilePdf_Transport_SurfacesDatabaseError(t *testing.T) {
	dbErr := errors.New("db is down")
	db := withStubQueries(t, &stubDB{execErr: dbErr}, nil, nil)
	s := &StoreFilePdf{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("out.pdf", "%PDF-1.7")
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

// TestStoreFilePdf_Transport_NilQueriesPanics documents CURRENT behaviour:
// like storefile, the plugin dereferences model.Queries unconditionally, so an
// uninitialised database panics inside the job worker.
func TestStoreFilePdf_Transport_NilQueriesPanics(t *testing.T) {
	prevQueries := model.Queries
	model.Queries = nil
	t.Cleanup(func() { model.Queries = prevQueries })

	s := &StoreFilePdf{}
	if err := s.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}

	defer func() {
		caught := recover()
		if caught == nil {
			t.Fatalf("Transport() with model.Queries == nil did NOT panic; current behaviour changed (storefilepdf.go:48 dereferences model.Queries)")
		}
		t.Logf("pinned behaviour: model.Queries == nil panics with %v (storefilepdf.go:48)", caught)
	}()
	_ = s.Transport("out.pdf", "%PDF-1.7")
}

func TestStoreFilePdf_Contract_Surface(t *testing.T) {
	s := &StoreFilePdf{}
	if got := s.InputFormat(); got != "pdf" {
		t.Errorf("InputFormat() = %q; want %q", got, "pdf")
	}
	if got := len(s.Options()); got != 0 {
		t.Errorf("Options() = %v; want an empty list (the Java plugin has no configuration options)", got)
	}
	m, err := InstantiateTransporter("storefilepdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*StoreFilePdf); !ok {
		t.Fatalf("InstantiateTransporter(\"storefilepdf\") = %T; want *StoreFilePdf", m)
	}
	// The two storefile variants must stay distinguishable by input format.
	if (&StoreFile{}).InputFormat() == s.InputFormat() {
		t.Fatalf("storefile and storefilepdf both declare InputFormat %q; the pdf translator would no longer be selected", s.InputFormat())
	}
}
