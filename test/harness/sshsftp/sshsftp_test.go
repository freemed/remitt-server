// Package sshsftp_test proves the harness really is a server: a real SSH
// handshake against the host key the harness reports, a real upload that lands
// in the SFTP root, and a real download of a file the test placed there. It is
// deliberately an external test package (sshsftp_test), so it can only use the
// API the harness exports - if the exported surface is not enough to drive a
// transport, these tests are where that shows up.
package sshsftp_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/test/harness/sshsftp"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// hostKeyCallback builds the verification a real deployment uses: a
// known_hosts file written by the harness, read back through knownhosts.
func hostKeyCallback(t *testing.T, srv *sshsftp.Server) ssh.HostKeyCallback {
	t.Helper()

	path, err := srv.WriteKnownHosts(t.TempDir())
	if err != nil {
		t.Fatalf("WriteKnownHosts() error = %v; want nil", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("WriteKnownHosts() returned %q but the file is not there: %v", path, err)
	}
	callback, err := knownhosts.New(path)
	if err != nil {
		t.Fatalf("knownhosts.New(%q) error = %v; want nil", path, err)
	}
	return callback
}

// dial opens a real SSH connection to the harness server, verifying the host
// key exactly as a transport does. The connection is closed with the test.
func dial(t *testing.T, srv *sshsftp.Server) *ssh.Client {
	t.Helper()

	client, err := ssh.Dial("tcp", srv.Address(), srv.SSHConfig(hostKeyCallback(t, srv)))
	if err != nil {
		t.Fatalf("ssh.Dial(%q) error = %v; want a completed handshake", srv.Address(), err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// sftpClient opens a real SFTP session over a real SSH connection.
func sftpClient(t *testing.T, srv *sshsftp.Server) *sftp.Client {
	t.Helper()

	client, err := sftp.NewClient(dial(t, srv))
	if err != nil {
		t.Fatalf("sftp.NewClient() error = %v; want a real SFTP session", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestHandshake_VerifiesHostKeyAndAuthenticates proves the first claim a test
// makes when it uses this harness: the endpoint answers, the host key the
// server presents is the one the harness reports, and the credentials the
// harness reports are the ones that work.
func TestHandshake_VerifiesHostKeyAndAuthenticates(t *testing.T) {
	srv := sshsftp.Start(t)

	client := dial(t, srv)

	if got := client.User(); got != srv.User {
		t.Errorf("authenticated as %q; want the harness credential %q", got, srv.User)
	}
	if got := srv.Connections(); got == 0 {
		t.Error("the server accepted no connection; the handshake never happened")
	}
	if got, want := srv.HostKey().Type(), "ssh-ed25519"; got != want {
		t.Errorf("host key type = %q; want %q", got, want)
	}
	if got, want := srv.Fingerprint(), ssh.FingerprintSHA256(srv.HostKey()); got != want {
		t.Errorf("Fingerprint() = %q; want the SHA256 fingerprint of HostKey() (%q)", got, want)
	}
	if !strings.HasPrefix(srv.Fingerprint(), "SHA256:") {
		t.Errorf("Fingerprint() = %q; want the ssh(1) SHA256: form an operator can compare", srv.Fingerprint())
	}
	t.Logf("real handshake: %s verified %s and authenticated as %s", srv.Address(), srv.Fingerprint(), srv.User)
}

// TestHandshake_RejectsWrongCredentials proves the server is a real
// authenticating server and not an open port: a wrong password and an unknown
// user are both refused, and the refusal is an authentication failure, not the
// host key check.
func TestHandshake_RejectsWrongCredentials(t *testing.T) {
	srv := sshsftp.Start(t)
	callback := hostKeyCallback(t, srv)

	tests := []struct {
		name     string
		user     string
		password string
	}{
		{"wrong password", srv.User, "not-the-password"},
		{"unknown user", "nobody", srv.Password},
		{"empty credentials", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := srv.SSHConfig(callback)
			config.User = tt.user
			config.Auth = []ssh.AuthMethod{ssh.Password(tt.password)}

			client, err := ssh.Dial("tcp", srv.Address(), config)
			if err == nil {
				_ = client.Close()
				t.Fatalf("ssh.Dial() with %s returned a client; the credentials must be refused", tt.name)
			}
			if !strings.Contains(err.Error(), "unable to authenticate") {
				t.Errorf("ssh.Dial() error = %q; want an authentication failure (the host key was valid, so nothing else can explain it)", err.Error())
			}
		})
	}

	// Every attempt reached the server, which is what makes the errors above
	// authentication failures rather than a failure to connect.
	if got := srv.Connections(); got < int64(len(tests)) {
		t.Errorf("Connections() = %d; want at least %d (each attempt must reach the server)", got, len(tests))
	}
}

// TestHandshake_RejectsMismatchedHostKey proves the harness is usable for host
// key policy tests: a known_hosts file that records a different key for the
// endpoint makes the connection fail, so a test can pin both halves of
// verification against the same server.
func TestHandshake_RejectsMismatchedHostKey(t *testing.T) {
	srv := sshsftp.Start(t)

	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a mismatched host key: %v", err)
	}
	otherSigner, err := ssh.NewSignerFromKey(otherPriv)
	if err != nil {
		t.Fatalf("mismatched host key signer: %v", err)
	}
	if bytes.Equal(otherSigner.PublicKey().Marshal(), srv.HostKey().Marshal()) {
		t.Fatal("the generated key equals the server's key; the test would not test a mismatch")
	}

	path := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{srv.Address()}, otherSigner.PublicKey()) + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatalf("write a mismatched known_hosts: %v", err)
	}
	callback, err := knownhosts.New(path)
	if err != nil {
		t.Fatalf("knownhosts.New(%q) error = %v", path, err)
	}

	client, err := ssh.Dial("tcp", srv.Address(), srv.SSHConfig(callback))
	if err == nil {
		_ = client.Close()
		t.Fatal("ssh.Dial() with a mismatched known_hosts entry returned a client; the server's key must be rejected")
	}
	if !strings.Contains(err.Error(), "knownhosts") && !strings.Contains(err.Error(), "key mismatch") {
		t.Errorf("ssh.Dial() error = %q; want the host key mismatch reported", err.Error())
	}
	if got := srv.Connections(); got == 0 {
		t.Error("the server accepted no connection; the handshake never happened, so the rejection proves nothing")
	}
	t.Logf("mismatched host key rejected as: %v", err)
}

// TestKnownHosts_LineNamesTheEndpointAndVerifiesTheServerKey pins the shape of
// what a caller pastes into a known_hosts file, and that it verifies the key
// the server actually presents.
func TestKnownHosts_LineNamesTheEndpointAndVerifiesTheServerKey(t *testing.T) {
	srv := sshsftp.Start(t)

	line := srv.KnownHostsLine()
	// knownhosts.Line brackets the endpoint because it carries a port; the
	// same bracketing is what knownhosts.Normalize produces for the address a
	// transport dials, which is why a transport's verification finds this line.
	if want := knownhosts.Normalize(srv.Address()); !strings.HasPrefix(line, want+" ") {
		t.Errorf("KnownHostsLine() = %q; want it to begin with the normalized endpoint %q", line, want)
	}
	if !strings.Contains(line, "[") {
		t.Errorf("KnownHostsLine() = %q; want the endpoint bracketed for a non-standard port", line)
	}
	if !strings.Contains(line, "ssh-ed25519") {
		t.Errorf("KnownHostsLine() = %q; want it to name the key type", line)
	}
	if strings.Contains(line, "\n") {
		t.Errorf("KnownHostsLine() = %q; want a single line without a terminator (WriteKnownHosts adds it)", line)
	}

	callback := hostKeyCallback(t, srv)

	if err := callback(srv.Address(), &stubAddr{}, srv.HostKey()); err != nil {
		t.Errorf("verifying the server's own key against the written file error = %v; want nil", err)
	}

	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate a different key: %v", err)
	}
	otherSigner, err := ssh.NewSignerFromKey(otherPriv)
	if err != nil {
		t.Fatalf("different key signer: %v", err)
	}
	if err := callback(srv.Address(), &stubAddr{}, otherSigner.PublicKey()); err == nil {
		t.Error("verifying a different key against the written file returned nil; the file must pin the server's key")
	}
}

// TestUpload_LandsInTheServerRoot proves a real upload: the bytes are written
// by an SFTP client over the wire and read back out of the SFTP root, both by
// name and through the uploaded-files view.
func TestUpload_LandsInTheServerRoot(t *testing.T) {
	srv := sshsftp.Start(t)
	client := sftpClient(t, srv)

	payload := []byte("ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~")

	file, err := client.Create("payload.x12")
	if err != nil {
		t.Fatalf("sftp Create() error = %v; want the file to be created on the server", err)
	}
	if _, err := file.Write(payload); err != nil {
		t.Fatalf("sftp Write() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("sftp Close() error = %v", err)
	}

	uploaded, err := srv.Uploaded("payload.x12")
	if err != nil {
		t.Fatalf("Uploaded(%q) error = %v; want the uploaded bytes", "payload.x12", err)
	}
	if !bytes.Equal(uploaded, payload) {
		t.Errorf("Uploaded(%q) = %q; want the bytes the client wrote (%q)", "payload.x12", uploaded, payload)
	}

	// A subdirectory, the way the transports write <sftpPath>/<filename>.
	if err := client.MkdirAll("outbound"); err != nil {
		t.Fatalf("sftp MkdirAll() error = %v", err)
	}
	nested, err := client.Create("outbound/payment.x12")
	if err != nil {
		t.Fatalf("sftp Create(\"outbound/payment.x12\") error = %v", err)
	}
	if _, err := nested.Write([]byte("nested")); err != nil {
		t.Fatalf("sftp Write() error = %v", err)
	}
	if err := nested.Close(); err != nil {
		t.Fatalf("sftp Close() error = %v", err)
	}

	files, err := srv.UploadedFiles()
	if err != nil {
		t.Fatalf("UploadedFiles() error = %v; want nil", err)
	}
	if len(files) != 2 {
		t.Fatalf("UploadedFiles() = %d file(s) (%v); want payload.x12 and outbound/payment.x12", len(files), keys(files))
	}
	if got := files["payload.x12"]; !bytes.Equal(got, payload) {
		t.Errorf("UploadedFiles()[\"payload.x12\"] = %q; want %q", got, payload)
	}
	if got := files["outbound/payment.x12"]; string(got) != "nested" {
		t.Errorf("UploadedFiles()[\"outbound/payment.x12\"] = %q; want %q", got, "nested")
	}

	if _, err := srv.Uploaded("never-written.x12"); err == nil {
		t.Error("Uploaded() for a file the client never wrote returned nil error; a missing file is what a silent upload failure looks like")
	}
}

// TestDownload_ComesFromTheServerRoot proves a real download: a file the test
// placed on the server is read back over SFTP, byte for byte.
func TestDownload_ComesFromTheServerRoot(t *testing.T) {
	srv := sshsftp.Start(t)

	payload := []byte("ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000002*0*P*:~")
	if err := srv.Put("inbound/remit.edi", payload); err != nil {
		t.Fatalf("Put() error = %v; want the file to be placed on the server", err)
	}
	if _, err := os.Stat(filepath.Join(srv.Dir, "inbound", "remit.edi")); err != nil {
		t.Fatalf("Put() reported success but the file is not in the SFTP root: %v", err)
	}

	client := sftpClient(t, srv)

	file, err := client.Open("inbound/remit.edi")
	if err != nil {
		t.Fatalf("sftp Open() error = %v; want the file the test placed to be readable", err)
	}
	defer file.Close()

	downloaded, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("reading the downloaded file error = %v", err)
	}
	if !bytes.Equal(downloaded, payload) {
		t.Errorf("downloaded %q; want the bytes Put() placed (%q)", downloaded, payload)
	}

	// The directory listing a scooper walks sees the same file.
	entries, err := client.ReadDir("inbound")
	if err != nil {
		t.Fatalf("sftp ReadDir(\"inbound\") error = %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "remit.edi" {
		t.Fatalf("ReadDir(\"inbound\") = %v; want exactly remit.edi", entries)
	}
	if got := entries[0].Size(); got != int64(len(payload)) {
		t.Errorf("listed size = %d; want %d", got, len(payload))
	}
}

// TestClose_RemovesItsOwnRootButKeepsACallersRoot pins the lifetime contract the
// cmd/sshsftp-harness tool depends on: WithRoot serves a directory the caller
// keeps, anything else is a temporary directory the harness cleans up.
func TestClose_RemovesItsOwnRootButKeepsACallersRoot(t *testing.T) {
	t.Run("temporary root is removed", func(t *testing.T) {
		srv, err := sshsftp.New()
		if err != nil {
			t.Fatalf("New() error = %v; want a running server", err)
		}
		dir := srv.Dir
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("the SFTP root %q is not there while the server runs: %v", dir, err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("Close() error = %v; want nil", err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("the temporary root %q still exists after Close(); want it removed", dir)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("second Close() error = %v; want nil (Close is idempotent)", err)
		}
	})

	t.Run("caller-supplied root is kept", func(t *testing.T) {
		dir := t.TempDir()
		srv, err := sshsftp.New(sshsftp.WithRoot(dir), sshsftp.WithCredentials("bob", "secret"))
		if err != nil {
			t.Fatalf("New(WithRoot) error = %v; want a running server", err)
		}
		if srv.Dir != dir {
			t.Errorf("Dir = %q; want the root the caller named (%q)", srv.Dir, dir)
		}
		if srv.User != "bob" || srv.Password != "secret" {
			t.Errorf("credentials = %q/%q; want the ones WithCredentials set", srv.User, srv.Password)
		}
		if err := srv.Put("kept.txt", []byte("mine")); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		if err := srv.Close(); err != nil {
			t.Errorf("Close() error = %v; want nil", err)
		}
		content, err := os.ReadFile(filepath.Join(dir, "kept.txt"))
		if err != nil {
			t.Fatalf("the caller's root was disturbed by Close(): %v", err)
		}
		if string(content) != "mine" {
			t.Errorf("kept.txt = %q; want %q", content, "mine")
		}
	})

	t.Run("a stopped server is gone", func(t *testing.T) {
		srv, err := sshsftp.New()
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		address := srv.Address()
		if err := srv.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		conn, err := net.Dial("tcp", address)
		if err == nil {
			_ = conn.Close()
			t.Fatalf("dialling %s still connects after Close(); the listener must be shut", address)
		}
	})
}

// stubAddr is the net.Addr a HostKeyCallback is handed; the knownhosts callback
// reads its String() and ignores the rest.
type stubAddr struct{}

func (stubAddr) Network() string { return "tcp" }
func (stubAddr) String() string  { return "127.0.0.1:0" }

// keys sorts an uploaded-files map for failure messages.
func keys(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for name := range files {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
