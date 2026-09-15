package scooper

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"github.com/freemed/remitt-server/crypto"
	"github.com/freemed/remitt-server/model"
	"golang.org/x/crypto/openpgp/packet"
)

const GatewayEdiScooperClass = "org.remitt.plugin.scooper.GatewayEdiSftpScooper"
const GatewayEdiScooperEnabled = "org.remitt.plugin.scooper.GatewayEdiSftpScooper.enabled"
const GatewayEdiKeyName = "GatewayEDI"

// The 0.5.x Java original hardcoded its vendor connection in
// GatewayEdiSftpScooper.java:52-62 — getHost() returned "sftp.gatewayedi.com",
// getPort() 22 and getPath() "remits" — so a Java-built GatewayEDI scooper was
// always configured.
//
// The Go port does not assume them: silently pointing a fresh scooper at a
// vendor endpoint would be a surprise, and the plugin loader only reads the
// parameters a caller stored in tUserConfig. They are recorded here (not
// invented) for migrations and for callers that want them, and they take effect
// only when supplied explicitly — through SetParameters, or by way of
// GatewayEdiJavaDefaultParams.
const (
	GatewayEdiJavaDefaultHost = "sftp.gatewayedi.com"
	GatewayEdiJavaDefaultPort = 22
	GatewayEdiJavaDefaultPath = "remits"
)

// GatewayEdiJavaDefaultParams returns the connection parameters the Java
// original hardcoded, for a caller or migration that wants to opt in
// explicitly. It is never applied implicitly: a GatewayEDI scooper with no
// parameters stays unconfigured and fails Scoop with the host/port error.
func GatewayEdiJavaDefaultParams() map[string]string {
	return map[string]string{
		"sftpHost": GatewayEdiJavaDefaultHost,
		"sftpPort": strconv.Itoa(GatewayEdiJavaDefaultPort),
		"sftpPath": GatewayEdiJavaDefaultPath,
	}
}

func init() {
	RegisterScooper(GatewayEdiScooperClass, func() Scooper { return &GatewayEdiSftpScooper{} })
}

// GatewayEdiSftpScooper extends SftpScooper by adding PGP decryption
// for files downloaded from GatewayEDI's SFTP server.
type GatewayEdiSftpScooper struct {
	SftpScooper
}

// Scoop overrides the embedded SftpScooper.Scoop only to hand the file loop the
// outer scooper as its content processor.
//
// Go has no virtual dispatch for embedded structs: without this method a call to
// g.Scoop() would run (*SftpScooper).Scoop with the embedded SftpScooper as
// receiver, which statically calls (*SftpScooper).PostProcess, and the
// decrypting PostProcess below — the whole point of this subclass, and what
// GatewayEdiSftpScooper.java:64-70 relies on — would never run. Remittances
// would then be stored as raw ciphertext.
func (g *GatewayEdiSftpScooper) Scoop() ([]ScooperResult, error) {
	return g.SftpScooper.scoop(g)
}

// PostProcess decrypts the PGP payload of a remittance file with the user's
// stored GatewayEDI private key. Content that is already readable remittance
// data (XML, X12, plain text) is returned verbatim and never costs a keyring
// lookup.
func (g *GatewayEdiSftpScooper) PostProcess(data []byte, filename string) ([]byte, error) {
	// Decryption used to be gated on crypto.IsPGPEncrypted, which only matches
	// a payload whose first non-blank characters are an armor header
	// (crypto/pgp.go:81-85). crypto.EncryptPGP — this codebase's own PGP pair —
	// emits binary messages, and a payer can put a transport preamble ahead of
	// an armored block, so both forms used to be stored as raw ciphertext with
	// the user's key never consulted.
	if !looksEncrypted(data) {
		return data, nil
	}

	// An uninitialised database (model.InitDb never ran) is a configuration
	// error, not a nil dereference while decrypting.
	if model.SqlDb == nil {
		return nil, fmt.Errorf("gatewayedi: database not initialized")
	}

	// Retrieve the GatewayEDI private key from tKeyring.
	row := model.SqlDb.QueryRowContext(context.Background(),
		"SELECT id, user, keyname, privatekey, publickey FROM tKeyring WHERE user = ? AND keyname = ?",
		g.username, GatewayEdiKeyName)
	var key model.KeyringModel
	if err := row.Scan(&key.Id, &key.User, &key.KeyName, &key.PrivateKey, &key.PublicKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Genuinely absent: name both pieces of the lookup, and keep
			// sql.ErrNoRows visible to errors.Is.
			return nil, fmt.Errorf("gatewayedi: key '%s' not found for user '%s': %w",
				GatewayEdiKeyName, g.username, err)
		}
		// A lookup failure (connection lost, permission denied, malformed
		// query) is not a missing key: wrap the cause so errors.Is/As can still
		// see the database error.
		return nil, fmt.Errorf("gatewayedi: lookup key '%s' for user '%s': %w",
			GatewayEdiKeyName, g.username, err)
	}

	decrypted, err := crypto.DecryptPGP(data, key.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("gatewayedi: pgp decrypt: %w", err)
	}

	return decrypted, nil
}

// PGP armor markers. An armored block is recognised anywhere in the payload
// rather than only at its start: a mail relay or a payer's wrapper can prepend
// a preamble ("Received: from …") ahead of the armor.
var (
	pgpArmorBeginMarker = []byte("-----BEGIN PGP")
	pgpArmorEndMarker   = []byte("-----END PGP")
)

// looksEncrypted reports whether the payload is PGP ciphertext rather than
// already-readable remittance content, in either of the two forms the vendor
// sends:
//
//   - an ASCII-armored block (crypto.IsPGPEncrypted only accepted one at the
//     very start of the file);
//   - a binary OpenPGP message, which is what crypto.EncryptPGP produces and
//     what most producers emit unless explicitly armored.
//
// Anything else — XML, X12, free text, an empty file — is not ciphertext, so it
// is handed back untouched and the keyring is never queried for it.
func looksEncrypted(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	if bytes.Contains(data, pgpArmorBeginMarker) && bytes.Contains(data, pgpArmorEndMarker) {
		return true
	}
	return looksLikeBinaryOpenPGP(data)
}

// looksLikeBinaryOpenPGP reports whether the payload starts with the packet
// carrying an encrypted OpenPGP message. The high bit of an OpenPGP packet
// header is set (RFC 4880 §4.2), so printable remittance content fails the cheap
// check first; the packet is then parsed so a payload that merely starts with a
// non-ASCII byte (a UTF-8 BOM, say) is not mistaken for ciphertext.
func looksLikeBinaryOpenPGP(data []byte) bool {
	if len(data) == 0 || data[0] < 0x80 {
		return false
	}

	p, err := packet.NewReader(bytes.NewReader(data)).Next()
	if err != nil {
		return false
	}

	switch p.(type) {
	case *packet.EncryptedKey, // public-key encrypted session key
		*packet.SymmetricKeyEncrypted,  // passphrase encrypted session key
		*packet.SymmetricallyEncrypted: // encrypted data (with or without MDC)
		return true
	default:
		return false
	}
}

// GetEnabledConfigValue returns the config key that enables this scooper.
func (g *GatewayEdiSftpScooper) GetEnabledConfigValue() string {
	return GatewayEdiScooperEnabled
}

// SetContext sets the execution context.
func (g *GatewayEdiSftpScooper) SetContext(ctx context.Context) error {
	return g.SftpScooper.SetContext(ctx)
}
