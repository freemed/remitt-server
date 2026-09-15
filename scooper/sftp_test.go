package scooper

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
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

// TestSftpScooper_SetParameters_PortParsing pins what fmt.Sscanf("%d") does with
// the sftpPort string (sftp.go:164-166). The parse error is discarded, so a
// malformed port silently becomes 0 or a truncated number instead of a
// configuration error.
func TestSftpScooper_SetParameters_PortParsing(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"plainPort", "22", 22},
		{"leadingSpace", " 2222", 2222},
		{"trailingGarbageTruncates", "2222xyz", 2222},
		{"signed", "+2200", 2200},
		{"zero", "0", 0},
		{"negativeIsAccepted", "-1", -1},
		{"floatTruncates", "22.5", 22},
		{"hexishParsesAsZero", "0x22", 0},
		{"emptyIsIgnored", "", 0},
		{"nonNumericIsIgnored", "abc", 0},
		{"overflowLeavesZero", "99999999999999999999", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &SftpScooper{}
			if err := s.SetParameters(map[string]string{"sftpHost": "sftp.example.invalid", "sftpPort": tt.in}); err != nil {
				t.Fatalf("SetParameters() error = %v; want nil", err)
			}
			if s.port != tt.want {
				t.Errorf("sftpPort %q parsed to %d; want %d", tt.in, s.port, tt.want)
			}
		})
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

// TestKnownBug_ScoopWithUninitializedDatabasePanics pins a robustness defect:
// sftp.go:42 dereferences model.SqlDb without checking that model.InitDb has
// run, so a scooper run in a process without a database panics instead of
// returning an error. This documents current behaviour; the guard at sftp.go:37
// only covers host/port.
func TestKnownBug_ScoopWithUninitializedDatabasePanics(t *testing.T) {
	withNilSqlDb(t)

	s := newConfiguredSftpScooper(t, "user1", "sftp.example.invalid", 22, "remits")

	var panicked any
	var err error
	func() {
		defer func() { panicked = recover() }()
		_, err = s.Scoop()
	}()

	if panicked == nil {
		t.Fatalf("Scoop() returned err = %v without panicking; want the current behaviour (a nil model.SqlDb dereference at sftp.go:42)", err)
	}
	if !strings.Contains(fmt.Sprint(panicked), "nil pointer dereference") {
		t.Errorf("Scoop() panicked with %v; want a nil pointer dereference from the unguarded model.SqlDb use", panicked)
	}
}
