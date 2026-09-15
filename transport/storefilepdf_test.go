package transport

// storefilepdf_test.go pins the storefilepdf transport contract
// (storefilepdf.go): the rendered PDF is persisted into tFileStore through
// model.Queries, exactly like storefile but declaring the "pdf" input format
// (which is what makes jobqueue pick the pdf translator, see
// translation.ResolveTranslator(jobqueue.go:316)).
//
// The two Go plugins necessarily differ, because the Java originals differ:
// StoreFile.java:85-97 sniffs the payload to pick a pdf/xml/x12/txt extension
// for the name it builds, while StoreFilePdf.java:83-87 always builds
// "<millis>.pdf". Both pass the identical category, the literal "output"
// (StoreFile.java:100, StoreFilePdf.java:87) - neither derives it - and both
// name the file themselves, whereas this port has jobqueue name it
// ("<nano>.<ext>", from the plugin's InputFormat()). Asserted here: the
// category is the Java literal, and the stored name always carries this
// plugin's own format extension.
//
// This file also uses the tiny in-test database stub declared in
// storefile_test.go; no real database is contacted.

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
	// Category: the Java literal both originals pass to DbFileStore.putFile
	// (StoreFile.java:100, StoreFilePdf.java:87). It is not derived from the
	// payload or the format by either Java plugin - the reported divergence
	// does not exist here - and it is half of the store's unique key
	// (user, category, filename).
	if got, want := call.Args[2], driver.Value("output"); got != want {
		t.Errorf("arg[2] (category) = %#v; want %#v (the Java literal)", got, want)
	}
	// The filename carries the plugin's own format extension. This one already
	// ends in ".pdf" (jobqueue names the file <nano>.<ext>, jobqueue.go:364),
	// so it is stored verbatim; the extension is appended when the caller's
	// name does not carry it (see
	// TestStoreFilePdf_Transport_StoresAPdfFilename).
	if got, want := call.Args[3], driver.Value("1700000001.pdf"); got != want {
		t.Errorf("arg[3] (filename) = %#v; want %#v", got, want)
	}
	if got := call.Args[3]; !strings.HasSuffix(got.(string), ".pdf") {
		t.Errorf("arg[3] (filename) = %#v; want a name carrying the plugin's format extension", got)
	}
	if got, want := call.Args[6], driver.Value(string(payload)); got != want {
		t.Errorf("arg[6] (content) = %#v; want the PDF bytes %#v", got, want)
	}
	if got, want := call.Args[7], driver.Value(int64(len(payload))); got != want {
		t.Errorf("arg[7] (contentsize) = %#v; want %#v", got, want)
	}
}

// TestStoreFilePdf_Transport_StoresAPdfFilename pins the corrected behaviour
// of the defect this suite used to document ("the filename is stored exactly
// as passed and no \".pdf\" is appended by the plugin").
//
// A Java StoreFilePdf row can never exist under a name that is not a ".pdf":
// the original builds "<System.currentTimeMillis()>.pdf" itself
// (StoreFilePdf.java:83-87) - it does NOT sniff the payload the way
// StoreFile.java:85-97 does for its pdf/xml/x12/txt cases. This port lets the
// caller name the file (jobqueue builds "<nano>.<ext>" from InputFormat()), so
// the plugin carries the Java's guarantee instead of trusting the caller: a
// name that already ends in the format's extension is stored verbatim, one
// that does not gets it appended.
func TestStoreFilePdf_Transport_StoresAPdfFilename(t *testing.T) {
	tests := []struct {
		name string
		pass string
		want string
	}{
		{"caller already named it .pdf", "1700000001.pdf", "1700000001.pdf"},
		{"caller used upper case", "1700000001.PDF", "1700000001.PDF"},
		{"name with no extension", "1700000001", "1700000001.pdf"},
		{"name declaring another format", "claim-form.txt", "claim-form.txt.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := withStubQueries(t, nil, nil, nil)
			s := &StoreFilePdf{}
			if err := s.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			if err := s.Transport(tt.pass, []byte("%PDF-1.7")); err != nil {
				t.Fatalf("Transport(%q) = %v; want nil", tt.pass, err)
			}
			calls := db.execCalls()
			if len(calls) != 1 {
				t.Fatalf("recorded %d ExecContext calls; want 1", len(calls))
			}
			if got := calls[0].Args[3]; got != driver.Value(tt.want) {
				t.Errorf("filename = %#v; want %#v", got, tt.want)
			}
			if got, want := calls[0].Args[2], driver.Value("output"); got != want {
				t.Errorf("category = %#v; want %#v (the Java literal)", got, want)
			}
		})
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

// TestStoreFilePdf_Transport_NilQueriesReturnsError pins that storefilepdf
// behaves like storefile: an uninitialised database fails the job with an error
// instead of panicking the worker.
//
// (Replaces TestStoreFilePdf_Transport_NilQueriesPanics, which pinned the
// panic.)
func TestStoreFilePdf_Transport_NilQueriesReturnsError(t *testing.T) {
	prevQueries := model.Queries
	model.Queries = nil
	t.Cleanup(func() { model.Queries = prevQueries })

	tests := []struct {
		name    string
		payload any
	}{
		{"string payload", "%PDF-1.7"},
		{"byte payload", []byte("%PDF-1.7")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &StoreFilePdf{}
			if err := s.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}

			// A panic here fails the test: the plugin must return, not unwind.
			err := s.Transport("out.pdf", tt.payload)
			if err == nil {
				t.Fatal("Transport() with model.Queries == nil returned a nil error; want a database error")
			}
			if !strings.Contains(err.Error(), "database is not initialised") {
				t.Errorf("Transport() = %q; want it to report that the database is not initialised", err.Error())
			}
		})
	}
}

func TestStoreFilePdf_Contract_Surface(t *testing.T) {
	s := &StoreFilePdf{}
	if got := s.InputFormat(); got != "pdf" {
		t.Errorf("InputFormat() = %q; want %q", got, "pdf")
	}
	// The stored name's extension is the plugin's declared format - this port's
	// expression of the Java's hardcoded ".pdf" (StoreFilePdf.java:83-87).
	if got, want := s.storedFilename("1700000001"), "1700000001."+s.InputFormat(); got != want {
		t.Errorf("storedFilename() = %q; want %q", got, want)
	}
	// The category is the Java literal in both originals, not derived.
	if fileStoreCategory != "output" {
		t.Errorf("fileStoreCategory = %q; want %q (StoreFile.java:100, StoreFilePdf.java:87)", fileStoreCategory, "output")
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
