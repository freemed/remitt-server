package scooper

import (
	"context"
	"fmt"

	"github.com/freemed/remitt-server/crypto"
	"github.com/freemed/remitt-server/model"
)

const GatewayEdiScooperClass = "org.remitt.plugin.scooper.GatewayEdiSftpScooper"
const GatewayEdiScooperEnabled = "org.remitt.plugin.scooper.GatewayEdiSftpScooper.enabled"
const GatewayEdiKeyName = "GatewayEDI"

func init() {
	RegisterScooper(GatewayEdiScooperClass, func() Scooper { return &GatewayEdiSftpScooper{} })
}

// GatewayEdiSftpScooper extends SftpScooper by adding PGP decryption
// for files downloaded from GatewayEDI's SFTP server.
type GatewayEdiSftpScooper struct {
	SftpScooper
}

// PostProcess decrypts PGP-encrypted content using the user's GatewayEDI key.
func (g *GatewayEdiSftpScooper) PostProcess(data []byte, filename string) ([]byte, error) {
	// Check if data appears to be PGP-encrypted
	if !crypto.IsPGPEncrypted(data) {
		return data, nil
	}

	// Retrieve the GatewayEDI private key from tKeyring
	row := model.SqlDb.QueryRowContext(context.Background(),
		"SELECT id, user, keyname, privatekey, publickey FROM tKeyring WHERE user = ? AND keyname = ?",
		g.username, GatewayEdiKeyName)
	var key model.KeyringModel
	err := row.Scan(&key.Id, &key.User, &key.KeyName, &key.PrivateKey, &key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: key '%s' not found for user '%s'",
			GatewayEdiKeyName, g.username)
	}

	decrypted, err := crypto.DecryptPGP(data, key.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: pgp decrypt: %w", err)
	}

	return decrypted, nil
}

// GetEnabledConfigValue returns the config key that enables this scooper.
func (g *GatewayEdiSftpScooper) GetEnabledConfigValue() string {
	return GatewayEdiScooperEnabled
}

// SetContext sets the execution context.
func (g *GatewayEdiSftpScooper) SetContext(ctx context.Context) error {
	return g.SftpScooper.SetContext(ctx)
}
