package scooper

import (
	"context"
	"fmt"
	"io"
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
}

// Scoop connects to the configured SFTP server, lists files in the target
// directory, and downloads any that haven't been scooped before.
func (s *SftpScooper) Scoop() ([]ScooperResult, error) {
	if s.host == "" || s.port == 0 {
		return nil, fmt.Errorf("sftpscooper: host/port not configured")
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

	// Connect to SFTP.
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
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("sftpscooper: sftp client: %w", err)
	}
	defer sftpClient.Close()

	// Resolve target path. Client.Join builds a path from elements.
	targetPath := sftpClient.Join(s.sftpPath)

	// List files in the target directory.
	files, err := sftpClient.ReadDir(targetPath)
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
		remotePath := sftpClient.Join(targetPath, file.Name())
		remoteFile, err := sftpClient.Open(remotePath)
		if err != nil {
			continue // skip files we can't open
		}

		content, err := io.ReadAll(remoteFile)
		remoteFile.Close()
		if err != nil {
			continue
		}

		// PostProcess provides a hook for subclasses to transform content.
		processed, err := s.PostProcess(content, file.Name())
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

// PostProcess provides a hook for subclasses to transform downloaded content.
// Default implementation returns data unchanged.
func (s *SftpScooper) PostProcess(data []byte, filename string) ([]byte, error) {
	return data, nil
}

// SetParameters configures the scooper from a key-value parameter map.
// Expected keys: sftpUsername, sftpPassword, sftpHost, sftpPort, sftpPath.
func (s *SftpScooper) SetParameters(params map[string]string) error {
	s.params = params
	s.sftpUser = params["sftpUsername"]
	s.sftpPass = params["sftpPassword"]
	s.host = params["sftpHost"]
	if portStr := params["sftpPort"]; portStr != "" {
		fmt.Sscanf(portStr, "%d", &s.port)
	}
	s.sftpPath = params["sftpPath"]
	return nil
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
