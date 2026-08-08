package crypto

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"
)

// DecryptPGP decrypts a PGP-encrypted message using the provided private key.
// encryptedData is the raw PGP message (armored or binary).
// privateKeyData is the ASCII-armored private key.
func DecryptPGP(encryptedData, privateKeyData []byte) ([]byte, error) {
	// Read private key
	keyRing, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(privateKeyData))
	if err != nil {
		return nil, fmt.Errorf("pgp: read keyring: %w", err)
	}

	if len(keyRing) == 0 {
		return nil, fmt.Errorf("pgp: no keys found in keyring")
	}

	// Try to read as armored, fall back to binary
	var messageReader io.Reader
	block, err := armor.Decode(bytes.NewReader(encryptedData))
	if err != nil {
		messageReader = bytes.NewReader(encryptedData)
	} else {
		messageReader = block.Body
	}

	md, err := openpgp.ReadMessage(messageReader, keyRing, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("pgp: read message: %w", err)
	}

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, md.UnverifiedBody); err != nil {
		return nil, fmt.Errorf("pgp: read body: %w", err)
	}

	return buf.Bytes(), nil
}

// EncryptPGP encrypts a message using the provided public key.
// plainData is the raw message to encrypt.
// publicKeyData is the ASCII-armored public key.
func EncryptPGP(plainData, publicKeyData []byte) ([]byte, error) {
	// Read public key
	keyRing, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(publicKeyData))
	if err != nil {
		return nil, fmt.Errorf("pgp: read keyring: %w", err)
	}

	if len(keyRing) == 0 {
		return nil, fmt.Errorf("pgp: no keys found in keyring")
	}

	var buf bytes.Buffer
	w, err := openpgp.Encrypt(&buf, keyRing, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("pgp: encrypt: %w", err)
	}

	if _, err := w.Write(plainData); err != nil {
		return nil, fmt.Errorf("pgp: write: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("pgp: close: %w", err)
	}

	return buf.Bytes(), nil
}

// IsPGPEncrypted performs a basic check for PGP message markers.
func IsPGPEncrypted(data []byte) bool {
	s := strings.TrimSpace(string(data))
	return strings.HasPrefix(s, "-----BEGIN PGP MESSAGE-----") ||
		strings.HasPrefix(s, "-----BEGIN PGP")
}
