package transport

// claimlogic_test.go pins the claimlogic transport contract (claimlogic.go):
// it takes the user from the context, accepts only string/[]byte payloads,
// wraps the payload in a ZIP container and pushes it to the configured
// claimlogic* host:port over SFTP.
//
// Two behaviours are pinned because they differ from the sibling SFTP plugins:
//   - ClaimLogic performs NO up-front configuration validation (gatewayedi and
//     sftp both do), so an unconfigured plugin attempts a dial to ":0". An
//     option that is present with the wrong type IS reported, by SetOptions.
//   - The Java original reads remitt.transport.claimlogic.{host,port,path}
//     from configuration; the Go port reads per-user plugin options instead.
//
// Host key verification follows the same policy as the sibling transports
// (paths.known-hosts, or the explicit sftp-insecure-ignore-hostkey opt-in; see
// common.HostKeyCallback): the tests that only care about the dial target
// configure the opt-in, and the ZIP + upload path is exercised for real against
// the in-process SSH/SFTP server in sftp_test.go.

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

func TestClaimLogic_Transport_RequiresUserInContext(t *testing.T) {
	c := &ClaimLogic{}
	if err := c.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{}); err != nil {
		t.Fatal(err)
	}
	err := c.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() without a user in the context returned a nil error; want an error")
	}
	if err.Error() != "claimlogic: unable to retrieve user from context" {
		t.Fatalf("Transport() = %q; want %q", err.Error(), "claimlogic: unable to retrieve user from context")
	}
}

// TestClaimLogic_Transport_RejectsUnsupportedPayloadType proves claimlogic
// rejects a non-string payload BEFORE it dials: the type switch runs before the
// ZIP and SSH work, so an unsupported type fails with no configuration present
// at all.
func TestClaimLogic_Transport_RejectsUnsupportedPayloadType(t *testing.T) {
	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{}); err != nil {
		t.Fatal(err)
	}
	err := c.Transport("payload.x12", map[string]string{"nope": "x"})
	if err == nil {
		t.Fatal("Transport(map) = nil; want an error")
	}
	if !strings.Contains(err.Error(), "claimlogic: invalid data type") {
		t.Fatalf("Transport(map) = %q; want it to report an invalid data type", err.Error())
	}
}

// TestClaimLogic_Transport_MissingConfigurationDialsEmptyAddress documents
// CURRENT behaviour: unlike sftp and gatewayedi, ClaimLogic validates nothing,
// so with empty (but correctly typed) options it zips the payload and dials
// ":0" (host "" and port 0) instead of failing with a configuration error.
//
// The insecure host key opt-in is configured because this test is about the
// dial target, not about verification: without a policy the transport would
// fail closed before dialling.
func TestClaimLogic_Transport_MissingConfigurationDialsEmptyAddress(t *testing.T) {
	withHostKeyPolicy(t, "", true)

	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{
		"claimlogicHost": "", "claimlogicPort": 0,
		"claimlogicUsername": "", "claimlogicPassword": "", "claimlogicPath": "",
	}); err != nil {
		t.Fatal(err)
	}
	err := c.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with no configuration returned a nil error; want a dial failure")
	}
	if !strings.Contains(err.Error(), "claimlogic: ssh dial") {
		t.Fatalf("Transport() = %q; want the ssh dial failure (there is no configuration validation)", err.Error())
	}
	if strings.Contains(err.Error(), "missing host") || strings.Contains(err.Error(), "missing password") {
		t.Fatalf("Transport() = %q; claimlogic now validates configuration - update this test", err.Error())
	}
	if !strings.Contains(err.Error(), ":0") {
		t.Fatalf("Transport() = %q; want the dial target to be the empty host and port 0", err.Error())
	}
}

// TestClaimLogic_Transport_DialsConfiguredHostPortButHandshakeCannotSucceed
// pinned the defect that made the plugin unable to deliver anything: an
// ssh.ClientConfig without HostKeyCallback, so ssh.Dial always failed with
// "ssh: must specify HostKeyCallback" right after connecting. It is replaced by
// the tests below.
//
// TestClaimLogic_Transport_FailsClosedWithoutHostKeyPolicy pins the fail-closed
// half: with neither paths.known-hosts nor sftp-insecure-ignore-hostkey
// configured the plugin reports a configuration error naming both options - and
// it does so before dialling, so the configured endpoint sees no connection.
func TestClaimLogic_Transport_FailsClosedWithoutHostKeyPolicy(t *testing.T) {
	host, port, accepted := localSSHBait(t)
	withHostKeyPolicy(t, "", false)

	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{
		"claimlogicHost":     host,
		"claimlogicPort":     port,
		"claimlogicUsername": testSftpUser,
		"claimlogicPassword": testSftpPass,
		"claimlogicPath":     "/outbound",
	}); err != nil {
		t.Fatal(err)
	}

	err := c.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with neither host key option configured returned a nil error; want the fail-closed configuration error")
	}
	for _, want := range []string{
		"claimlogic:",
		"ssh host key verification is not configured",
		"paths.known-hosts",
		"sftp-insecure-ignore-hostkey",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Transport() error = %q; want it to contain %q", err.Error(), want)
		}
	}
	awaitNoConnection(t, accepted, 500*time.Millisecond)
}

// TestClaimLogic_Transport_KnownHostsVerifiesHostKeyAndUploads drives the
// policy's primary path against a real, in-process SSH/SFTP server: the
// server's own host key is in paths.known-hosts, so the handshake, the
// authentication and the ZIP upload all complete.
func TestClaimLogic_Transport_KnownHostsVerifiesHostKeyAndUploads(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, srv.knownHostsFile(t), false)

	payload := []byte("ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~")

	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{
		"claimlogicHost":     srv.host,
		"claimlogicPort":     srv.port,
		"claimlogicUsername": testSftpUser,
		"claimlogicPassword": testSftpPass,
		"claimlogicPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	if err := c.Transport("payload.x12", payload); err != nil {
		t.Fatalf("Transport() against a known_hosts-verified server error = %v; want the upload to complete", err)
	}
	if got := srv.acceptedConnections(); got == 0 {
		t.Error("the SSH server accepted no connection; the handshake never happened")
	}

	zipped := srv.uploaded(t, "payload.x12.zip")
	zr, err := zip.NewReader(bytes.NewReader(zipped), int64(len(zipped)))
	if err != nil {
		t.Fatalf("the uploaded file is not a readable ZIP archive: %v", err)
	}
	if len(zr.File) != 1 {
		t.Fatalf("the uploaded ZIP holds %d entries; want 1", len(zr.File))
	}
	rc, err := zr.File[0].Open()
	if err != nil {
		t.Fatalf("open the ZIP entry: %v", err)
	}
	defer rc.Close()
	unzipped, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read the ZIP entry: %v", err)
	}
	if string(unzipped) != string(payload) {
		t.Errorf("the uploaded ZIP contains %q; want the payload %q", unzipped, payload)
	}
	t.Logf("real SSH handshake under paths.known-hosts: %s verified the server key, authenticated and uploaded the ZIP container to %s",
		srv.address(), srv.dir)
}

// TestClaimLogic_Transport_KnownHostsMismatchIsRejected pins that a known_hosts
// entry that does not match the server's key rejects the connection, with
// nothing uploaded.
func TestClaimLogic_Transport_KnownHostsMismatchIsRejected(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, srv.knownHostsFileWithDifferentKey(t), false)

	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{
		"claimlogicHost":     srv.host,
		"claimlogicPort":     srv.port,
		"claimlogicUsername": testSftpUser,
		"claimlogicPassword": testSftpPass,
		"claimlogicPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	err := c.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with a mismatched known_hosts entry returned a nil error; the connection must be rejected")
	}
	if !strings.Contains(err.Error(), "claimlogic: ssh dial") {
		t.Errorf("Transport() error = %q; want the rejected handshake surfaced on the dial", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(srv.dir, "payload.x12.zip")); statErr == nil {
		t.Error("a file was uploaded over a rejected host key")
	}
}

// TestClaimLogic_Transport_InsecureOptInDialsRealServer covers the explicit
// bypass: with sftp-insecure-ignore-hostkey set the plugin reaches a real
// handshake and uploads.
func TestClaimLogic_Transport_InsecureOptInDialsRealServer(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, "", true)

	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{
		"claimlogicHost":     srv.host,
		"claimlogicPort":     srv.port,
		"claimlogicUsername": testSftpUser,
		"claimlogicPassword": testSftpPass,
		"claimlogicPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	if err := c.Transport("payload.x12", []byte("ISA*00*")); err != nil {
		t.Fatalf("Transport() with the insecure opt-in error = %v; want the upload to complete", err)
	}
	if _, err := os.Stat(filepath.Join(srv.dir, "payload.x12.zip")); err != nil {
		t.Errorf("the server did not receive the payload archive: %v", err)
	}
}

// TestClaimLogic_SetOptions_ReportsUncoercibleOptions pins the corrected
// SetOptions contract: an option present with the wrong type is reported
// instead of silently leaving the plugin unconfigured (the previous
// implementation discarded the coercion error, and ClaimLogic has no other
// configuration validation to catch it).
func TestClaimLogic_SetOptions_ReportsUncoercibleOptions(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]any
		wantKey string
	}{
		{"string option given an int", map[string]any{"claimlogicHost": 127}, "claimlogicHost"},
		{"string option given a bool", map[string]any{"claimlogicPassword": true}, "claimlogicPassword"},
		{"int option given a string", map[string]any{"claimlogicPort": "22"}, "claimlogicPort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &ClaimLogic{}
			err := c.SetOptions(tt.options)
			if err == nil {
				t.Fatalf("SetOptions(%#v) = nil; want the coercion error for %q", tt.options, tt.wantKey)
			}
			if !strings.Contains(err.Error(), tt.wantKey) || !strings.Contains(err.Error(), "unable to coerce value") {
				t.Errorf("SetOptions(%#v) error = %q; want it to name %q and the failed coercion", tt.options, err.Error(), tt.wantKey)
			}
			if !strings.HasPrefix(err.Error(), "claimlogic:") {
				t.Errorf("SetOptions(%#v) error = %q; want it tagged with the claimlogic plugin prefix", tt.options, err.Error())
			}
		})
	}
}

// TestClaimLogic_Transport_IgnoresOtherPluginsOptionKeys pins that the
// claimlogic* key names (not sftp*/gatewayEdi*) are the configuration source:
// options published under another plugin's names leave the plugin unconfigured.
func TestClaimLogic_Transport_IgnoresOtherPluginsOptionKeys(t *testing.T) {
	withHostKeyPolicy(t, "", true)

	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{
		"sftpHost": "127.0.0.1", "sftpPort": 22,
		"sftpUsername": "bob", "sftpPassword": "secret",
		"gatewayEdiHost": "127.0.0.1", "gatewayEdiPort": 22,
		"claimLogicHost": "typo-cased-key", "claimlogicport": 22,
	}); err != nil {
		t.Fatal(err)
	}
	err := c.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() returned a nil error; want the dial failure against the empty address")
	}
	if !strings.Contains(err.Error(), ":0") {
		t.Fatalf("Transport() = %q; want the dial target to stay empty (options only come from the exact claimlogic* key names)", err.Error())
	}
	if strings.Contains(err.Error(), "typo-cased-key") {
		t.Fatalf("Transport() = %q; a differently-cased key was accepted - key matching is now case-insensitive", err.Error())
	}
}

// TestClaimLogic_Transport_ZipAndUploadRequiresSftpServer used to skip for want
// of a server. The ZIP container and the upload are now exercised for real:
// TestClaimLogic_Transport_KnownHostsVerifiesHostKeyAndUploads verifies both
// against the in-process SSH/SFTP server, so the skip is gone.

func TestClaimLogic_Contract_Surface(t *testing.T) {
	c := &ClaimLogic{}
	if got := c.InputFormat(); got != "x12" {
		t.Errorf("InputFormat() = %q; want %q", got, "x12")
	}
	want := []string{"claimlogicHost", "claimlogicPort", "claimlogicUsername", "claimlogicPassword", "claimlogicPath"}
	got := c.Options()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Options() = %v; want %v", got, want)
	}
	m, err := InstantiateTransporter("claimlogic")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*ClaimLogic); !ok {
		t.Fatalf("InstantiateTransporter(\"claimlogic\") = %T; want *ClaimLogic", m)
	}
}
