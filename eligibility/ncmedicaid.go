package eligibility

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/freemed/remitt-server/crypto"
	"github.com/freemed/remitt-server/model"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const (
	NCMedicaidPluginName    = "org.remitt.plugin.eligibility.NCMedicaidEligibility"
	NCMedicaidPluginVersion = "0.1"
	NCMedicaidKeyringName   = "NCMedicaid"
	NCMedicaidConfigNS      = "eligibility_ncmedicaid"
)

var ncMedicaidConfigKeys = []string{
	"ncMedicaidHost",
	"ncMedicaidPort",
	"ncMedicaidUsername",
	"ncMedicaidPassword",
	"ncMedicaidPath",
}

func init() {
	RegisterChecker(NCMedicaidPluginName, func() EligibilityChecker {
		return &NCMedicaidEligibility{}
	})
}

// NCMedicaidEligibility checks patient eligibility by serializing the request
// as JSON, PGP-encrypting it with the NCMedicaid key, and uploading the
// encrypted payload to a configured SFTP server.
type NCMedicaidEligibility struct {
	username   string
	host       string
	port       int
	sftpUser   string
	sftpPass   string
	sftpPath   string
	configured bool
	ctx        context.Context
}

func (c *NCMedicaidEligibility) GetPluginName() string {
	return NCMedicaidPluginName
}

func (c *NCMedicaidEligibility) GetPluginVersion() string {
	return NCMedicaidPluginVersion
}

func (c *NCMedicaidEligibility) GetPluginConfigurationOptions() []string {
	return ncMedicaidConfigKeys
}

func (c *NCMedicaidEligibility) SetContext(ctx context.Context) error {
	c.ctx = ctx
	return nil
}

// CheckEligibility serializes the request, PGP-encrypts it using the NCMedicaid
// public key from the keyring, and uploads the result to the configured SFTP
// server. Configuration is loaded from tUserConfig on first call.
func (c *NCMedicaidEligibility) CheckEligibility(userName string, values map[string]string, resubmission bool, jobID int64) (*EligibilityResponse, error) {
	// Load configuration on first call.
	if !c.configured {
		if err := c.loadConfig(userName); err != nil {
			return nil, fmt.Errorf("ncmedicaid: config: %w", err)
		}
	}

	// Serialize request values to JSON.
	req := EligibilityRequest{
		Plugin:  NCMedicaidPluginName,
		Request: values,
	}
	jsonData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ncmedicaid: marshal: %w", err)
	}

	// Retrieve NCMedicaid public key from keyring.
	keyringEntry, err := model.GetKeyringEntry(userName, NCMedicaidKeyringName)
	if err != nil {
		return nil, fmt.Errorf("ncmedicaid: keyring %q: %w", NCMedicaidKeyringName, err)
	}
	if len(keyringEntry.PublicKey) == 0 {
		return nil, fmt.Errorf("ncmedicaid: keyring %q has no public key", NCMedicaidKeyringName)
	}

	// PGP-encrypt the JSON payload.
	encrypted, err := crypto.EncryptPGP(jsonData, keyringEntry.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("ncmedicaid: encrypt: %w", err)
	}

	// Upload via SFTP.
	filename := fmt.Sprintf("nc_elig_%d_%d.json.pgp", jobID, time.Now().Unix())
	if err := c.sftpUpload(filename, encrypted); err != nil {
		return nil, fmt.Errorf("ncmedicaid: sftp upload: %w", err)
	}

	return &EligibilityResponse{
		Status:      StatusOK,
		SuccessCode: SuccessCodeSuccess,
		Messages:    []string{"NCMedicaid eligibility request submitted"},
	}, nil
}

// loadConfig reads NCMedicaid configuration from tUserConfig.
func (c *NCMedicaidEligibility) loadConfig(userName string) error {
	if model.Queries == nil {
		return fmt.Errorf("load config: database not initialized")
	}
	configValues, err := model.GetConfigValues(userName)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	params := make(map[string]string)
	for _, cv := range configValues {
		if cv.Namespace == NCMedicaidConfigNS {
			params[cv.Option] = cv.Value
		}
	}

	c.host = params["ncMedicaidHost"]
	c.sftpUser = params["ncMedicaidUsername"]
	c.sftpPass = params["ncMedicaidPassword"]
	c.sftpPath = params["ncMedicaidPath"]
	if portStr := params["ncMedicaidPort"]; portStr != "" {
		fmt.Sscanf(portStr, "%d", &c.port)
	}

	if c.host == "" || c.port == 0 {
		return fmt.Errorf("host/port not configured")
	}
	if c.sftpUser == "" || c.sftpPass == "" {
		return fmt.Errorf("username/password not configured")
	}
	if c.sftpPath == "" {
		return fmt.Errorf("remote path not configured")
	}

	c.username = userName
	c.configured = true
	return nil
}

// sftpUpload connects to the SFTP server and uploads the payload.
func (c *NCMedicaidEligibility) sftpUpload(filename string, data []byte) error {
	sshConfig := &ssh.ClientConfig{
		User:            c.sftpUser,
		Auth:            []ssh.AuthMethod{ssh.Password(c.sftpPass)},
		Timeout:         10 * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	sshClient, err := ssh.Dial("tcp", fmt.Sprintf("%s:%d", c.host, c.port), sshConfig)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer sshClient.Close()

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		return fmt.Errorf("sftp client: %w", err)
	}
	defer sftpClient.Close()

	remotePath := sftpClient.Join(c.sftpPath, filename)
	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create %q: %w", remotePath, err)
	}
	defer remoteFile.Close()

	if _, err := remoteFile.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}
