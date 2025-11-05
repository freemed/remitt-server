package transport

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func init() {
	RegisterTransporter("sftp", func() Transporter { return &Sftp{} })
}

// Sftp represents a transport which
type Sftp struct {
	host     string
	port     int
	username string
	password string
	keydata  string
	path     string
	ctx      context.Context
}

// Transport performs the actual work of transport, given the input.
func (s *Sftp) Transport(filename string, data any) error {
	// Validate basic settings before doing anything
	if s.host == "" || s.port == 0 || s.username == "" {
		return fmt.Errorf("sftp: missing host, port, or username")
	}
	if s.keydata == "" && s.password == "" {
		return fmt.Errorf("sftp: no password or key given")
	}

	sshConfig := &ssh.ClientConfig{
		User:    s.username,
		Auth:    []ssh.AuthMethod{}, // populate later
		Timeout: time.Duration(10) * time.Second,
	}
	if s.password != "" || s.keydata != "" {
		sshConfig.Auth = append(sshConfig.Auth, ssh.Password(s.password))
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", s.host, s.port), sshConfig)
	if err != nil {
		return fmt.Errorf("sftp: dial: %w", err)
	}
	defer sshClient.Close()
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("sftp: client: %w", err)
	}
	defer sftpClient.Close()

	f, err := sftpClient.Create(fmt.Sprintf("%s/%s", s.path, filename))
	if err != nil {
		return fmt.Errorf("sftp: create: %w", err)
	}
	defer f.Close()
	switch data := data.(type) {
	case string:
		f.Write([]byte(data))
	case []byte:
		f.Write(data)
	default:
		return fmt.Errorf("sftp: invalid data type %#v presented", data)
	}
	return nil
}

// InputFormat returns the input format required by this plugin.
func (s *Sftp) InputFormat() string {
	return "x12"
}

// Options returns a list of valid options for this transporter type
func (s *Sftp) Options() []string {
	return []string{"sftpUsername", "sftpPassword", "sftpHost", "sftpPort", "sftpPath"}
}

// SetOptions sets the current options for this plugin
func (s *Sftp) SetOptions(o map[string]any) error {
	s.username, _ = s.coerceOptionString(o, "sftpUsername")
	s.password, _ = s.coerceOptionString(o, "sftpPassword")
	s.host, _ = s.coerceOptionString(o, "sftpHost")
	s.port, _ = s.coerceOptionInt(o, "sftpPort")
	s.path, _ = s.coerceOptionString(o, "sftpPath")

	return nil
}

// SetContext sets the context in which this executes
func (s *Sftp) SetContext(c context.Context) error {
	s.ctx = c
	return nil
}

func (s *Sftp) coerceOptionString(o map[string]any, keyname string) (string, error) {
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

func (s *Sftp) coerceOptionInt(o map[string]any, keyname string) (int, error) {
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
