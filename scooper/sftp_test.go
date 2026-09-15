package scooper

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// newConfiguredSftpScooper builds an SftpScooper with exactly the parameters
// the plugin loader would hand over from tUserConfig.
func newConfiguredSftpScooper(t *testing.T, user, host string, port int, path string) *SftpScooper {
	t.Helper()

	s := &SftpScooper{}
	err := s.SetParameters(map[string]string{
		"sftpHost":     host,
		"sftpPort":     fmt.Sprintf("%d", port),
		"sftpUsername": "sftpuser",
		"sftpPassword": "sftppass",
		"sftpPath":     path,
	})
	if err != nil {
		t.Fatalf("SetParameters() error = %v; want nil", err)
	}
	if err := s.SetUsername(user); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}
	return s
}

// closedLoopbackPort returns a port on 127.0.0.1 that has just been released, so
// connecting to it fails immediately. No external host is ever contacted.
func closedLoopbackPort(t *testing.T) (string, int) {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen(loopback): %v", err)
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not a *net.TCPAddr", l.Addr())
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close loopback listener: %v", err)
	}
	return "127.0.0.1", addr.Port
}

// TestSftpScooper_SetParameters_MapsConfigKeys is the input->output mapping
// table for the plugin loader's parameter map: which tUserConfig key lands in
// which field, and which keys are ignored (sftp.go:159-169).
func TestSftpScooper_SetParameters_MapsConfigKeys(t *testing.T) {
	tests := []struct {
		name     string
		params   map[string]string
		wantHost string
		wantPort int
		wantUser string
		wantPass string
		wantPath string
	}{
		{
			name: "allDocumentedKeys",
			params: map[string]string{
				"sftpHost":     "sftp.example.invalid",
				"sftpPort":     "2222",
				"sftpUsername": "remitt-user",
				"sftpPassword": "s3cret",
				"sftpPath":     "remits",
			},
			wantHost: "sftp.example.invalid",
			wantPort: 2222,
			wantUser: "remitt-user",
			wantPass: "s3cret",
			wantPath: "remits",
		},
		{
			name:     "nilMapLeavesEverythingEmpty",
			params:   nil,
			wantHost: "",
			wantPort: 0,
			wantUser: "",
			wantPass: "",
			wantPath: "",
		},
		{
			name:     "emptyMapLeavesEverythingEmpty",
			params:   map[string]string{},
			wantHost: "",
			wantPort: 0,
			wantUser: "",
			wantPass: "",
			wantPath: "",
		},
		{
			name: "unknownKeysAreIgnored",
			params: map[string]string{
				"unknownHost": "ignored.invalid",
				"sftpTls":     "true",
			},
			wantHost: "",
			wantPort: 0,
		},
		{
			name: "javaStyleDottedKeysAreNotRecognised",
			params: map[string]string{
				"org.remitt.plugin.scooper.SftpScooper.sftpHost": "ignored.invalid",
				"org.remitt.plugin.scooper.SftpScooper.sftpPort": "22",
			},
			wantHost: "",
			wantPort: 0,
		},
		{
			name: "partialConfiguration",
			params: map[string]string{
				"sftpHost": "sftp.example.invalid",
				"sftpPath": "/inbound/remits",
			},
			wantHost: "sftp.example.invalid",
			wantPort: 0,
			wantPath: "/inbound/remits",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SftpScooper{}
			if err := s.SetParameters(tt.params); err != nil {
				t.Fatalf("SetParameters(%v) error = %v; want nil", tt.params, err)
			}

			if s.host != tt.wantHost {
				t.Errorf("host = %q; want %q", s.host, tt.wantHost)
			}
			if s.port != tt.wantPort {
				t.Errorf("port = %d; want %d", s.port, tt.wantPort)
			}
			if s.sftpUser != tt.wantUser {
				t.Errorf("sftpUser = %q; want %q", s.sftpUser, tt.wantUser)
			}
			if s.sftpPass != tt.wantPass {
				t.Errorf("sftpPass = %q; want %q", s.sftpPass, tt.wantPass)
			}
			if s.sftpPath != tt.wantPath {
				t.Errorf("sftpPath = %q; want %q", s.sftpPath, tt.wantPath)
			}
			if got := fmt.Sprint(s.params); tt.params != nil && got != fmt.Sprint(tt.params) {
				t.Errorf("params = %v; want the map handed to SetParameters (%v)", s.params, tt.params)
			}
		})
	}
}

// TestSftpScooper_SetParameters_PortParsingIsStrict is the regression test for
// the discarded fmt.Sscanf error (sftp.go:164-166). A malformed sftpPort used to
// be truncated ("2222xyz" -> 2222), silently zeroed ("abc" -> 0, "22.5" -> 22,
// "0x22" -> 0, an overflowing literal -> 0) or accepted as negative ("-1" passed
// the host/port guard and was dialled). It must now be rejected: no truncated
// value, and a reported configuration error instead.
//
// SetParameters still returns nil — the plugin loader never inspects it — so the
// error is asserted through validateConfig, which is what Scoop consults before
// any database or network access.
func TestSftpScooper_SetParameters_PortParsingIsStrict(t *testing.T) {
	tests := []struct {
		name            string
		in              string
		want            int
		wantInvalidPort bool
	}{
		{"plainPort", "22", 22, false},
		{"leadingSpace", " 2222", 2222, false},
		{"signed", "+2200", 2200, false},
		{"zeroMeansUnset", "0", 0, false},
		{"emptyMeansUnset", "", 0, false},
		{"trailingGarbageIsRejected", "2222xyz", 0, true},
		{"nonNumericIsRejected", "abc", 0, true},
		{"negativeIsRejected", "-1", 0, true},
		{"fractionalIsRejected", "22.5", 0, true},
		{"hexishIsRejected", "0x22", 0, true},
		{"overflowIsRejected", "99999999999999999999", 0, true},
		{"aboveRangeIsRejected", "65536", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SftpScooper{}
			if err := s.SetParameters(map[string]string{"sftpHost": "sftp.example.invalid", "sftpPort": tt.in}); err != nil {
				t.Fatalf("SetParameters() error = %v; want nil (the port is reported by Scoop, not by the setter)", err)
			}
			if s.port != tt.want {
				t.Errorf("sftpPort %q parsed to %d; want %d (a malformed value must never be truncated or go negative)", tt.in, s.port, tt.want)
			}

			err := s.validateConfig()
			switch {
			case tt.wantInvalidPort:
				if err == nil {
					t.Fatalf("validateConfig() error = nil for sftpPort %q; want the malformed port reported", tt.in)
				}
				if !strings.Contains(err.Error(), fmt.Sprintf("invalid sftpPort %q", tt.in)) {
					t.Errorf("validateConfig() error = %q; want it to name the malformed value %q", err.Error(), tt.in)
				}
				if !strings.Contains(err.Error(), "sftpscooper: host/port not configured") {
					t.Errorf("validateConfig() error = %q; want the host/port configuration error", err.Error())
				}
			case tt.want == 0:
				// "0" and an absent port both mean "not configured": the generic
				// guard, not a malformed value.
				if err == nil {
					t.Fatalf("validateConfig() error = nil for an unset port; want the host/port configuration error")
				}
				if strings.Contains(err.Error(), "invalid sftpPort") {
					t.Errorf("validateConfig() error = %q; want the generic not-configured error for an unset port", err.Error())
				}
			default:
				if err != nil {
					t.Errorf("validateConfig() error = %v; want nil for the usable port %d", err, tt.want)
				}
			}
		})
	}
}

// TestSftpScooper_Scoop_MalformedPortNeverReachesDatabaseOrNetwork pins the
// consequence of the strict parse: a port that does not parse is reported before
// the dedupe query runs and before any session is opened, so "22xyz" can never
// dial port 22 and "-1" can never dial at all.
func TestSftpScooper_Scoop_MalformedPortNeverReachesDatabaseOrNetwork(t *testing.T) {
	fake := installFakeScooperDB(t)

	s := &SftpScooper{}
	if err := s.SetParameters(map[string]string{
		"sftpHost": "sftp.example.invalid",
		"sftpPort": "22xyz",
		"sftpPath": "remits",
	}); err != nil {
		t.Fatalf("SetParameters() error = %v; want nil", err)
	}
	if err := s.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	dialed := false
	s.sessionOpener = func() (sftpSession, error) {
		dialed = true
		return nil, errors.New("a session must not be opened for a malformed port")
	}

	results, err := s.Scoop()
	if err == nil {
		t.Fatal("Scoop() error = nil; want the malformed sftpPort to be reported")
	}
	if results != nil {
		t.Errorf("Scoop() results = %v; want nil alongside the error", results)
	}
	if !strings.Contains(err.Error(), `invalid sftpPort "22xyz"`) {
		t.Errorf("Scoop() error = %q; want it to name the malformed value", err.Error())
	}
	if !strings.Contains(err.Error(), "sftpscooper: host/port not configured") {
		t.Errorf("Scoop() error = %q; want the host/port configuration error", err.Error())
	}
	if dialed {
		t.Error("Scoop opened an SFTP session despite the malformed port")
	}
	if got := fake.scoopedQueryCount(); got != 0 {
		t.Errorf("previously-scooped query ran %d times; want 0 (the configuration error comes first)", got)
	}
}

// ---------------------------------------------------------------------------
// In-memory SFTP session
//
// SftpScooper.scoop's file loop is reachable only through a session, and the
// dependency tree ships a client but no server (ssh.Dial is called directly), so
// the injected sessionOpener is the only way to exercise the loop — and with it
// the PostProcess dispatch and the persistence of what was downloaded — from a
// test. No SSH or SFTP server is started and no external host is contacted.
// ---------------------------------------------------------------------------

// fakeSftpSession serves a fixed directory of files from memory.
type fakeSftpSession struct {
	mu      sync.Mutex
	files   map[string][]byte
	listErr error
	openErr error
	closed  bool
}

func (f *fakeSftpSession) Join(elem ...string) string {
	parts := make([]string, 0, len(elem))
	for _, e := range elem {
		if e = strings.Trim(e, "/"); e == "" {
			continue
		}
		parts = append(parts, e)
	}
	return strings.Join(parts, "/")
}

func (f *fakeSftpSession) ReadDir(string) ([]os.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, f.listErr
	}

	names := make([]string, 0, len(f.files))
	for name := range f.files {
		names = append(names, name)
	}
	sort.Strings(names)

	infos := make([]os.FileInfo, 0, len(names))
	for _, name := range names {
		infos = append(infos, fakeSftpFileInfo{name: name, size: int64(len(f.files[name]))})
	}
	return infos, nil
}

func (f *fakeSftpSession) Open(path string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.openErr != nil {
		return nil, f.openErr
	}

	content, found := f.files[path[strings.LastIndex(path, "/")+1:]]
	if !found {
		return nil, fmt.Errorf("fakeSftpSession: no such file %q", path)
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (f *fakeSftpSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeSftpSession) wasClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// fakeSftpFileInfo is the minimum os.FileInfo the file loop reads.
type fakeSftpFileInfo struct {
	name string
	size int64
}

func (f fakeSftpFileInfo) Name() string       { return f.name }
func (f fakeSftpFileInfo) Size() int64        { return f.size }
func (f fakeSftpFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeSftpFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeSftpFileInfo) IsDir() bool        { return false }
func (f fakeSftpFileInfo) Sys() any           { return nil }

// withFakeSftpSession installs an in-memory session on s so its Scoop file loop
// runs without an SSH server.
func withFakeSftpSession(s *SftpScooper, files map[string][]byte) *fakeSftpSession {
	session := &fakeSftpSession{files: files}
	s.sessionOpener = func() (sftpSession, error) { return session, nil }
	return session
}

// TestSftpScooper_Scoop_StoresDownloadedFilesAndSkipsScoopedOnes drives the file
// loop end to end: a file already recorded in tScooper is skipped, a new file is
// downloaded, transformed by the base PostProcess (identity), stored exactly as
// downloaded and returned, and the session is closed.
func TestSftpScooper_Scoop_StoresDownloadedFilesAndSkipsScoopedOnes(t *testing.T) {
	fake := installFakeScooperDB(t)
	fake.setScoopedRows(fakeScooperRecord(1, SftpScooperClass, "user1", time.Now(),
		"sftp.example.invalid", "remits", "already.edi", []byte("old bytes")))

	s := newConfiguredSftpScooper(t, "user1", "sftp.example.invalid", 22, "remits")
	session := withFakeSftpSession(s, map[string][]byte{
		"already.edi": []byte("old bytes"),
		"new.edi":     []byte("fresh remittance"),
	})

	results, err := s.Scoop()
	if err != nil {
		t.Fatalf("Scoop() error = %v; want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Scoop() returned %d results; want 1 (already.edi must be skipped)", len(results))
	}
	if results[0].Filename != "new.edi" {
		t.Errorf("result filename = %q; want %q", results[0].Filename, "new.edi")
	}
	if string(results[0].Content) != "fresh remittance" {
		t.Errorf("result content = %q; want the downloaded bytes unchanged", results[0].Content)
	}

	if got := fake.insertCount(); got != 1 {
		t.Fatalf("tScooper inserts = %d; want exactly 1", got)
	}
	args := fake.insertedArgs()
	if len(args) != 7 {
		t.Fatalf("tScooper insert args = %v; want 7 values", args)
	}
	if got := fmt.Sprint(args[5]); got != "new.edi" {
		t.Errorf("inserted filename = %v; want new.edi", got)
	}
	content, ok := args[6].([]byte)
	if !ok {
		t.Fatalf("inserted content is %T; want []byte", args[6])
	}
	if string(content) != "fresh remittance" {
		t.Errorf("stored content = %q; want the downloaded bytes unchanged", content)
	}
	if !session.wasClosed() {
		t.Error("the SFTP session was not closed after the scoop")
	}
}

// TestKnownBug_ScoopWithUninitializedDatabasePanics pinned a robustness defect:
// sftp.go:42 dereferenced model.SqlDb without checking that model.InitDb had
// run, so a scooper run in a process without a database panicked instead of
// returning an error. The guard at sftp.go:37 only covered host/port.
//
// The corrected contract: Scoop reports the uninitialised database and does not
// touch the network.
func TestSftpScooper_ScoopWithUninitializedDatabaseReturnsError(t *testing.T) {
	withNilSqlDb(t)

	s := newConfiguredSftpScooper(t, "user1", "sftp.example.invalid", 22, "remits")

	dialed := false
	s.sessionOpener = func() (sftpSession, error) {
		dialed = true
		return nil, errors.New("a session must not be opened without a database")
	}

	results, err := s.Scoop()
	if err == nil {
		t.Fatal("Scoop() error = nil; want a database-not-initialised error instead of a nil dereference")
	}
	if results != nil {
		t.Errorf("Scoop() results = %v; want nil alongside the error", results)
	}
	if !strings.Contains(err.Error(), "sftpscooper: database not initialized") {
		t.Errorf("Scoop() error = %q; want it to contain %q", err.Error(), "sftpscooper: database not initialized")
	}
	if dialed {
		t.Error("Scoop opened an SFTP session before noticing the database is missing")
	}
}

// TestSftpScooper_Scoop_RejectsUnconfiguredHostOrPort covers the configuration
// guard at sftp.go:37, which runs before any database or network access: a
// scooper with no usable host/port must fail with a configuration error rather
// than dialling anything. model.SqlDb is deliberately left nil, so any attempt
// to reach the database would panic instead of quietly succeeding.
func TestSftpScooper_Scoop_RejectsUnconfiguredHostOrPort(t *testing.T) {
	withNilSqlDb(t)

	tests := []struct {
		name   string
		params map[string]string
	}{
		{"noParameters", nil},
		{"hostButNoPort", map[string]string{"sftpHost": "sftp.example.invalid"}},
		{"portButNoHost", map[string]string{"sftpPort": "22"}},
		{"nonNumericPort", map[string]string{"sftpHost": "sftp.example.invalid", "sftpPort": "sftp"}},
		{"zeroPort", map[string]string{"sftpHost": "sftp.example.invalid", "sftpPort": "0"}},
		{"emptyHost", map[string]string{"sftpHost": "", "sftpPort": "22"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SftpScooper{}
			if err := s.SetParameters(tt.params); err != nil {
				t.Fatalf("SetParameters(%v) error = %v; want nil", tt.params, err)
			}
			if err := s.SetUsername("user1"); err != nil {
				t.Fatalf("SetUsername() error = %v; want nil", err)
			}

			results, err := s.Scoop()
			if err == nil {
				t.Fatal("Scoop() error = nil; want a host/port configuration error")
			}
			if !strings.Contains(err.Error(), "sftpscooper: host/port not configured") {
				t.Errorf("Scoop() error = %q; want it to contain %q", err.Error(), "sftpscooper: host/port not configured")
			}
			if results != nil {
				t.Errorf("Scoop() results = %v; want nil alongside the error", results)
			}
		})
	}
}

// TestSftpScooper_Scoop_PropagatesDatabaseErrors checks that a failure to list
// previously scooped files is reported, not treated as "nothing scooped yet"
// (sftp.go:42-48).
func TestSftpScooper_Scoop_PropagatesDatabaseErrors(t *testing.T) {
	fake := installFakeScooperDB(t)
	sentinel := errors.New("fake tScooper failure")
	fake.setScoopedQueryError(sentinel)

	s := newConfiguredSftpScooper(t, "user1", "sftp.example.invalid", 22, "remits")

	results, err := s.Scoop()
	if err == nil {
		t.Fatal("Scoop() error = nil; want the database failure to be reported")
	}
	if results != nil {
		t.Errorf("Scoop() results = %v; want nil alongside the error", results)
	}
	if !strings.Contains(err.Error(), "sftpscooper: query scooped") {
		t.Errorf("Scoop() error = %q; want it to contain %q", err.Error(), "sftpscooper: query scooped")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("Scoop() error = %v; want it to wrap the driver error so errors.Is works", err)
	}
	if got := fake.scoopedQueryCount(); got != 1 {
		t.Errorf("previously-scooped query ran %d times; want exactly 1", got)
	}
	if got := fake.insertCount(); got != 0 {
		t.Errorf("tScooper inserts = %d; want none after a failed lookup", got)
	}
}

// TestSftpScooper_Scoop_QueriesDedupeByClassUserHostPath pins the key used to
// decide what has already been scooped: the contract is
// (scooperClass, user, host, path) with the class constant from this file.
func TestSftpScooper_Scoop_QueriesDedupeByClassUserHostPath(t *testing.T) {
	fake := installFakeScooperDB(t)
	host, port := closedLoopbackPort(t)

	s := newConfiguredSftpScooper(t, "user1", host, port, "remits")

	// This run fails at the SSH dial (see the unreachable-server test); the
	// dedupe query happens first and is what this test asserts on.
	if _, err := s.Scoop(); err == nil {
		t.Fatal("Scoop() error = nil; want the SSH dial to fail against a closed port")
	}

	if got := fake.scoopedQueryCount(); got != 1 {
		t.Fatalf("previously-scooped query ran %d times; want exactly 1", got)
	}

	want := []any{SftpScooperClass, "user1", host, "remits"}
	got := fake.scoopedQueryArgs()
	if len(got) != len(want) {
		t.Fatalf("previously-scooped query args = %v; want %v", got, want)
	}
	for i := range want {
		if fmt.Sprint(got[i]) != fmt.Sprint(want[i]) {
			t.Errorf("previously-scooped query arg %d = %v; want %v", i, got[i], want[i])
		}
	}
}

// TestSftpScooper_Scoop_UnreachableServerIsReported covers the error path past
// the dedupe query: with no SFTP server listening the run must report a wrapped
// ssh dial error and must not report any scooped files. Only the loopback
// interface is contacted; no payer or remote SFTP system is involved.
func TestSftpScooper_Scoop_UnreachableServerIsReported(t *testing.T) {
	fake := installFakeScooperDB(t)
	host, port := closedLoopbackPort(t)

	s := newConfiguredSftpScooper(t, "user1", host, port, "remits")

	results, err := s.Scoop()
	if err == nil {
		t.Fatal("Scoop() error = nil; want an ssh dial failure against a closed loopback port")
	}
	if results != nil {
		t.Errorf("Scoop() results = %v; want nil alongside the error", results)
	}
	if !strings.Contains(err.Error(), "sftpscooper: ssh dial") {
		t.Errorf("Scoop() error = %q; want it to contain %q", err.Error(), "sftpscooper: ssh dial")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%s:%d", host, port)) {
		t.Errorf("Scoop() error = %q; want it to name the configured endpoint %s:%d", err.Error(), host, port)
	}
	if got := fake.insertCount(); got != 0 {
		t.Errorf("tScooper inserts = %d; want none when the connection never opened", got)
	}
}

// TestSftpScooper_PostProcess_DefaultIsIdentity pins the base-class hook: the
// bytes received must be handed on unchanged, whatever the filename
// (sftp.go:151-155, the port of SftpScooper.postprocess in the Java original).
func TestSftpScooper_PostProcess_DefaultIsIdentity(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"x12Payload", []byte("ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER       *240101*1200*^*00501*000000001*0*P*:~")},
		{"emptyPayload", []byte{}},
		{"nilPayload", nil},
		{"binaryPayload", []byte{0x00, 0x01, 0xfe, 0xff}},
		{"armoredLookingPayload", []byte("-----BEGIN PGP MESSAGE-----\n\nnot really\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SftpScooper{}
			got, err := s.PostProcess(tt.data, "remit.edi")
			if err != nil {
				t.Fatalf("PostProcess() error = %v; want nil", err)
			}
			if !bytes.Equal(got, tt.data) {
				t.Errorf("PostProcess() = %q; want the input unchanged (%q)", got, tt.data)
			}

			// The filename argument is accepted but ignored.
			other, err := s.PostProcess(tt.data, "completely-different-name.pgp")
			if err != nil {
				t.Fatalf("PostProcess() with another filename error = %v; want nil", err)
			}
			if !bytes.Equal(other, got) {
				t.Errorf("PostProcess() depends on the filename: %q vs %q", other, got)
			}
		})
	}
}
