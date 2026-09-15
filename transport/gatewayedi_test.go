package transport

// gatewayedi_test.go pins the gatewayedi transport contract (gatewayedi.go):
// configuration comes from the gatewayEdi* option keys, missing configuration
// fails with a descriptive error before anything is dialled, an option that
// cannot be coerced is reported by SetOptions, the payload type switch rejects
// unsupported input, and the connection is attempted against exactly the
// configured host:port.
//
// The Java original (GatewayEdiTransport extends SftpTransport) wraps the
// payload in a ZIP container and pushes it with the SFTP connection. That path
// is exercised for real against an in-process SSH/SFTP server (see
// startSftpTestServer in sftp_test.go), under both host key policies: the
// known_hosts file for this server, and the explicit insecure opt-in.

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

func TestGatewayEdi_Transport_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]any
		want    string
	}{
		{
			name:    "no options at all",
			options: map[string]any{},
			want:    "gatewayedi: missing host, port, or username",
		},
		{
			name: "host and port, no username",
			options: map[string]any{
				"gatewayEdiHost": "127.0.0.1", "gatewayEdiPort": 22,
			},
			want: "gatewayedi: missing host, port, or username",
		},
		{
			name: "no password",
			options: map[string]any{
				"gatewayEdiHost": "127.0.0.1", "gatewayEdiPort": 22, "gatewayEdiUsername": "bob",
			},
			want: "gatewayedi: missing password",
		},
		{
			// Wrong source: sftp/claimlogic key names must not populate gatewayedi.
			name: "sftp option keys are not read",
			options: map[string]any{
				"sftpHost": "127.0.0.1", "sftpPort": 22,
				"sftpUsername": "bob", "sftpPassword": "secret", "sftpPath": "/out",
			},
			want: "gatewayedi: missing host, port, or username",
		},
		{
			name: "claimlogic option keys are not read",
			options: map[string]any{
				"claimlogicHost": "127.0.0.1", "claimlogicPort": 22,
				"claimlogicUsername": "bob", "claimlogicPassword": "secret",
			},
			want: "gatewayedi: missing host, port, or username",
		},
		{
			// Every documented key is present and correctly typed, but the
			// values describe no host: SetOptions accepts this map and the
			// plugin's own validation is what refuses to run.
			name: "all options present but empty",
			options: map[string]any{
				"gatewayEdiHost": "", "gatewayEdiPort": 0, "gatewayEdiUsername": "",
				"gatewayEdiPassword": "", "gatewayEdiPath": "",
			},
			want: "gatewayedi: missing host, port, or username",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GatewayEdi{}
			if err := g.SetContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := g.SetOptions(tt.options); err != nil {
				t.Fatal(err)
			}
			err := g.Transport("payload.x12", []byte("ISA*00*"))
			if err == nil {
				t.Fatalf("Transport() with %#v returned a nil error; want %q", tt.options, tt.want)
			}
			if err.Error() != tt.want {
				t.Fatalf("Transport() error = %q; want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestGatewayEdi_Transport_RequiresUserInContext(t *testing.T) {
	g := &GatewayEdi{}
	if err := g.SetContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.SetOptions(map[string]any{
		"gatewayEdiHost": "127.0.0.1", "gatewayEdiPort": 22,
		"gatewayEdiUsername": "bob", "gatewayEdiPassword": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	err := g.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() without a user in the context returned a nil error; want an error")
	}
	if err.Error() != "gatewayedi: unable to retrieve user from context" {
		t.Fatalf("Transport() = %q; want %q", err.Error(), "gatewayedi: unable to retrieve user from context")
	}
}

// TestGatewayEdi_Transport_RejectsUnsupportedPayloadType proves the type check
// happens BEFORE any network activity: configuration and user are valid, the
// host is an unbound loopback port, and the call still fails on the payload
// type instead of dialling.
func TestGatewayEdi_Transport_RejectsUnsupportedPayloadType(t *testing.T) {
	g := &GatewayEdi{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := g.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetOptions(map[string]any{
		"gatewayEdiHost": "127.0.0.1", "gatewayEdiPort": 1,
		"gatewayEdiUsername": "bob", "gatewayEdiPassword": "secret",
	}); err != nil {
		t.Fatal(err)
	}
	err := g.Transport("payload.x12", 4242)
	if err == nil {
		t.Fatal("Transport(int) = nil; want an error")
	}
	if !strings.Contains(err.Error(), "gatewayedi: invalid data type") {
		t.Fatalf("Transport(int) = %q; want it to report an invalid data type (and to have failed before dialling)", err.Error())
	}
}

// TestGatewayEdi_Transport_DialsConfiguredHostPortButHandshakeCannotSucceed
// pinned the defect that made the plugin unable to deliver anything: an
// ssh.ClientConfig without HostKeyCallback, so ssh.Dial always failed with
// "ssh: must specify HostKeyCallback" right after connecting.
//
// It is replaced by the two tests below: the corrected fail-closed contract
// when no host key policy is configured, and a real handshake/upload when one
// is.
//
// TestGatewayEdi_Transport_FailsClosedWithoutHostKeyPolicy pins the fail-closed
// half: with neither paths.known-hosts nor sftp-insecure-ignore-hostkey
// configured the plugin must report a configuration error naming both options
// and must not connect to the configured endpoint at all.
func TestGatewayEdi_Transport_FailsClosedWithoutHostKeyPolicy(t *testing.T) {
	host, port, accepted := localSSHBait(t)
	withHostKeyPolicy(t, "", false)

	g := &GatewayEdi{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := g.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetOptions(map[string]any{
		"gatewayEdiHost":     host,
		"gatewayEdiPort":     port,
		"gatewayEdiUsername": testSftpUser,
		"gatewayEdiPassword": testSftpPass,
		"gatewayEdiPath":     "/outbound",
	}); err != nil {
		t.Fatal(err)
	}

	err := g.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with neither host key option configured returned a nil error; want the fail-closed configuration error")
	}
	for _, want := range []string{
		"gatewayedi:",
		"ssh host key verification is not configured",
		"paths.known-hosts",
		"sftp-insecure-ignore-hostkey",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Transport() error = %q; want it to contain %q", err.Error(), want)
		}
	}
	// The ZIP container is built before the dial, so the error must be the
	// host key policy - and nothing may reach the endpoint.
	awaitNoConnection(t, accepted, 500*time.Millisecond)
}

// TestGatewayEdi_Transport_KnownHostsVerifiesHostKeyAndUploads drives the
// policy's primary path against a real, in-process SSH/SFTP server: the
// server's own host key is in paths.known-hosts, so the handshake, the
// authentication and the ZIP upload all complete.
func TestGatewayEdi_Transport_KnownHostsVerifiesHostKeyAndUploads(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, srv.knownHostsFile(t), false)

	payload := []byte("ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~")

	g := &GatewayEdi{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := g.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetOptions(map[string]any{
		"gatewayEdiHost":     srv.host,
		"gatewayEdiPort":     srv.port,
		"gatewayEdiUsername": testSftpUser,
		"gatewayEdiPassword": testSftpPass,
		"gatewayEdiPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	if err := g.Transport("payload.x12", payload); err != nil {
		t.Fatalf("Transport() against a known_hosts-verified server error = %v; want the upload to complete", err)
	}
	if got := srv.acceptedConnections(); got == 0 {
		t.Error("the SSH server accepted no connection; the handshake never happened")
	}

	// The uploaded file is the ZIP container, and it holds the payload.
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

// TestGatewayEdi_Transport_KnownHostsMismatchIsRejected pins that a known_hosts
// entry that does not match the server's key rejects the connection.
func TestGatewayEdi_Transport_KnownHostsMismatchIsRejected(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, srv.knownHostsFileWithDifferentKey(t), false)

	g := &GatewayEdi{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := g.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetOptions(map[string]any{
		"gatewayEdiHost":     srv.host,
		"gatewayEdiPort":     srv.port,
		"gatewayEdiUsername": testSftpUser,
		"gatewayEdiPassword": testSftpPass,
		"gatewayEdiPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	err := g.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with a mismatched known_hosts entry returned a nil error; the connection must be rejected")
	}
	if !strings.Contains(err.Error(), "gatewayedi: ssh dial") {
		t.Errorf("Transport() error = %q; want the rejected handshake surfaced on the dial", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(srv.dir, "payload.x12.zip")); statErr == nil {
		t.Error("a file was uploaded over a rejected host key")
	}
}

// TestGatewayEdi_Transport_InsecureOptInDialsRealServer covers the explicit
// bypass: with sftp-insecure-ignore-hostkey set the plugin reaches a real
// handshake and uploads.
func TestGatewayEdi_Transport_InsecureOptInDialsRealServer(t *testing.T) {
	srv := startSftpTestServer(t)
	withHostKeyPolicy(t, "", true)

	g := &GatewayEdi{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := g.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetOptions(map[string]any{
		"gatewayEdiHost":     srv.host,
		"gatewayEdiPort":     srv.port,
		"gatewayEdiUsername": testSftpUser,
		"gatewayEdiPassword": testSftpPass,
		"gatewayEdiPath":     srv.dir,
	}); err != nil {
		t.Fatal(err)
	}

	if err := g.Transport("payload.x12", []byte("ISA*00*")); err != nil {
		t.Fatalf("Transport() with the insecure opt-in error = %v; want the upload to complete", err)
	}
	if _, err := os.Stat(filepath.Join(srv.dir, "payload.x12.zip")); err != nil {
		t.Errorf("the server did not receive the payload archive: %v", err)
	}
}

// TestGatewayEdi_SetOptions_ReportsUncoercibleOptions pins the corrected
// SetOptions contract: an option present with the wrong type is reported
// instead of silently leaving the plugin unconfigured (the previous
// implementation discarded the coercion error).
func TestGatewayEdi_SetOptions_ReportsUncoercibleOptions(t *testing.T) {
	tests := []struct {
		name    string
		options map[string]any
		wantKey string
	}{
		{"string option given an int", map[string]any{"gatewayEdiHost": 127}, "gatewayEdiHost"},
		{"string option given a bool", map[string]any{"gatewayEdiPassword": true}, "gatewayEdiPassword"},
		{"int option given a string", map[string]any{"gatewayEdiPort": "22"}, "gatewayEdiPort"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GatewayEdi{}
			err := g.SetOptions(tt.options)
			if err == nil {
				t.Fatalf("SetOptions(%#v) = nil; want the coercion error for %q", tt.options, tt.wantKey)
			}
			if !strings.Contains(err.Error(), tt.wantKey) || !strings.Contains(err.Error(), "unable to coerce value") {
				t.Errorf("SetOptions(%#v) error = %q; want it to name %q and the failed coercion", tt.options, err.Error(), tt.wantKey)
			}
			if !strings.HasPrefix(err.Error(), "gatewayedi:") {
				t.Errorf("SetOptions(%#v) error = %q; want it tagged with the gatewayedi plugin prefix", tt.options, err.Error())
			}
		})
	}
}

func TestGatewayEdi_Contract_Surface(t *testing.T) {
	g := &GatewayEdi{}
	if got := g.InputFormat(); got != "x12" {
		t.Errorf("InputFormat() = %q; want %q", got, "x12")
	}
	want := []string{"gatewayEdiHost", "gatewayEdiPort", "gatewayEdiUsername", "gatewayEdiPassword", "gatewayEdiPath"}
	got := g.Options()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Options() = %v; want %v", got, want)
	}
	m, err := InstantiateTransporter("gatewayedi")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.(*GatewayEdi); !ok {
		t.Fatalf("InstantiateTransporter(\"gatewayedi\") = %T; want *GatewayEdi", m)
	}
}
