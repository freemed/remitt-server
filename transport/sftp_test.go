package transport

// sftp_test.go pins the sftp transport contract (sftp.go): configuration is
// read from the documented option keys (sftpUsername/sftpPassword/sftpHost/
// sftpPort/sftpPath), the plugin refuses to run with missing configuration,
// and the connection is attempted against exactly the host and port it was
// configured with. The reachable-but-not-SSH listener below is a loopback test
// double (it never speaks SSH), so nothing here talks to a real SFTP host.
//
// Uploading a file to a real SFTP server (sftpClient.Create + Write) is skipped
// with an explicit reason - see TestSftp_Transport_LiveUploadRequiresSftpServer.

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
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
			name: "wrong types are not coerced",
			options: map[string]any{
				"sftpHost": 127, "sftpPort": "22", "sftpUsername": 42, "sftpPassword": []byte("x"),
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

// TestSftp_Transport_DialsConfiguredHostPortButHandshakeCannotSucceed pins the
// configuration source (sftpHost/sftpPort are what get dialled) AND the defect
// that makes these transports unable to upload anything: sftp.go builds an
// ssh.ClientConfig with only User, Auth and Timeout, so HostKeyCallback is nil
// and ssh.Dial always fails with "ssh: must specify HostKeyCallback" after
// connecting (x/crypto/ssh/client.go:74-76).
func TestSftp_Transport_DialsConfiguredHostPortButHandshakeCannotSucceed(t *testing.T) {
	host, port, accepted := localSSHBait(t)

	s := &Sftp{}
	if err := s.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOptions(map[string]any{
		"sftpHost":     host,
		"sftpPort":     port,
		"sftpUsername": "bob",
		"sftpPassword": "secret",
		"sftpPath":     "/outbound",
	}); err != nil {
		t.Fatal(err)
	}

	err := s.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() against a non-SSH listener returned a nil error; the handshake failure must be surfaced")
	}
	if !strings.Contains(err.Error(), "sftp:") {
		t.Errorf("Transport() error = %q; want it to be tagged with the sftp plugin prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "must specify HostKeyCallback") {
		t.Fatalf("Transport() error = %q; want the HostKeyCallback failure - if the config was fixed, this test needs updating", err.Error())
	}
	if line := awaitConnection(t, accepted); line != "" {
		t.Fatalf("peer sent %q; want no SSH identification string (current behaviour: the client closes the connection before the handshake)", line)
	}
	t.Log("pinned behaviour: the configured host:port is dialled, then ssh.Dial aborts with \"ssh: must specify HostKeyCallback\" (sftp.go:37-46) - no file can ever be uploaded")
}

// TestSftp_Transport_IgnoresUserContext documents that sftp.Transport never
// reads user.FromContext, unlike the other transports in this package: a
// context with no user is still used to push files (no identity is attached to
// the transfer).
func TestSftp_Transport_IgnoresUserContext(t *testing.T) {
	host, port, accepted := localSSHBait(t)

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
		t.Fatal("Transport() returned a nil error; want the handshake failure")
	}
	if strings.Contains(err.Error(), "unable to retrieve user") {
		t.Fatalf("Transport() = %q; sftp now consults the user context - update this test", err.Error())
	}
	_ = awaitConnection(t, accepted)
	t.Log("pinned behaviour: sftp.Transport proceeds without a user in the context (sftp.go:28-46 never calls user.FromContext)")
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

func TestSftp_Transport_LiveUploadRequiresSftpServer(t *testing.T) {
	t.Skip("requires a live SFTP server: the upload path (sftpClient.Create + Write at sftp.go:57-69) only completes against a real SSH/SFTP endpoint, and no in-process SFTP server is available in this suite")
}

func TestSftp_Transport_UnsupportedDataType(t *testing.T) {
	t.Skip("requires a live SFTP server: the payload type switch at sftp.go:62-69 runs only after sftpClient.Create has succeeded on a real server, so an unsupported type cannot be reached without one")
}
