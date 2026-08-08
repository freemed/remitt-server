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

	// SFTP upload
	sshConfig := &ssh.ClientConfig{
		User:    c.username,
		Auth:    []ssh.AuthMethod{ssh.Password(c.password)},
		Timeout: 10 * time.Second,
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

func (c *ClaimLogic) SetOptions(o map[string]any) error {
	c.host, _ = c.coerceOptionString(o, "claimlogicHost")
	c.port, _ = c.coerceOptionInt(o, "claimlogicPort")
	c.username, _ = c.coerceOptionString(o, "claimlogicUsername")
	c.password, _ = c.coerceOptionString(o, "claimlogicPassword")
	c.path, _ = c.coerceOptionString(o, "claimlogicPath")
	return nil
}

func (c *ClaimLogic) SetContext(ctx context.Context) error {
	c.ctx = ctx
	return nil
}

func (c *ClaimLogic) coerceOptionString(o map[string]any, keyname string) (string, error) {
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

func (c *ClaimLogic) coerceOptionInt(o map[string]any, keyname string) (int, error) {
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
