package transport

// sftp_test.go pins the sftp transport contract (sftp.go): configuration is
// read from the documented option keys (sftpUsername/sftpPassword/sftpHost/
// sftpPort/sftpPath), the plugin refuses to run with missing configuration, an
// option that cannot be coerced is reported by SetOptions, and the connection
// is attempted against exactly the host and port it was configured with.
//
// Host key verification is pinned here too: the transport verifies against
// paths.known-hosts, sftp-insecure-ignore-hostkey is the only (explicit,
// warning-logging) bypass, and with neither configured Transport fails closed
// without dialling. The host key tests drive real SSH/SFTP servers stood up
// in-process by startSftpTestServer (golang.org/x/crypto/ssh server +
// pkg/sftp's own sftp.NewServer), so a handshake, an authentication and an
// upload really happen; no remote host is contacted.
//
// The reachable-but-not-SSH listener (localSSHBait) remains a loopback test
// double for the pure dial-target and fail-closed-before-dial assertions.

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/freemed/remitt-server/config"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// localSSHBait starts a loopback listener for one incoming TCP connection. The
// returned channel yields the SSH identification string the client sent once a
// connection is accepted (an empty string means the peer closed the connection
// without sending one), and yields nothing at all if no connection ever
// arrives. It exists so tests can prove which host:port a plugin dialled without
// contacting a real SFTP server; it never speaks SSH.
func localSSHBait(t *testing.T) (host string, port int, accepted <-chan string) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on loopback: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	ch := make(chan string, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return // no connection arrived
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		line, _ := bufio.NewReader(conn).ReadString('\n')
		ch <- strings.TrimSpace(line)
	}()

	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not a *net.TCPAddr", l.Addr())
	}
	return "127.0.0.1", addr.Port, ch
}

// awaitConnection waits for the loopback listener to accept a connection.
func awaitConnection(t *testing.T, accepted <-chan string) string {
	t.Helper()
	select {
	case line := <-accepted:
		return line
	case <-time.After(10 * time.Second):
		t.Fatal("no connection reached the configured host:port within 10s")
		return ""
	}
}

// awaitNoConnection fails if the loopback listener accepts a connection within
// grace. It is how "the transport refused to dial at all" is asserted: the
// listener is the only thing that can observe a connection attempt.
func awaitNoConnection(t *testing.T, accepted <-chan string, grace time.Duration) {
	t.Helper()
	select {
	case line := <-accepted:
		t.Fatalf("the transport connected to the configured host:port (peer received %q); no connection may be attempted under this policy", line)
	case <-time.After(grace):
	}
}

// ---------------------------------------------------------------------------
// Host key policy test harness
// ---------------------------------------------------------------------------

// withHostKeyPolicy installs a host key policy for the duration of the test.
// common.HostKeyCallback reads the global config.Config, which is what the
// transports consult, so the policy is installed there and the previous value
// restored afterwards (script_mail_test.go does the same for config.Config.Mail).
func withHostKeyPolicy(t *testing.T, knownHostsPath string, insecureIgnoreHostKey bool) {
	t.Helper()

	prev := config.Config
	cfg := config.AppConfig{}
	if prev != nil {
		// Copy: the test must not mutate a configuration another test is using.
		cfg = *prev
	}
	cfg.Paths.KnownHostsPath = knownHostsPath
	cfg.SftpInsecureIgnoreHostKey = insecureIgnoreHostKey
	config.Config = &cfg
	t.Cleanup(func() { config.Config = prev })
}

// ---------------------------------------------------------------------------
// In-process SSH + SFTP server
//
// pkg/sftp ships a server (sftp.NewServer) as well as a client, so a transport
// can be driven against a real SSH handshake, a real authentication and a real
// SFTP session without a remote host: the endpoint, the credentials and the
// host key all belong to the test, and the only filesystem touched is the
// server's temporary directory.
// ---------------------------------------------------------------------------

const (
	testSftpUser = "bob"
	testSftpPass = "secret"
)

type sftpTestServer struct {
	host   string
	port   int
	dir    string
	pubKey ssh.PublicKey
	conns  atomic.Int64
}

// startSftpTestServer stands up a password-authenticated SSH server that serves
// an SFTP subsystem rooted at a temporary directory.
func startSftpTestServer(t *testing.T) *sftpTestServer {
	t.Helper()

	dir := t.TempDir()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate server host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("server host key signer: %v", err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == testSftpUser && string(pass) == testSftpPass {
				return nil, nil
			}
			return nil, fmt.Errorf("test sftp server: access denied for %q", c.User())
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not a *net.TCPAddr", ln.Addr())
	}

	srv := &sftpTestServer{
		host:   "127.0.0.1",
		port:   addr.Port,
		dir:    dir,
		pubKey: signer.PublicKey(),
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			srv.conns.Add(1)
			go serveTestSftpConn(conn, cfg, dir)
		}
	}()

	return srv
}

// serveTestSftpConn completes the SSH handshake and hands an accepted "sftp"
// subsystem channel to pkg/sftp's server implementation (same shape as the
// server in .hermes/probe-tmp/main.go).
func serveTestSftpConn(conn net.Conn, cfg *ssh.ServerConfig, dir string) {
	sconn, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			_ = newChan.Reject(ssh.UnknownChannelType, "only session is supported")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		go func(ch ssh.Channel, in <-chan *ssh.Request) {
			defer ch.Close()
			for r := range in {
				if r.Type == "subsystem" && len(r.Payload) >= 4 && string(r.Payload[4:]) == "sftp" {
					_ = r.Reply(true, nil)
					s, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(dir))
					if err != nil {
						return
					}
					_ = s.Serve()
					return
				}
				_ = r.Reply(false, nil)
			}
		}(ch, chReqs)
	}
}

// address is the host:port a transport has to be configured with to reach it.
func (s *sftpTestServer) address() string { return fmt.Sprintf("%s:%d", s.host, s.port) }

// knownHostsFile writes the known_hosts file an operator would capture from
// this server with ssh-keyscan, for exactly the dialled address.
func (s *sftpTestServer) knownHostsFile(t *testing.T) string {
	t.Helper()
	return writeKnownHosts(t, knownhosts.Line([]string{s.address()}, s.pubKey)+"\n")
}

// knownHostsFileWithDifferentKey writes a known_hosts file that records the
// same address under a DIFFERENT host key - what a rebuilt server or a
// man-in-the-middle produces.
func (s *sftpTestServer) knownHostsFileWithDifferentKey(t *testing.T) string {
	t.Helper()

	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate mismatched host key: %v", err)
	}
	otherSigner, err := ssh.NewSignerFromKey(otherPriv)
	if err != nil {
		t.Fatalf("mismatched host key signer: %v", err)
	}
	if string(otherSigner.PublicKey().Marshal()) == string(s.pubKey.Marshal()) {
		t.Fatal("the mismatched key equals the server's key; the test would not test a mismatch")
	}
	return writeKnownHosts(t, knownhosts.Line([]string{s.address()}, otherSigner.PublicKey())+"\n")
}

func writeKnownHosts(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

// uploaded reads a file the transport should have created on the server.
func (s *sftpTestServer) uploaded(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		t.Fatalf("the SFTP server did not receive %q: %v", name, err)
	}
	return data
}

// acceptedConnections reports how many TCP connections the server accepted.
func (s *sftpTestServer) acceptedConnections() int64 { return s.conns.Load() }

func TestSftp_Transport_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]any
		want    string
	}{
		{
			name:    "no options at all",
			options: map[string]any{},
			want:    "sftp: missing host, port, or username",
		},
		{
			name: "host only",
			options: map[string]any{
				"sftpHost": "127.0.0.1",
			},
			want: "sftp: missing host, port, or username",
		},
		{
			name: "host and port, no username",
			options: map[string]any{
				"sftpHost": "127.0.0.1", "sftpPort": 22,
			},
			want: "sftp: missing host, port, or username",
		},
		{
			name: "host, port and username, no credentials",
			options: map[string]any{
				"sftpHost": "127.0.0.1", "sftpPort": 22, "sftpUsername": "bob",
			},
			want: "sftp: no password or key given",
		},
		{
			// Wrong source: these are the gatewayedi/claimlogic key names.
			name: "gatewayedi option keys are not the sftp key names",
			options: map[string]any{
				"gatewayEdiHost": "127.0.0.1", "gatewayEdiPort": 22,
				"gatewayEdiUsername": "bob", "gatewayEdiPassword": "secret",
			},
			want: "sftp: missing host, port, or username",
		},
		{
			name: "unsuffixed option keys are not read",
			options: map[string]any{
				"host": "127.0.0.1", "port": 22, "username": "bob", "password": "secret",
			},
			want: "sftp: missing host, port, or username",
		},
		{
			// Every documented key is present and correctly typed, but the
			// values describe no host: SetOptions accepts this map and the
			// plugin's own validation is what refuses to run.
			name: "all options present but empty",
			options: map[string]any{
				"sftpHost": "", "sftpPort": 0, "sftpUsername": "",
				"sftpPassword": "", "sftpPath": "",
			},
			want: "sftp: missing host, port, or username",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sftp{}
			if err := s.SetContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := s.SetOptions(tt.options); err != nil {
				t.Fatal(err)
			}
			err := s.Transport("payload.x12", []byte("ISA*00*"))
			if err == nil {
				t.Fatalf("Transport() with %#v returned a nil error; want %q", tt.options, tt.want)
			}
			if err.Error() != tt.want {
				t.Fatalf("Transport() error = %q; want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestSftp_SetOptions_ReportsUncoercibleOptions pins the corrected
// SetOptions contract: an option that is present but cannot be coerced into the
// field's type is reported to the caller instead of being dropped on the floor.
// The previous implementation discarded the coercion error, so the plugin
// stayed silently unconfigured and only failed much later, in Transport, with
// an error ("sftp: missing host, port, or username") that never named the
// option actually at fault.
func TestSftp_SetOptions_ReportsUncoercibleOptions(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]any
		wantKey string
	}{
		{"string option given an int", map[string]any{"sftpHost": 127}, "sftpHost"},
		{"string option given a byte slice", map[string]any{"sftpPassword": []byte("secret")}, "sftpPassword"},
		{"string option given a bool", map[string]any{"sftpUsername": true}, "sftpUsername"},
		{"int option given a string", map[string]any{"sftpPort": "22"}, "sftpPort"},
		{"int option given a float", map[string]any{"sftpPort": 22.5}, "sftpPort"},
		{"nil is not a string", map[string]any{"sftpPath": nil}, "sftpPath"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Sftp{}
			err := s.SetOptions(tt.options)
			if err == nil {
				t.Fatalf("SetOptions(%#v) = nil; want the coercion error for %q instead of silently leaving the plugin unconfigured", tt.options, tt.wantKey)
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Errorf("SetOptions(%#v) error = %q; want it to name the option %q", tt.options, err.Error(), tt.wantKey)
			}
			if !strings.Contains(err.Error(), "unable to coerce value") {
				t.Errorf("SetOptions(%#v) error = %q; want it to say the value could not be coerced", tt.options, err.Error())
			}
			if !strings.HasPrefix(err.Error(), "sftp:") {
				t.Errorf("SetOptions(%#v) error = %q; want it tagged with the sftp plugin prefix", tt.options, err.Error())
			}
		})
	}

	// A correctly typed option map is still accepted, and an absent option is
	// not an error: the field keeps its zero value and Transport reports the
	// missing configuration.
	s := &Sftp{}
	if err := s.SetOptions(map[string]any{
		"sftpHost": "127.0.0.1", "sftpPort": 22,
		"sftpUsername": "bob", "sftpPassword": "secret",
	}); err != nil {
		t.Fatalf("SetOptions() with valid, correctly typed options (sftpPath absent) error = %v; want nil", err)
	}
}

// TestSftp_Transport_FailsClosedWithoutHostKeyPolicy pins the host key policy:
// the transport verifies the peer's key against paths.known-hosts, and
// sftp-insecure-ignore-hostkey is the only way to skip that. With NEITHER
// configured the transport must refuse to connect at all - the configured
// endpoint must never see a connection - and must report a configuration error
// naming both options.
//
// This replaces
// TestSftp_Transport_DialsConfiguredHostPortButHandshakeCannotSucceed, which
// pinned the old defect: an ssh.ClientConfig with a nil HostKeyCallback, so
// ssh.Dial always aborted with "ssh: must specify HostKeyCallback" after
// connecting, and no file could ever be uploaded.
func TestSftp_Transport_FailsClosedWithoutHostKeyPolicy(t *testing.T) {
	host, port, accepted := localSSHBait(t)
	withHostKeyPolicy(t, "", false) // neither paths.known-hosts nor the opt-in

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost":     host,
		"sftpPort":     port,
		"sftpUsername": testSftpUser,
		"sftpPassword": testSftpPass,
		"sftpPath":     "/outbound",
	}); err != nil {
		t.Fatal(err)
	}

	err := s.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with neither host key option configured returned a nil error; want the fail-closed configuration error")
	}
	for _, want := range []string{
		"sftp:",
		"ssh host key verification is not configured",
		"paths.known-hosts",
		"sftp-insecure-ignore-hostkey",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Transport() error = %q; want it to contain %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "must specify HostKeyCallback") {
		t.Errorf("Transport() error = %q; the nil-HostKeyCallback defect is back", err.Error())
	}

	// The fail-closed check runs before the dial: nothing may reach the host.
	awaitNoConnection(t, accepted, 500*time.Millisecond)
	t.Logf("fail-closed error text: %v", err)
}

// TestSftp_Transport_KnownHostsVerifiesHostKeyAndUploads drives the policy's
// primary path end to end against a real, in-process SSH/SFTP server: the
// server's own host key is in paths.known-hosts, so the handshake, the
// password authentication and the SFTP upload all complete, and the payload
// really lands in the server's directory. (This is what used to be skipped as
// "requires a live SFTP server".)
func TestSftp_Transport_KnownHostsVerifiesHostKeyAndUploads(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, srv.knownHostsFile(t), false)

	payload := []byte("ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~")

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost":     srv.host,
		"sftpPort":     srv.port,
		"sftpUsername": testSftpUser,
		"sftpPassword": testSftpPass,
		"sftpPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Transport("payload.x12", payload); err != nil {
		t.Fatalf("Transport() against a known_hosts-verified server error = %v; want the upload to complete", err)
	}
	if got := srv.acceptedConnections(); got == 0 {
		t.Error("the SSH server accepted no connection; the handshake never happened")
	}
	if got := srv.uploaded(t, "payload.x12"); string(got) != string(payload) {
		t.Errorf("uploaded content = %q; want %q", got, payload)
	}
	t.Logf("real SSH handshake under paths.known-hosts: %s verified the server key, authenticated and uploaded %d bytes to %s",
		srv.address(), len(payload), srv.dir)
}

// TestSftp_Transport_KnownHostsMismatchIsRejected pins the other half of
// verifying: a known_hosts file that records a different key for the endpoint
// must make the connection fail. The server still authenticates passwords, so
// the only thing that can reject the connection is the host key check.
func TestSftp_Transport_KnownHostsMismatchIsRejected(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, srv.knownHostsFileWithDifferentKey(t), false)

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost":     srv.host,
		"sftpPort":     srv.port,
		"sftpUsername": testSftpUser,
		"sftpPassword": testSftpPass,
		"sftpPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	err := s.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with a mismatched known_hosts entry returned a nil error; the connection must be rejected")
	}
	if !strings.Contains(err.Error(), "knownhosts") && !strings.Contains(err.Error(), "host key") {
		t.Errorf("Transport() error = %q; want the host key mismatch to be reported", err.Error())
	}
	if !strings.Contains(err.Error(), "sftp: dial") {
		t.Errorf("Transport() error = %q; want the failure surfaced on the dial (the handshake is where the key is checked)", err.Error())
	}
	if !strings.Contains(err.Error(), "key mismatch") {
		t.Logf("note: host key rejection reported as %q", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(srv.dir, "payload.x12")); statErr == nil {
		t.Error("a file was uploaded over a rejected host key")
	}
}

// TestSftp_Transport_InsecureOptInDialsRealServer covers the documented bypass:
// with sftp-insecure-ignore-hostkey set (and no known_hosts file) the transport
// proceeds to a real handshake and uploads, which is what makes first contact
// and testing possible at all. The bypass must never be the default - see
// TestSftp_Transport_FailsClosedWithoutHostKeyPolicy.
func TestSftp_Transport_InsecureOptInDialsRealServer(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, "", true)

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost":     srv.host,
		"sftpPort":     srv.port,
		"sftpUsername": testSftpUser,
		"sftpPassword": testSftpPass,
		"sftpPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.Transport("payload.x12", []byte("ISA*00*")); err != nil {
		t.Fatalf("Transport() with the insecure opt-in error = %v; want the upload to complete", err)
	}
	if got := srv.uploaded(t, "payload.x12"); string(got) != "ISA*00*" {
		t.Errorf("uploaded content = %q; want %q", got, "ISA*00*")
	}
}

// TestSftp_Transport_KnownHostsWinsOverInsecureOptIn pins the precedence: when
// both options are configured the known_hosts file is used, so a mismatched key
// is still rejected even though the bypass was also set.
func TestSftp_Transport_KnownHostsWinsOverInsecureOptIn(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, srv.knownHostsFileWithDifferentKey(t), true)

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost":     srv.host,
		"sftpPort":     srv.port,
		"sftpUsername": testSftpUser,
		"sftpPassword": testSftpPass,
		"sftpPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	err := s.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with both options set and a mismatched key returned a nil error; known_hosts must win over the insecure opt-in")
	}
	if !strings.Contains(err.Error(), "sftp: dial") {
		t.Errorf("Transport() error = %q; want the rejected handshake", err.Error())
	}
}

// TestSftp_Transport_UnverifiableKnownHostsFileIsReported pins that an
// unusable known_hosts file is an error rather than a silent fallback to
// trusting the peer.
func TestSftp_Transport_UnverifiableKnownHostsFileIsReported(t *testing.T) {
	s := &Sftp{}
	withHostKeyPolicy(t, filepath.Join(t.TempDir(), "does-not-exist"), false)

	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost": "127.0.0.1", "sftpPort": 22,
		"sftpUsername": testSftpUser, "sftpPassword": testSftpPass,
		"sftpPath": "/outbound",
	}); err != nil {
		t.Fatal(err)
	}

	err := s.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with an unreadable known_hosts file returned a nil error; want a configuration error")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Errorf("Transport() error = %q; want it to name the known_hosts file it could not use", err.Error())
	}
}

// TestSftp_Transport_IgnoresUserContext documents that sftp.Transport never
// reads user.FromContext, unlike the other transports in this package: a
// context with no user is still used to push files (no identity is attached to
// the transfer). The insecure opt-in is configured only so the call gets as far
// as the dial; that path is not what this test asserts on.
func TestSftp_Transport_IgnoresUserContext(t *testing.T) {
	host, port, accepted := localSSHBait(t)
	withHostKeyPolicy(t, "", true)

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil { // deliberately no user
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost": host, "sftpPort": port,
		"sftpUsername": "bob", "sftpPassword": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() against a non-SSH listener returned a nil error; want the handshake failure")
	}
	if strings.Contains(err.Error(), "unable to retrieve user") {
		t.Fatalf("Transport() = %q; sftp now consults the user context - update this test", err.Error())
	}
	_ = awaitConnection(t, accepted)
	t.Log("pinned behaviour: sftp.Transport proceeds without a user in the context (sftp.go never calls user.FromContext)")
}

// TestSftp_Options_KeydataIsNotSettable documents CURRENT behaviour: Sftp has a
// keydata field for key-based authentication, but Options() advertises no key
// option and SetOptions never assigns it, so the key path at sftp.go:33/42-44
// is dead code and key-based auth cannot be configured at all.
func TestSftp_Options_KeydataIsNotSettable(t *testing.T) {
	for _, opt := range (&Sftp{}).Options() {
		if strings.Contains(strings.ToLower(opt), "key") {
			t.Fatalf("Options() now advertises %q; the key-auth gap was fixed and this test should be updated", opt)
		}
	}

	s := &Sftp{}
	if err := s.SetOptions(map[string]any{
		"sftpHost": "127.0.0.1", "sftpPort": 22, "sftpUsername": "bob",
		"sftpKeydata": "-----BEGIN OPENSSH PRIVATE KEY-----",
		"keydata":     "-----BEGIN OPENSSH PRIVATE KEY-----",
	}); err != nil {
		t.Fatal(err)
	}
	err := s.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() returned a nil error; want the missing-credential error")
	}
	if err.Error() != "sftp: no password or key given" {
		t.Fatalf("Transport() = %q; want %q - the keydata option is accepted by SetOptions but never stored", err.Error(), "sftp: no password or key given")
	}
}

// TestSftp_Transport_UnsupportedDataType exercises the payload type switch,
// which only runs after a real SFTP session has been opened (sftpClient.Create
// has to succeed first). The in-process server plus the insecure opt-in makes
// that reachable, so the "unsupported type" contract is no longer asserted by a
// skipped test.
func TestSftp_Transport_UnsupportedDataType(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, "", true)

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost":     srv.host,
		"sftpPort":     srv.port,
		"sftpUsername": testSftpUser,
		"sftpPassword": testSftpPass,
		"sftpPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	err := s.Transport("payload.x12", 4242)
	if err == nil {
		t.Fatal("Transport(int) = nil; want an invalid-data-type error")
	}
	if !strings.Contains(err.Error(), "sftp: invalid data type") {
		t.Errorf("Transport(int) = %q; want it to report an invalid data type", err.Error())
	}
}
