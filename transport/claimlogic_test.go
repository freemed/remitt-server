package transport

// claimlogic_test.go pins the claimlogic transport contract (claimlogic.go):
// it takes the user from the context, accepts only string/[]byte payloads,
// wraps the payload in a ZIP container and pushes it to the configured
// claimlogic* host:port over SFTP.
//
// Two behaviours are pinned because they differ from the sibling SFTP plugins:
//   - ClaimLogic performs NO up-front configuration validation (gatewayedi and
//     sftp both do), so an unconfigured plugin attempts a dial to ":0".
//   - The Java original reads remitt.transport.claimlogic.{host,port,path}
//     from configuration; the Go port reads per-user plugin options instead.
//
// The ZIP + upload path needs a live SFTP server and is skipped with a reason.

import (
	"context"
	"strings"
	"testing"

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
// so with empty options it zips the payload and dials ":0" (host "" and port
// 0) instead of failing with a configuration error.
func TestClaimLogic_Transport_MissingConfigurationDialsEmptyAddress(t *testing.T) {
	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{}); err != nil {
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
// pins that the claimlogic* keys are what get dialled, and pins the defect that
// makes the plugin unable to deliver anything: claimlogic.go builds an
// ssh.ClientConfig without HostKeyCallback, so ssh.Dial always fails with
// "ssh: must specify HostKeyCallback" right after connecting.
func TestClaimLogic_Transport_DialsConfiguredHostPortButHandshakeCannotSucceed(t *testing.T) {
	host, port, accepted := localSSHBait(t)

	c := &ClaimLogic{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := c.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := c.SetOptions(map[string]any{
		"claimlogicHost":     host,
		"claimlogicPort":     port,
		"claimlogicUsername": "bob",
		"claimlogicPassword": "secret",
		"claimlogicPath":     "/outbound",
	}); err != nil {
		t.Fatal(err)
	}

	err := c.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() against a non-SSH listener returned a nil error; the handshake failure must be surfaced")
	}
	if !strings.Contains(err.Error(), "claimlogic:") {
		t.Errorf("Transport() error = %q; want it to be tagged with the claimlogic plugin prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "must specify HostKeyCallback") {
		t.Fatalf("Transport() error = %q; want the HostKeyCallback failure - if the config was fixed, this test needs updating", err.Error())
	}
	if line := awaitConnection(t, accepted); line != "" {
		t.Fatalf("peer sent %q; want no SSH identification string (current behaviour: the client closes the connection before the handshake)", line)
	}
	t.Log("pinned behaviour: the configured host:port is dialled, then ssh.Dial aborts with \"ssh: must specify HostKeyCallback\" (claimlogic.go:56-65) - no file can ever be uploaded")
}

// TestClaimLogic_Transport_IgnoresOtherPluginsOptionKeys pins that the
// claimlogic* key names (not sftp*/gatewayEdi*) are the configuration source:
// options published under another plugin's names leave the plugin unconfigured.
func TestClaimLogic_Transport_IgnoresOtherPluginsOptionKeys(t *testing.T) {
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

func TestClaimLogic_Transport_ZipAndUploadRequiresSftpServer(t *testing.T) {
	t.Skip("requires a live SFTP server: the ZIP container (claimlogic.go:44-53) and the file upload (claimlogic.go:68-83) only complete against a real SSH/SFTP endpoint, and no in-process SFTP server is available in this suite")
}

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
