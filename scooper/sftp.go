package scooper

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/freemed/remitt-server/model"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const SftpScooperClass = "org.remitt.plugin.scooper.SftpScooper"
const SftpScooperEnabled = "org.remitt.plugin.scooper.SftpScooper.enabled"

func init() {
	RegisterScooper(SftpScooperClass, func() Scooper { return &SftpScooper{} })
}

// contentPostProcessor is the transformation hook a scoop applies to every file
// it downloads.
//
// Go does not dispatch methods of an embedded struct virtually: a call to
// (*GatewayEdiSftpScooper).Scoop would compile into (*SftpScooper).Scoop and
// statically call (*SftpScooper).PostProcess, so a subclass override would never
// run (the Java original relied on exactly that override, see
// GatewayEdiSftpScooper.java:64-70). The scoop therefore takes the processor
// explicitly, and a subclass that overrides PostProcess passes itself — see
// (*GatewayEdiSftpScooper).Scoop.
type contentPostProcessor interface {
	PostProcess(data []byte, filename string) ([]byte, error)
}

// sftpSession is the part of an SFTP connection a scoop uses. It is an interface
// so the file loop — and with it the PostProcess dispatch and the persistence of
// what was downloaded — can be exercised without an SSH/SFTP server: ssh.Dial is
// called directly and nothing in the dependency tree provides a server, so the
// loop would otherwise be unreachable from a test.
type sftpSession interface {
	Join(elem ...string) string
	ReadDir(path string) ([]os.FileInfo, error)
	Open(path string) (io.ReadCloser, error)
	Close() error
}

// SftpScooper polls an SFTP server for new files, downloading and storing them
// in the tScooper table. Previously scooped files (by filename) are skipped.
type SftpScooper struct {
	username string
	host     string
	port     int
	sftpUser string
	sftpPass string
	sftpPath string
	params   map[string]string
	ctx      context.Context

	// paramErr records a configuration error found by SetParameters. The
	// setters keep their "always return nil" contract for the plugin loader, so
	// the error is reported by validateConfig before Scoop touches the database
	// or the network.
	paramErr error

	// sessionOpener is the transport seam. It is nil in production, where Scoop
	// dials the configured host itself; tests set it to drive the file loop
	// without an SSH server.
	sessionOpener func() (sftpSession, error)
}

// Scoop connects to the configured SFTP server, lists files in the target
// directory, and downloads any that haven't been scooped before. The base
// implementation passes itself as the content processor; a subclass that
// overrides PostProcess overrides Scoop to pass itself instead.
func (s *SftpScooper) Scoop() ([]ScooperResult, error) {
	return s.scoop(s)
}

// scoop is the implementation behind Scoop. post is the object whose
// PostProcess transforms each downloaded file, and is the outer scooper whenever
// one overrides PostProcess.
func (s *SftpScooper) scoop(post contentPostProcessor) ([]ScooperResult, error) {
	if err := s.validateConfig(); err != nil {
		return nil, err
	}

	// An uninitialised database (model.InitDb never ran) is a configuration
	// error, not a nil dereference in the middle of a scoop run.
	if model.SqlDb == nil {
		return nil, fmt.Errorf("sftpscooper: database not initialized")
	}

	// Get previously scooped files for this user/host/path combo.
	rows, err := model.SqlDb.QueryContext(context.Background(),
		"SELECT * FROM tScooper"+
			" WHERE scooperClass = ? AND user = ? AND host = ? AND path = ?",
		SftpScooperClass, s.username, s.host, s.sftpPath)
	if err != nil {
		return nil, fmt.Errorf("sftpscooper: query scooped: %w", err)
	}
	defer rows.Close()

	var scooped []model.ScooperModel
	for rows.Next() {
		var sc model.ScooperModel
		if err := rows.Scan(&sc.Id, &sc.ScooperClass, &sc.User, &sc.Stamp, &sc.Host, &sc.Path, &sc.Filename, &sc.Content); err != nil {
			continue
		}
		scooped = append(scooped, sc)
	}

	previouslyScooped := make(map[string]bool)
	for _, sc := range scooped {
		previouslyScooped[sc.Filename] = true
	}

	session, err := s.openSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()

	// Resolve target path. Client.Join builds a path from elements.
	targetPath := session.Join(s.sftpPath)

	// List files in the target directory.
	files, err := session.ReadDir(targetPath)
	if err != nil {
		return nil, fmt.Errorf("sftpscooper: readdir %q: %w", targetPath, err)
	}

	var results []ScooperResult
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if previouslyScooped[file.Name()] {
			continue
		}

		// Download file.
		remotePath := session.Join(targetPath, file.Name())
		remoteFile, err := session.Open(remotePath)
		if err != nil {
			continue // skip files we can't open
		}

		content, err := io.ReadAll(remoteFile)
		remoteFile.Close()
		if err != nil {
			continue
		}

		// PostProcess provides a hook for subclasses to transform content. The
		// call goes through the processor the scoop was handed, so a subclass
		// override actually runs here.
		processed, err := post.PostProcess(content, file.Name())
		if err != nil {
			continue
		}

		// Persist to tScooper so this file isn't scooped again.
		scooperEntry := model.ScooperModel{
			ScooperClass: SftpScooperClass,
			User:         s.username,
			Stamp:        time.Now(),
			Host:         s.host,
			Path:         s.sftpPath,
			Filename:     file.Name(),
			Content:      processed,
		}
		_, err = model.SqlDb.ExecContext(context.Background(),
			"INSERT INTO tScooper (scooperClass, user, stamp, host, path, filename, content) VALUES (?, ?, ?, ?, ?, ?, ?)",
			scooperEntry.ScooperClass, scooperEntry.User, scooperEntry.Stamp,
			scooperEntry.Host, scooperEntry.Path, scooperEntry.Filename, scooperEntry.Content)
		if err != nil {
			continue
		}

		results = append(results, ScooperResult{
			Filename: file.Name(),
			Host:     s.host,
			Path:     s.sftpPath,
			Content:  processed,
		})
	}

	return results, nil
}

// validateConfig reports a configuration problem that makes a scoop attempt
// meaningless: a parameter set rejected by SetParameters, or a host/port that
// was never configured. It runs before any database or network access, so a
// mistyped port can never become a dial target.
func (s *SftpScooper) validateConfig() error {
	if s.paramErr != nil {
		return fmt.Errorf("sftpscooper: host/port not configured: %w", s.paramErr)
	}
	if s.host == "" || s.port == 0 {
		return fmt.Errorf("sftpscooper: host/port not configured")
	}
	return nil
}

// openSession establishes the SFTP session Scoop reads from: the injected seam
// when a test provided one, otherwise a real SSH/SFTP connection to the
// configured host.
func (s *SftpScooper) openSession() (sftpSession, error) {
	if s.sessionOpener != nil {
		return s.sessionOpener()
	}

	sshConfig := &ssh.ClientConfig{
		User:            s.sftpUser,
		Auth:            []ssh.AuthMethod{ssh.Password(s.sftpPass)},
		Timeout:         10 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", s.host, s.port), sshConfig)
	if err != nil {
		return nil, fmt.Errorf("sftpscooper: ssh dial: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, fmt.Errorf("sftpscooper: sftp client: %w", err)
	}

	return &sftpClientSession{client: sftpClient, ssh: sshClient}, nil
}

// sftpClientSession adapts the real SSH/SFTP clients to sftpSession. Closing it
// releases both the SFTP and the underlying SSH connection.
type sftpClientSession struct {
	client *sftp.Client
	ssh    *ssh.Client
}

func (c *sftpClientSession) Join(elem ...string) string { return c.client.Join(elem...) }

func (c *sftpClientSession) ReadDir(path string) ([]os.FileInfo, error) {
	return c.client.ReadDir(path)
}

func (c *sftpClientSession) Open(path string) (io.ReadCloser, error) { return c.client.Open(path) }

func (c *sftpClientSession) Close() error {
	err := c.client.Close()
	if sshErr := c.ssh.Close(); err == nil {
		err = sshErr
	}
	return err
}

// PostProcess provides a hook for subclasses to transform downloaded content.
// Default implementation returns data unchanged.
func (s *SftpScooper) PostProcess(data []byte, filename string) ([]byte, error) {
	return data, nil
}

// SetParameters configures the scooper from a key-value parameter map.
// Expected keys: sftpUsername, sftpPassword, sftpHost, sftpPort, sftpPath.
//
// A malformed sftpPort is recorded rather than silently truncated, zeroed or
// accepted as negative; it is reported by validateConfig and therefore by
// Scoop. The return value stays nil: it is not what the plugin loader inspects,
// and callers rely on the setters never failing.
func (s *SftpScooper) SetParameters(params map[string]string) error {
	s.params = params
	s.sftpUser = params["sftpUsername"]
	s.sftpPass = params["sftpPassword"]
	s.host = params["sftpHost"]
	s.sftpPath = params["sftpPath"]
	s.port = 0
	s.paramErr = nil

	if portStr := params["sftpPort"]; portStr != "" {
		port, err := parseSftpPort(portStr)
		if err != nil {
			// Leave the port unset: a truncated or implausible value must never
			// reach ssh.Dial.
			s.paramErr = fmt.Errorf("invalid sftpPort %q: %w", portStr, err)
			return nil
		}
		s.port = port
	}

	return nil
}

// parseSftpPort parses the sftpPort parameter strictly. The previous
// fmt.Sscanf(portStr, "%d", &port) discarded its error, so "22xyz" became 22,
// "abc" became 0 and "-1" passed the guard and was dialled.
func parseSftpPort(raw string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("must be a whole number: %w", err)
	}
	if port < 0 {
		return 0, fmt.Errorf("must not be negative")
	}
	if port > 65535 {
		return 0, fmt.Errorf("must be 65535 or lower")
	}
	return port, nil
}

// SetUsername sets the user owning this scooper run.
func (s *SftpScooper) SetUsername(user string) error {
	s.username = user
	return nil
}

// GetEnabledConfigValue returns the configuration key that controls whether
// this scooper is enabled.
func (s *SftpScooper) GetEnabledConfigValue() string {
	return SftpScooperEnabled
}

// SetContext sets the execution context.
func (s *SftpScooper) SetContext(ctx context.Context) error {
	s.ctx = ctx
	return nil
}
