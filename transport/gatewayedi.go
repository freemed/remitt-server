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
	// Legacy database and UI store the Java class name
	// (migrations/001_legacy.up.sql); jobqueue passes that value straight
	// through, so the alias must resolve too.
	registerJavaTransporter("GatewayEdiTransport", func() Transporter { return &GatewayEdi{} })
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

	// explicit holds the options a direct caller handed to SetOptions. They
	// take precedence over the values stored for the user in tUserConfig (see
	// configureFromUser and selfconfig.go).
	explicit map[string]any
}

// Transport performs the actual work of transport, given the input.
func (g *GatewayEdi) Transport(filename string, data any) error {
	// Resolve the configuration before it is validated or used: the caller's
	// own tUserConfig rows are applied through SetOptions' coercion path, and
	// anything a direct caller set explicitly beats them.
	if err := g.configureFromUser(); err != nil {
		return err
	}

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

	// Host key verification follows the configured policy
	// (paths.known-hosts, or the explicit sftp-insecure-ignore-hostkey
	// opt-in); with neither configured this fails closed here, before any
	// connection is attempted. See common.HostKeyCallback.
	hostKeyCallback, err := common.HostKeyCallback()
	if err != nil {
		return fmt.Errorf("gatewayedi: %w", err)
	}

	// SFTP upload
	sshConfig := &ssh.ClientConfig{
		User:            g.username,
		Auth:            []ssh.AuthMethod{ssh.Password(g.password)},
		Timeout:         10 * time.Second,
		HostKeyCallback: hostKeyCallback,
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

// userConfigNamespaces returns the tUserConfig namespaces this plugin's own
// rows live under, the Java FQCN the legacy database stores first and the
// registered short name second.
func (g *GatewayEdi) userConfigNamespaces() []string {
	return transportNamespaces("gatewayedi", "GatewayEdiTransport")
}

// storedOptionValue converts one stored tUserConfig value into the Go value
// SetOptions expects. gatewayEdiPort is the plugin's only int option; every
// other option is text.
func (g *GatewayEdi) storedOptionValue(option, stored string) (any, error) {
	if option == "gatewayEdiPort" {
		return storedIntOption(option, stored)
	}
	return storedStringOption(option, stored)
}

// configureFromUser reads the caller's own configuration out of tUserConfig and
// applies it, so the plugin delivers to the endpoint the user actually
// configured without anything having to call SetOptions.
//
// Explicitly-set options always win over database-derived ones; the database
// only fills in what SetOptions did not supply. With no user in the context, or
// no database, there is no database-derived configuration and the plugin keeps
// exactly what SetOptions gave it (see selfconfig.go).
func (g *GatewayEdi) configureFromUser() error {
	stored, err := loadUserConfigOptions(g.ctx, "gatewayedi", g.userConfigNamespaces(), g.Options(), g.storedOptionValue)
	if err != nil {
		return fmt.Errorf("gatewayedi: %w", err)
	}
	return g.applyOptions(mergeOptions(stored, g.explicit))
}

// SetOptions sets the current options for this plugin.
//
// An option that is present but carries the wrong type is reported: the
// previous implementation discarded the coercion error, so a wrong-typed
// option left the plugin silently unconfigured and only failed much later, in
// Transport, with an error that never named the option at fault.
//
// An option that is simply absent is not an error - the field keeps its zero
// value and Transport reports it as missing configuration.
//
// The options handed in here are remembered as the explicitly-set ones: they
// take precedence, option by option, over the values the caller has stored for
// this plugin in tUserConfig, which Transport applies on top of what is missing
// (see configureFromUser).
func (g *GatewayEdi) SetOptions(o map[string]any) error {
	g.explicit = copyOptions(o)
	return g.applyOptions(o)
}

// applyOptions is the option-coercion path: it maps an option map onto the
// plugin's own fields, reporting any value it cannot coerce.
func (g *GatewayEdi) applyOptions(o map[string]any) error {
	var err error
	if g.host, err = g.stringOption(o, "gatewayEdiHost"); err != nil {
		return err
	}
	if g.port, err = g.intOption(o, "gatewayEdiPort"); err != nil {
		return err
	}
	if g.username, err = g.stringOption(o, "gatewayEdiUsername"); err != nil {
		return err
	}
	if g.password, err = g.stringOption(o, "gatewayEdiPassword"); err != nil {
		return err
	}
	if g.path, err = g.stringOption(o, "gatewayEdiPath"); err != nil {
		return err
	}
	return nil
}

// SetContext sets the context in which this executes
func (g *GatewayEdi) SetContext(c context.Context) error {
	g.ctx = c
	return nil
}

// stringOption reads an optional string option. A missing key leaves the field
// at its zero value; a present value of another type is a configuration error.
func (g *GatewayEdi) stringOption(o map[string]any, keyname string) (string, error) {
	x, ok := o[keyname]
	if !ok {
		return "", nil
	}
	y, ok := x.(string)
	if !ok {
		return "", fmt.Errorf("gatewayedi: unable to coerce value for '%s': got %T, want string", keyname, x)
	}
	return y, nil
}

// intOption reads an optional int option, with the same missing/typed rules as
// stringOption.
func (g *GatewayEdi) intOption(o map[string]any, keyname string) (int, error) {
	x, ok := o[keyname]
	if !ok {
		return 0, nil
	}
	y, ok := x.(int)
	if !ok {
		return 0, fmt.Errorf("gatewayedi: unable to coerce value for '%s': got %T, want int", keyname, x)
	}
	return y, nil
}
