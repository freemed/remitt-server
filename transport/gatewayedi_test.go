package transport

// gatewayedi_test.go pins the gatewayedi transport contract (gatewayedi.go):
// configuration comes from the gatewayEdi* option keys, missing configuration
// fails with a descriptive error before anything is dialled, the payload type
// switch rejects unsupported input, and the connection is attempted against
// exactly the configured host:port.
//
// The Java original (GatewayEdiTransport extends SftpTransport) wraps the
// payload in a ZIP container and pushes it with the SFTP connection, which is
// skipped here with an explicit reason - see
// TestGatewayEdi_Transport_ZipContainerRequiresSftpServer.

import (
	"context"
	"strings"
	"testing"

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
			name: "wrong types are not coerced",
			options: map[string]any{
				"gatewayEdiHost": 127, "gatewayEdiPort": "22",
				"gatewayEdiUsername": 42, "gatewayEdiPassword": true,
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
// pins that the gatewayEdi* keys are what get dialled, and pins the defect that
// makes the plugin unable to deliver anything: gatewayedi.go builds an
// ssh.ClientConfig without HostKeyCallback, so ssh.Dial always fails with
// "ssh: must specify HostKeyCallback" right after connecting.
func TestGatewayEdi_Transport_DialsConfiguredHostPortButHandshakeCannotSucceed(t *testing.T) {
	host, port, accepted := localSSHBait(t)

	g := &GatewayEdi{}
	u := &model.UserModel{Username: "bob", Id: 2}
	if err := g.SetContext(user.NewContext(context.Background(), u)); err != nil {
		t.Fatal(err)
	}
	if err := g.SetOptions(map[string]any{
		"gatewayEdiHost":     host,
		"gatewayEdiPort":     port,
		"gatewayEdiUsername": "bob",
		"gatewayEdiPassword": "secret",
		"gatewayEdiPath":     "/outbound",
	}); err != nil {
		t.Fatal(err)
	}

	err := g.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() against a non-SSH listener returned a nil error; the handshake failure must be surfaced")
	}
	if !strings.Contains(err.Error(), "gatewayedi:") {
		t.Errorf("Transport() error = %q; want it to be tagged with the gatewayedi plugin prefix", err.Error())
	}
	if !strings.Contains(err.Error(), "must specify HostKeyCallback") {
		t.Fatalf("Transport() error = %q; want the HostKeyCallback failure - if the config was fixed, this test needs updating", err.Error())
	}
	if line := awaitConnection(t, accepted); line != "" {
		t.Fatalf("peer sent %q; want no SSH identification string (current behaviour: the client closes the connection before the handshake)", line)
	}
	t.Log("pinned behaviour: the configured host:port is dialled, then ssh.Dial aborts with \"ssh: must specify HostKeyCallback\" (gatewayedi.go:66-72) - no file can ever be uploaded")
}

func TestGatewayEdi_Transport_ZipContainerRequiresSftpServer(t *testing.T) {
	t.Skip("requires a live SFTP server: the ZIP container built at gatewayedi.go:55-63 is only observable on the far side of the SFTP upload (sftpClient.Create + Write at gatewayedi.go:85-93), and no in-process SFTP server is available in this suite")
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
