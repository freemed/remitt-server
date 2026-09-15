package transport

import (
	"context"
	"fmt"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func init() {
	RegisterTransporter("sftp", func() Transporter { return &Sftp{} })
	// Legacy database and UI store the Java class name
	// (migrations/001_legacy.up.sql); jobqueue passes that value straight
	// through, so the alias must resolve too.
	registerJavaTransporter("SftpTransport", func() Transporter { return &Sftp{} })
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

	// explicit holds the options a direct caller handed to SetOptions. They
	// take precedence over the values stored for the user in tUserConfig (see
	// configureFromUser and selfconfig.go).
	explicit map[string]any
}

// Transport performs the actual work of transport, given the input.
func (s *Sftp) Transport(filename string, data any) error {
	// Resolve the configuration before it is validated or used: the caller's
	// own tUserConfig rows are applied through SetOptions' coercion path, and
	// anything a direct caller set explicitly beats them.
	if err := s.configureFromUser(); err != nil {
		return err
	}

	// Validate basic settings before doing anything
	if s.host == "" || s.port == 0 || s.username == "" {
		return fmt.Errorf("sftp: missing host, port, or username")
	}
	if s.keydata == "" && s.password == "" {
		return fmt.Errorf("sftp: no password or key given")
	}

	// Host key verification follows the configured policy
	// (paths.known-hosts, or the explicit sftp-insecure-ignore-hostkey
	// opt-in); with neither configured this fails closed here, before any
	// connection is attempted. See common.HostKeyCallback.
	hostKeyCallback, err := common.HostKeyCallback()
	if err != nil {
		return fmt.Errorf("sftp: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            s.username,
		Auth:            []ssh.AuthMethod{}, // populate later
		Timeout:         time.Duration(10) * time.Second,
		HostKeyCallback: hostKeyCallback,
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

// userConfigNamespaces returns the tUserConfig namespaces this plugin's own
// rows live under, the Java FQCN the legacy database stores first and the
// registered short name second.
func (s *Sftp) userConfigNamespaces() []string {
	return transportNamespaces("sftp", "SftpTransport")
}

// storedOptionValue converts one stored tUserConfig value into the Go value
// SetOptions expects. sftpPort is the plugin's only int option; every other
// option is text.
func (s *Sftp) storedOptionValue(option, stored string) (any, error) {
	if option == "sftpPort" {
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
func (s *Sftp) configureFromUser() error {
	stored, err := loadUserConfigOptions(s.ctx, "sftp", s.userConfigNamespaces(), s.Options(), s.storedOptionValue)
	if err != nil {
		return fmt.Errorf("sftp: %w", err)
	}
	return s.applyOptions(mergeOptions(stored, s.explicit))
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
func (s *Sftp) SetOptions(o map[string]any) error {
	s.explicit = copyOptions(o)
	return s.applyOptions(o)
}

// applyOptions is the option-coercion path: it maps an option map onto the
// plugin's own fields, reporting any value it cannot coerce.
func (s *Sftp) applyOptions(o map[string]any) error {
	var err error
	if s.username, err = s.stringOption(o, "sftpUsername"); err != nil {
		return err
	}
	if s.password, err = s.stringOption(o, "sftpPassword"); err != nil {
		return err
	}
	if s.host, err = s.stringOption(o, "sftpHost"); err != nil {
		return err
	}
	if s.port, err = s.intOption(o, "sftpPort"); err != nil {
		return err
	}
	if s.path, err = s.stringOption(o, "sftpPath"); err != nil {
		return err
	}

	return nil
}

// SetContext sets the context in which this executes
func (s *Sftp) SetContext(c context.Context) error {
	s.ctx = c
	return nil
}

// stringOption reads an optional string option. A missing key leaves the field
// at its zero value; a present value of another type is a configuration error.
func (s *Sftp) stringOption(o map[string]any, keyname string) (string, error) {
	x, ok := o[keyname]
	if !ok {
		return "", nil
	}
	y, ok := x.(string)
	if !ok {
		return "", fmt.Errorf("sftp: unable to coerce value for '%s': got %T, want string", keyname, x)
	}
	return y, nil
}

// intOption reads an optional int option, with the same missing/typed rules as
// stringOption.
func (s *Sftp) intOption(o map[string]any, keyname string) (int, error) {
	x, ok := o[keyname]
	if !ok {
		return 0, nil
	}
	y, ok := x.(int)
	if !ok {
		return 0, fmt.Errorf("sftp: unable to coerce value for '%s': got %T, want int", keyname, x)
	}
	return y, nil
}
