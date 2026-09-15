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
	RegisterTransporter("claimlogic", func() Transporter { return &ClaimLogic{} })
	// Legacy database and UI store the Java class name
	// (migrations/001_legacy.up.sql); jobqueue passes that value straight
	// through, so the alias must resolve too.
	registerJavaTransporter("ClaimLogicTransport", func() Transporter { return &ClaimLogic{} })
}

type ClaimLogic struct {
	host     string
	port     int
	username string
	password string
	path     string
	ctx      context.Context
}

func (c *ClaimLogic) Transport(filename string, data any) error {
	um, ok := user.FromContext(c.ctx)
	if !ok {
		return fmt.Errorf("claimlogic: unable to retrieve user from context")
	}

	// Convert data to bytes
	var payload []byte
	switch d := data.(type) {
	case string:
		payload = []byte(d)
	case []byte:
		payload = d
	default:
		return fmt.Errorf("claimlogic: invalid data type %T", data)
	}

	// ZIP the payload
	izw := common.NewInternalZipWriter()
	zipName := fmt.Sprintf("%d.x12", time.Now().Unix())
	if err := izw.Store(zipName, payload); err != nil {
		return fmt.Errorf("claimlogic: zip store: %w", err)
	}
	zippedData, err := izw.GetData()
	if err != nil {
		return fmt.Errorf("claimlogic: zip getdata: %w", err)
	}

	// Host key verification follows the configured policy
	// (paths.known-hosts, or the explicit sftp-insecure-ignore-hostkey
	// opt-in); with neither configured this fails closed here, before any
	// connection is attempted. See common.HostKeyCallback.
	hostKeyCallback, err := common.HostKeyCallback()
	if err != nil {
		return fmt.Errorf("claimlogic: %w", err)
	}

	// SFTP upload
	sshConfig := &ssh.ClientConfig{
		User:            c.username,
		Auth:            []ssh.AuthMethod{ssh.Password(c.password)},
		Timeout:         10 * time.Second,
		HostKeyCallback: hostKeyCallback,
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", c.host, c.port), sshConfig)
	if err != nil {
		return fmt.Errorf("claimlogic: ssh dial: %w", err)
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("claimlogic: sftp client: %w", err)
	}
	defer sftpClient.Close()

	remotePath := fmt.Sprintf("%s/%s.zip", c.path, filename)
	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("claimlogic: sftp create: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(zippedData); err != nil {
		return fmt.Errorf("claimlogic: sftp write: %w", err)
	}

	_ = um
	return nil
}

func (c *ClaimLogic) InputFormat() string {
	return "x12"
}

func (c *ClaimLogic) Options() []string {
	return []string{"claimlogicHost", "claimlogicPort", "claimlogicUsername", "claimlogicPassword", "claimlogicPath"}
}

// SetOptions sets the current options for this plugin.
//
// An option that is present but carries the wrong type is reported: the
// previous implementation discarded the coercion error, so a wrong-typed
// option left the plugin silently unconfigured and only failed much later, in
// Transport, with an error that never named the option at fault.
//
// An option that is simply absent is not an error - the field keeps its zero
// value (ClaimLogic still performs no up-front configuration validation, so an
// empty endpoint is reported by the dial).
func (c *ClaimLogic) SetOptions(o map[string]any) error {
	var err error
	if c.host, err = c.stringOption(o, "claimlogicHost"); err != nil {
		return err
	}
	if c.port, err = c.intOption(o, "claimlogicPort"); err != nil {
		return err
	}
	if c.username, err = c.stringOption(o, "claimlogicUsername"); err != nil {
		return err
	}
	if c.password, err = c.stringOption(o, "claimlogicPassword"); err != nil {
		return err
	}
	if c.path, err = c.stringOption(o, "claimlogicPath"); err != nil {
		return err
	}
	return nil
}

func (c *ClaimLogic) SetContext(ctx context.Context) error {
	c.ctx = ctx
	return nil
}

// stringOption reads an optional string option. A missing key leaves the field
// at its zero value; a present value of another type is a configuration error.
func (c *ClaimLogic) stringOption(o map[string]any, keyname string) (string, error) {
	x, ok := o[keyname]
	if !ok {
		return "", nil
	}
	y, ok := x.(string)
	if !ok {
		return "", fmt.Errorf("claimlogic: unable to coerce value for '%s': got %T, want string", keyname, x)
	}
	return y, nil
}

// intOption reads an optional int option, with the same missing/typed rules as
// stringOption.
func (c *ClaimLogic) intOption(o map[string]any, keyname string) (int, error) {
	x, ok := o[keyname]
	if !ok {
		return 0, nil
	}
	y, ok := x.(int)
	if !ok {
		return 0, fmt.Errorf("claimlogic: unable to coerce value for '%s': got %T, want int", keyname, x)
	}
	return y, nil
}
