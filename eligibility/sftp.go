package eligibility

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/freemed/remitt-server/crypto"
	"github.com/freemed/remitt-server/model"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const SftpEligibilityClass = "org.remitt.plugin.eligibility.SftpEligibility"

func init() {
	RegisterChecker(SftpEligibilityClass, func() EligibilityChecker { return &SftpEligibility{} })
}

// SftpEligibility implements the EligibilityChecker interface by serializing
// eligibility request values to JSON, optionally PGP-encrypting them using a
// key from the user's keyring, and uploading the result to a configured SFTP
// server.
//
// Config keys (from tUserConfig):
//
//	sftpHost     - SFTP server hostname (required)
//	sftpPort     - SFTP server port (required)
//	sftpUsername - SFTP username (required)
//	sftpPassword - SFTP password (required)
//	sftpPath     - Remote directory path for uploads (required)
//	sftpKeyName  - Optional PGP key name in the user's keyring
type SftpEligibility struct {
	ctx context.Context
}

// GetPluginName returns the fully-qualified Java-style plugin class name.
func (s *SftpEligibility) GetPluginName() string {
	return SftpEligibilityClass
}

// GetPluginVersion returns the plugin version.
func (s *SftpEligibility) GetPluginVersion() string {
	return "0.1"
}

// GetPluginConfigurationOptions returns the list of config keys expected in
// tUserConfig for this plugin.
func (s *SftpEligibility) GetPluginConfigurationOptions() []string {
	return []string{"sftpHost", "sftpPort", "sftpUsername", "sftpPassword", "sftpPath", "sftpKeyName"}
}

// SetContext stores the context for later use.
func (s *SftpEligibility) SetContext(ctx context.Context) error {
	s.ctx = ctx
	return nil
}

// CheckEligibility serializes the eligibility request values to JSON,
// optionally PGP-encrypts them, and uploads the result to the configured
// SFTP server.  All configuration is read from tUserConfig for the given
// userName.
func (s *SftpEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	// Load config from tUserConfig
	configRows, err := model.GetConfigValues(userName)
	if err != nil {
		return nil, fmt.Errorf("sftp eligibility: failed to load config: %w", err)
	}

	config := make(map[string]string)
	for _, row := range configRows {
		config[row.Option] = row.Value
	}

	// Validate required config
	host := config["sftpHost"]
	portStr := config["sftpPort"]
	username := config["sftpUsername"]
	password := config["sftpPassword"]
	path := config["sftpPath"]

	if host == "" {
		return nil, fmt.Errorf("sftp eligibility: sftpHost not configured")
	}
	if portStr == "" {
		return nil, fmt.Errorf("sftp eligibility: sftpPort not configured")
	}
	if username == "" {
		return nil, fmt.Errorf("sftp eligibility: sftpUsername not configured")
	}
	if password == "" {
		return nil, fmt.Errorf("sftp eligibility: sftpPassword not configured")
	}
	if path == "" {
		return nil, fmt.Errorf("sftp eligibility: sftpPath not configured")
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("sftp eligibility: invalid sftpPort %q: %w", portStr, err)
	}

	// Serialize request values to JSON
	jsonData, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("sftp eligibility: marshal: %w", err)
	}

	// Optionally PGP encrypt
	payload := jsonData
	if keyName := config["sftpKeyName"]; keyName != "" {
		keyEntry, err := model.GetKeyringEntry(userName, keyName)
		if err != nil {
			return nil, fmt.Errorf("sftp eligibility: keyring: %w", err)
		}
		encrypted, err := crypto.EncryptPGP(jsonData, keyEntry.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("sftp eligibility: pgp encrypt: %w", err)
		}
		payload = encrypted
	}

	// Generate output filename
	filename := fmt.Sprintf("eligibility_%s_%d_%d.json", userName, jobID, time.Now().Unix())

	// SFTP upload
	sshConfig := &ssh.ClientConfig{
		User:            username,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		Timeout:         10 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", host, port), sshConfig)
	if err != nil {
		return nil, fmt.Errorf("sftp eligibility: ssh dial: %w", err)
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return nil, fmt.Errorf("sftp eligibility: sftp client: %w", err)
	}
	defer sftpClient.Close()

	// sftpClient.Join returns a plain string, not (string, error)
	remotePath := sftpClient.Join(path, filename)
	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return nil, fmt.Errorf("sftp eligibility: create: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(payload); err != nil {
		return nil, fmt.Errorf("sftp eligibility: write: %w", err)
	}

	return &EligibilityResponse{
		Status:      StatusOK,
		SuccessCode: SuccessCodeSuccess,
		Messages:    []string{"Eligibility request uploaded via SFTP"},
	}, nil
}
