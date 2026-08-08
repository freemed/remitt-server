package transport

import (
	"context"
	"fmt"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/model/user"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func init() {
	RegisterTransporter("gatewayedi", func() Transporter { return &GatewayEdi{} })
}

// GatewayEdi represents a transport which wraps payloads in a ZIP
// archive and pushes them via SFTP to a remote host.
type GatewayEdi struct {
	host     string
	port     int
	username string
	password string
	path     string
	ctx      context.Context
}

// Transport performs the actual work of transport, given the input.
func (g *GatewayEdi) Transport(filename string, data any) error {
	if g.host == "" || g.port == 0 || g.username == "" {
		return fmt.Errorf("gatewayedi: missing host, port, or username")
	}
	if g.password == "" {
		return fmt.Errorf("gatewayedi: missing password")
	}

	_, ok := user.FromContext(g.ctx)
	if !ok {
		return fmt.Errorf("gatewayedi: unable to retrieve user from context")
	}

	// Convert data to bytes
	var payload []byte
	switch d := data.(type) {
	case string:
		payload = []byte(d)
	case []byte:
		payload = d
	default:
		return fmt.Errorf("gatewayedi: invalid data type %T", data)
	}

	// ZIP the payload
	izw := common.NewInternalZipWriter()
	zipName := fmt.Sprintf("%d.x12", time.Now().Unix())
	if err := izw.Store(zipName, payload); err != nil {
		return fmt.Errorf("gatewayedi: zip store: %w", err)
	}
	zippedData, err := izw.GetData()
	if err != nil {
		return fmt.Errorf("gatewayedi: zip getdata: %w", err)
	}

	// SFTP upload
	sshConfig := &ssh.ClientConfig{
		User:    g.username,
		Auth:    []ssh.AuthMethod{ssh.Password(g.password)},
		Timeout: 10 * time.Second,
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", g.host, g.port), sshConfig)
	if err != nil {
		return fmt.Errorf("gatewayedi: ssh dial: %w", err)
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("gatewayedi: sftp client: %w", err)
	}
	defer sftpClient.Close()

	remotePath := fmt.Sprintf("%s/%s.zip", g.path, filename)
	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("gatewayedi: sftp create: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(zippedData); err != nil {
		return fmt.Errorf("gatewayedi: sftp write: %w", err)
	}

	return nil
}

// InputFormat returns the input format required by this plugin.
func (g *GatewayEdi) InputFormat() string {
	return "x12"
}

// Options returns a list of valid options for this transporter type
func (g *GatewayEdi) Options() []string {
	return []string{"gatewayEdiHost", "gatewayEdiPort", "gatewayEdiUsername", "gatewayEdiPassword", "gatewayEdiPath"}
}

// SetOptions sets the current options for this plugin
func (g *GatewayEdi) SetOptions(o map[string]any) error {
	g.host, _ = g.coerceOptionString(o, "gatewayEdiHost")
	g.port, _ = g.coerceOptionInt(o, "gatewayEdiPort")
	g.username, _ = g.coerceOptionString(o, "gatewayEdiUsername")
	g.password, _ = g.coerceOptionString(o, "gatewayEdiPassword")
	g.path, _ = g.coerceOptionString(o, "gatewayEdiPath")
	return nil
}

// SetContext sets the context in which this executes
func (g *GatewayEdi) SetContext(c context.Context) error {
	g.ctx = c
	return nil
}

func (g *GatewayEdi) coerceOptionString(o map[string]any, keyname string) (string, error) {
	x, ok := o[keyname]
	if !ok {
		return "", fmt.Errorf("unable to read option for '%s'", keyname)
	}
	y, ok := x.(string)
	if !ok {
		return "", fmt.Errorf("unable to coerce value for '%s'", keyname)
	}
	return y, nil
}

func (g *GatewayEdi) coerceOptionInt(o map[string]any, keyname string) (int, error) {
	x, ok := o[keyname]
	if !ok {
		return 0, fmt.Errorf("unable to read option for '%s'", keyname)
	}
	y, ok := x.(int)
	if !ok {
		return 0, fmt.Errorf("unable to coerce value for '%s'", keyname)
	}
	return y, nil
}
