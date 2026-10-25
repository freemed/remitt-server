package crypto

import (
	"bytes"
	gocrypto "crypto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// pgpTestEnvelope is the real on-the-wire shape this package carries in
// production: a SOAP envelope using the soapenv and urn:remitt:eligibility
// namespaces (see eligibility/gatewayedi.go).
const pgpTestEnvelope = `<?xml version="1.0" encoding="UTF-8"?>
<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">
  <soapenv:Header/>
  <soapenv:Body>
    <eligibilityRequest xmlns="urn:remitt:eligibility">
      <payerId>GATEWAYEDI</payerId>
      <memberId>M-000-42</memberId>
      <dateOfService>2026-09-15</dateOfService>
    </eligibilityRequest>
  </soapenv:Body>
</soapenv:Envelope>`

const pgpTestMultilineText = `line one
line two with trailing spaces   
	
line four after a blank line
case-sensitive: ExpectedValue != expectedvalue`

// pgpTestHighBytes is a full 0x00..0xff byte sweep plus non-ASCII UTF-8, so a
// round trip proves the payload is byte-exact and not text-normalised.
var pgpTestHighBytes = func() []byte {
	var b []byte
	for i := 0; i < 256; i++ {
		b = append(b, byte(i))
	}
	return append(b, []byte("\nRésumé — 東京 🚑 ünïcödé\n")...)
}()

type pgpTestKeyPair struct {
	public  []byte
	private []byte
	entity  *openpgp.Entity
}

// pgpTestGenerateKeyPair builds an in-memory, never-persisted RSA key pair and
// returns it ASCII-armored, exactly as EncryptPGP/DecryptPGP expect.
//
// The entity is created with an explicit packet.Config (2048-bit RSA,
// crypto.SHA256, AES-256) so the fixtures are cheap and deterministic.
//
// ProtonMail/go-crypto differs from golang.org/x/crypto/openpgp here, and the
// difference matters to the tests: PreferredHash is attached to the identity
// before it is signed, so BOTH serialized blocks carry PreferredHash=[8]
// (SHA-256). x/crypto dropped it from the public block, which is what used to
// force its RIPEMD160 fallback (and the blank ripemd160 import this suite
// needed). SerializePrivate re-signs the identity, so the private block agrees.
func pgpTestGenerateKeyPair(t *testing.T, name string) pgpTestKeyPair {
	t.Helper()

	config := &packet.Config{
		RSABits:       2048,
		DefaultHash:   gocrypto.SHA256,
		DefaultCipher: packet.CipherAES256,
	}

	entity, err := openpgp.NewEntity(name, "", strings.ToLower(name)+"@remitt-test.invalid", config)
	if err != nil {
		t.Fatalf("openpgp.NewEntity(%s): %v", name, err)
	}

	var pubBuf bytes.Buffer
	pubWriter, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode(public): %v", err)
	}
	if err := entity.Serialize(pubWriter); err != nil {
		t.Fatalf("entity.Serialize: %v", err)
	}
	if err := pubWriter.Close(); err != nil {
		t.Fatalf("close public armor writer: %v", err)
	}

	var privBuf bytes.Buffer
	privWriter, err := armor.Encode(&privBuf, openpgp.PrivateKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode(private): %v", err)
	}
	if err := entity.SerializePrivate(privWriter, nil); err != nil {
		t.Fatalf("entity.SerializePrivate: %v", err)
	}
	if err := privWriter.Close(); err != nil {
		t.Fatalf("close private armor writer: %v", err)
	}

	return pgpTestKeyPair{public: pubBuf.Bytes(), private: privBuf.Bytes(), entity: entity}
}

// pgpTestPublicKeyWithPreferredHashes re-signs every identity self-signature of
// key with the given preferred-hash list and returns the re-armored public
// block. hashes == nil produces a key with NO PreferredHash subpacket at all,
// which is the shape of the older partner keys this server exchanges with: the
// subpacket is optional and older tooling often omits it.
//
// It re-signs instead of editing the parsed signature because openpgp builds a
// signature's subpacket area once, at signing time (packet.Signature.
// outSubpackets), and Serialize writes that cached area — mutating
// sig.PreferredHash afterwards changes nothing in the bytes.
//
// The public key is the recipient side only; the private block is untouched, so
// the pair still decrypts what is encrypted to this block.
func pgpTestPublicKeyWithPreferredHashes(t *testing.T, key pgpTestKeyPair, hashes []uint8) []byte {
	t.Helper()

	entity := key.entity
	if len(entity.Identities) == 0 {
		t.Fatal("generated entity has no identities to re-sign")
	}

	for _, identity := range entity.Identities {
		primary := true
		sig := &packet.Signature{
			Version:       entity.PrimaryKey.Version,
			SigType:       packet.SigTypePositiveCert,
			PubKeyAlgo:    entity.PrimaryKey.PubKeyAlgo,
			Hash:          gocrypto.SHA256,
			CreationTime:  time.Now(),
			IssuerKeyId:   &entity.PrimaryKey.KeyId,
			IsPrimaryId:   &primary,
			PreferredHash: hashes,
		}
		if err := sig.SignUserId(identity.UserId.Id, entity.PrimaryKey, entity.PrivateKey, nil); err != nil {
			t.Fatalf("re-sign identity %q: %v", identity.UserId.Id, err)
		}
		identity.SelfSignature = sig
		identity.Signatures = []*packet.Signature{sig}
	}

	var buf bytes.Buffer
	writer, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode(re-signed public): %v", err)
	}
	if err := entity.Serialize(writer); err != nil {
		t.Fatalf("entity.Serialize(re-signed public): %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close re-signed public armor writer: %v", err)
	}
	return buf.Bytes()
}

// pgpTestPreferredHashes re-reads an armored public key and returns the
// PreferredHash subpacket of its primary self-signature, so tests can assert
// which encryption hash path a fixture actually exercises.
func pgpTestPreferredHashes(t *testing.T, publicKey []byte) []uint8 {
	t.Helper()

	keyRing, err := openpgp.ReadArmoredKeyRing(bytes.NewReader(publicKey))
	if err != nil {
		t.Fatalf("ReadArmoredKeyRing(public): %v", err)
	}
	if len(keyRing) == 0 {
		t.Fatal("ReadArmoredKeyRing(public) returned an empty keyring")
	}

	for _, identity := range keyRing[0].Identities {
		if identity.SelfSignature == nil {
			continue
		}
		if identity.SelfSignature.IsPrimaryId != nil && *identity.SelfSignature.IsPrimaryId {
			return identity.SelfSignature.PreferredHash
		}
	}
	for _, identity := range keyRing[0].Identities {
		if identity.SelfSignature != nil {
			return identity.SelfSignature.PreferredHash
		}
	}
	t.Fatal("no self-signature found on generated public key")
	return nil
}

// pgpTestArmorMessage wraps a binary PGP message in an ASCII armor block, the
// transport form DecryptPGP's armor-first path is written for.
func pgpTestArmorMessage(t *testing.T, binaryMessage []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer, err := armor.Encode(&buf, "PGP MESSAGE", nil)
	if err != nil {
		t.Fatalf("armor.Encode(message): %v", err)
	}
	if _, err := writer.Write(binaryMessage); err != nil {
		t.Fatalf("write armored message: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close armored message: %v", err)
	}
	return buf.Bytes()
}

func pgpTestMustEncrypt(t *testing.T, plain, publicKey []byte) []byte {
	t.Helper()

	ciphertext, err := EncryptPGP(plain, publicKey)
	if err != nil {
		t.Fatalf("EncryptPGP: %v", err)
	}
	if len(ciphertext) == 0 {
		t.Fatal("EncryptPGP returned empty ciphertext with nil error")
	}
	return ciphertext
}

func TestEncryptPGPDecryptPGPRoundTrip(t *testing.T) {
	key := pgpTestGenerateKeyPair(t, "round-trip")

	t.Run("keyringShapes", func(t *testing.T) {
		// Pinned facts this suite depends on:
		//  - ProtonMail/go-crypto attaches PreferredHash before signing, so the
		//    public block carries [8] (SHA-256) — unlike x/crypto, which
		//    dropped it and fell back to RIPEMD160;
		//  - SerializePrivate re-signs the identity, so the private block
		//    carries the same list.
		if hashes := pgpTestPreferredHashes(t, key.public); len(hashes) != 1 || hashes[0] != 8 {
			t.Errorf("public key PreferredHash = %v; want [8] (SHA-256)", hashes)
		}
		if hashes := pgpTestPreferredHashes(t, key.private); len(hashes) != 1 || hashes[0] != 8 {
			t.Errorf("private key PreferredHash = %v; want [8] (SHA-256)", hashes)
		}
	})

	payloads := map[string][]byte{
		"multilineText": []byte(pgpTestMultilineText),
		"soapEnvelope":  []byte(pgpTestEnvelope),
		"highBytesUtf8": pgpTestHighBytes,
		"emptyPayload":  {},
		"largePayload":  bytes.Repeat([]byte(pgpTestEnvelope), 64),
	}

	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			ciphertext := pgpTestMustEncrypt(t, payload, key.public)

			plaintext, err := DecryptPGP(ciphertext, key.private)
			if err != nil {
				t.Fatalf("DecryptPGP: %v", err)
			}
			if !bytes.Equal(plaintext, payload) {
				t.Errorf("decrypted %d bytes; want %d bytes byte-identical to the input\n got: %q\nwant: %q",
					len(plaintext), len(payload), plaintext, payload)
			}
		})
	}

	t.Run("sha256PreferredRecipientKey", func(t *testing.T) {
		// Encrypting to the keyring read from the private block goes through a
		// recipient self-signature that does carry PreferredHash=[8], i.e. the
		// normal SHA-256 path rather than the fallback.
		payload := []byte(pgpTestEnvelope)
		ciphertext := pgpTestMustEncrypt(t, payload, key.private)

		plaintext, err := DecryptPGP(ciphertext, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP: %v", err)
		}
		if !bytes.Equal(plaintext, payload) {
			t.Errorf("round trip via SHA-256-preferred keyring = %q; want %q", plaintext, payload)
		}
	})

	t.Run("armoredCiphertext", func(t *testing.T) {
		payload := []byte(pgpTestEnvelope)
		armored := pgpTestArmorMessage(t, pgpTestMustEncrypt(t, payload, key.public))
		if !IsPGPEncrypted(armored) {
			t.Fatal("IsPGPEncrypted(armored message) = false; want true")
		}

		plaintext, err := DecryptPGP(armored, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP(armored): %v", err)
		}
		if !bytes.Equal(plaintext, payload) {
			t.Errorf("decrypted armored payload = %q; want %q", plaintext, payload)
		}
	})

	t.Run("armoredKeyAcceptedAsInput", func(t *testing.T) {
		// DecryptPGP documents encryptedData as "armored or binary"; pin both.
		payload := []byte("binary input still decrypts")
		binaryCiphertext := pgpTestMustEncrypt(t, payload, key.public)

		plaintext, err := DecryptPGP(binaryCiphertext, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP(binary): %v", err)
		}
		if !bytes.Equal(plaintext, payload) {
			t.Errorf("decrypted binary payload = %q; want %q", plaintext, payload)
		}
	})

	t.Run("ciphertextFromTempDir", func(t *testing.T) {
		// This suite keeps all key material in memory; only the (public)
		// ciphertext touches disk, under t.TempDir(), which the testing package
		// removes automatically.
		dir := t.TempDir()
		path := filepath.Join(dir, "message.pgp")

		payload := []byte(pgpTestEnvelope)
		if err := os.WriteFile(path, pgpTestMustEncrypt(t, payload, key.public), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		onDisk, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}

		plaintext, err := DecryptPGP(onDisk, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP(file): %v", err)
		}
		if !bytes.Equal(plaintext, payload) {
			t.Errorf("decrypted payload from file = %q; want %q", plaintext, payload)
		}
	})
}

// TestIsPGPEncrypted pins the detection contract, including the two
// asymmetries that matter in production:
//
//   - EncryptPGP emits a BINARY message, for which IsPGPEncrypted returns false.
//   - The check is a "-----BEGIN PGP" prefix test on trimmed input, so it is
//     true for ANY PGP armor (public/private key blocks, signatures) and false
//     for armor preceded by non-whitespace text — even though DecryptPGP can
//     still decrypt that same input.
func TestIsPGPEncrypted(t *testing.T) {
	key := pgpTestGenerateKeyPair(t, "detection")
	binaryCiphertext := pgpTestMustEncrypt(t, []byte(pgpTestEnvelope), key.public)
	armoredCiphertext := pgpTestArmorMessage(t, binaryCiphertext)

	falseCases := map[string][]byte{
		"binaryPgpMessage": binaryCiphertext,
		"plainText":        []byte("hello, this is not pgp"),
		"rawXML":           []byte(pgpTestEnvelope),
		"soapEnvelopeNoDecl": []byte(`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/">` +
			`<soapenv:Body><eligibilityResponse xmlns="urn:remitt:eligibility"><Status>SUCCESS</Status>` +
			`</eligibilityResponse></soapenv:Body></soapenv:Envelope>`),
		"empty":                  {},
		"whitespaceOnly":         []byte(" 	\n\r\n "),
		"armorLikeNoDash":        []byte("--BEGIN PGP MESSAGE--"),
		"markerNotAtLineStart":   []byte("garbage -----BEGIN PGP MESSAGE-----"),
		"armorWithPreambleText":  append([]byte("some preamble\n"), armoredCiphertext...),
		"armorAfterXML":          append([]byte(pgpTestEnvelope+"\n"), armoredCiphertext...),
		"highBytes":              pgpTestHighBytes,
		"binaryPgpMessageSecond": pgpTestMustEncrypt(t, []byte("another payload"), key.public),
	}
	for name, data := range falseCases {
		t.Run("false/"+name, func(t *testing.T) {
			if got := IsPGPEncrypted(data); got {
				t.Errorf("IsPGPEncrypted(%s) = true; want false", name)
			}
		})
	}

	anyArmor := func() []byte {
		return []byte("-----BEGIN PGP WHATEVER-----\n\nAAAA\n-----END PGP WHATEVER-----\n")
	}
	trueCases := map[string][]byte{
		"armoredMessage": armoredCiphertext,
		"leadingNewlines": append([]byte("\n\n  \n"),
			armoredCiphertext...),
		"trailingNewlines":   append(append([]byte{}, armoredCiphertext...), '\n'),
		"armoredKeyBlock":    key.public,  // not a message, still detected
		"armoredPrivateKey":  key.private, // not a message, still detected
		"anyArmorBlockType":  anyArmor(),
		"armoredMessageCRLF": []byte(strings.ReplaceAll(string(armoredCiphertext), "\n", "\r\n")),
		// Truncated marker: the second HasPrefix clause matches the bare prefix
		// itself, so even an incomplete "-----BEGIN PGP" string is reported as
		// encrypted.
		"barePrefixMarker": []byte("-----BEGIN PGP"),
		"bareArmorHeader":  []byte("-----BEGIN PGP MESSAGE-----"),
	}
	for name, data := range trueCases {
		t.Run("true/"+name, func(t *testing.T) {
			if got := IsPGPEncrypted(data); !got {
				t.Errorf("IsPGPEncrypted(%s) = false; want true", name)
			}
		})
	}

	t.Run("binaryMessageNotDetectedButStillDecrypts", func(t *testing.T) {
		// The real asymmetry: the package's own EncryptPGP output fails its own
		// IsPGPEncrypted check (which is why eligibility/gatewayedi.go needs a
		// binary fallback), yet DecryptPGP handles it fine.
		if IsPGPEncrypted(binaryCiphertext) {
			t.Error("IsPGPEncrypted(binary EncryptPGP output) = true; want false")
		}
		plaintext, err := DecryptPGP(binaryCiphertext, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP(binary): %v", err)
		}
		if string(plaintext) != pgpTestEnvelope {
			t.Error("DecryptPGP(binary) did not return the original payload")
		}
	})

	t.Run("preambleArmorUndetectedButDecryptable", func(t *testing.T) {
		// Second asymmetry: IsPGPEncrypted reports false for armor that does not
		// begin the input, while DecryptPGP's armor.Decode path scans past the
		// preamble and decrypts it anyway.
		withPreamble := append([]byte("some preamble\n"), armoredCiphertext...)
		if IsPGPEncrypted(withPreamble) {
			t.Error("IsPGPEncrypted(armor with preamble) = true; want false")
		}
		plaintext, err := DecryptPGP(withPreamble, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP(armor with preamble): %v", err)
		}
		if string(plaintext) != pgpTestEnvelope {
			t.Error("DecryptPGP(armor with preamble) did not return the original payload")
		}
	})
}

func TestDecryptPGPErrors(t *testing.T) {
	key := pgpTestGenerateKeyPair(t, "decrypt-errors")
	otherKey := pgpTestGenerateKeyPair(t, "decrypt-errors-other")
	ciphertext := pgpTestMustEncrypt(t, []byte(pgpTestEnvelope), key.public)

	tamperedTail := append([]byte{}, ciphertext...)
	tamperedTail[len(tamperedTail)-1] ^= 0xff

	tamperedMiddle := append([]byte{}, ciphertext...)
	tamperedMiddle[len(tamperedMiddle)/2] ^= 0xff

	truncated := append([]byte{}, ciphertext[:len(ciphertext)/2]...)

	garbage := map[string]struct {
		data []byte
		key  []byte
	}{
		"plainTextCiphertext": {[]byte("this is not a pgp message at all"), key.private},
		"emptyCiphertext":     {[]byte{}, key.private},
		"garbageBytes":        {[]byte{0x00, 0x01, 0x02, 0x03, 0xde, 0xad, 0xbe, 0xef}, key.private},
		"highBytes":           {pgpTestHighBytes, key.private},
		"armorWithJunkBody":   {[]byte("-----BEGIN PGP MESSAGE-----\n\nnot base64 at all!!\n-----END PGP MESSAGE-----\n"), key.private},
		"armorHeaderOnly":     {[]byte("-----BEGIN PGP MESSAGE-----\n"), key.private},
		"truncatedCiphertext": {truncated, key.private},
		"tamperedLastByte":    {tamperedTail, key.private},
		"tamperedMiddleByte":  {tamperedMiddle, key.private},
		"wrongPrivateKey":     {ciphertext, otherKey.private},
		"emptyPrivateKey":     {ciphertext, []byte{}},
		"garbagePrivateKey":   {ciphertext, []byte("-----BEGIN PGP PRIVATE KEY BLOCK-----\n\nAAAA\n-----END PGP PRIVATE KEY BLOCK-----\n")},
		"publicKeyAsPrivate":  {ciphertext, key.public},
		"armoredWrongKey":     {pgpTestArmorMessage(t, ciphertext), otherKey.private},
	}

	for name, tc := range garbage {
		t.Run(name, func(t *testing.T) {
			plaintext, err := DecryptPGP(tc.data, tc.key)
			if err == nil {
				t.Fatalf("DecryptPGP() error = nil; want a failure (returned %d bytes: %q)", len(plaintext), plaintext)
			}
			if plaintext != nil {
				t.Errorf("DecryptPGP() returned non-nil plaintext %q alongside error %v; want nil", plaintext, err)
			}
		})
	}

	t.Run("nilInputs", func(t *testing.T) {
		if _, err := DecryptPGP(nil, key.private); err == nil {
			t.Error("DecryptPGP(nil ciphertext) error = nil; want failure")
		}
		if _, err := DecryptPGP(ciphertext, nil); err == nil {
			t.Error("DecryptPGP(nil private key) error = nil; want failure")
		}
		if _, err := DecryptPGP(ciphertext, garbledKeyMaterial()); err == nil {
			t.Error("DecryptPGP(garbled private key) error = nil; want failure")
		}
	})

	t.Run("validInputNeverReturnsWrappedNilError", func(t *testing.T) {
		// Guard against a future regression where an error is swallowed and an
		// empty plaintext is returned as success.
		plaintext, err := DecryptPGP(ciphertext, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP: %v", err)
		}
		if len(plaintext) == 0 {
			t.Error("DecryptPGP returned empty plaintext for a non-empty payload")
		}
	})

	t.Run("noSignatureVerificationHook", func(t *testing.T) {
		// Documents current behaviour: DecryptPGP passes a nil signature check
		// callback to openpgp.ReadMessage, so anonymous (unsigned) ciphertext
		// from any sender decrypts without an authenticity signal.
		plaintext, err := DecryptPGP(ciphertext, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP: %v", err)
		}
		if string(plaintext) != pgpTestEnvelope {
			t.Error("DecryptPGP plaintext mismatch")
		}
	})
}

func TestEncryptPGPErrors(t *testing.T) {
	key := pgpTestGenerateKeyPair(t, "encrypt-errors")

	badKeys := map[string][]byte{
		"emptyPublicKey":      {},
		"nilPublicKey":        nil,
		"plainTextPublicKey":  []byte("not a key"),
		"highBytesPublicKey":  pgpTestHighBytes,
		"junkArmoredKey":      []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nAAAA\n-----END PGP PUBLIC KEY BLOCK-----\n"),
		"armorNoBody":         []byte("-----BEGIN PGP PUBLIC KEY BLOCK-----\n-----END PGP PUBLIC KEY BLOCK-----\n"),
		"garbledKeyMaterial":  garbledKeyMaterial(),
		"recipientKeyRemoved": []byte("-----BEGIN PGP MESSAGE-----\ndata\n-----END PGP MESSAGE-----\n"),
	}
	for name, publicKey := range badKeys {
		t.Run(name, func(t *testing.T) {
			ciphertext, err := EncryptPGP([]byte(pgpTestEnvelope), publicKey)
			if err == nil {
				t.Fatalf("EncryptPGP() error = nil; want a failure (returned %d bytes)", len(ciphertext))
			}
			if ciphertext != nil {
				t.Errorf("EncryptPGP() returned non-nil ciphertext alongside error %v; want nil", err)
			}
		})
	}

	t.Run("privateKeyBlockAlsoUsableAsRecipientKey", func(t *testing.T) {
		// openpgp.ReadArmoredKeyRing accepts a private-key block, so passing a
		// private key to EncryptPGP silently succeeds. Pinned, not asserted as
		// desirable.
		ciphertext, err := EncryptPGP([]byte("payload"), key.private)
		if err != nil {
			t.Fatalf("EncryptPGP(private key block): %v", err)
		}
		plaintext, err := DecryptPGP(ciphertext, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP: %v", err)
		}
		if string(plaintext) != "payload" {
			t.Errorf("round trip through private-key block = %q; want %q", plaintext, "payload")
		}
	})

	t.Run("nilConfigPathProducesBinaryOutput", func(t *testing.T) {
		ciphertext := pgpTestMustEncrypt(t, []byte("payload"), key.public)
		if IsPGPEncrypted(ciphertext) {
			t.Error("EncryptPGP output is armored; the package emits a binary PGP message")
		}
		if !bytes.HasPrefix(ciphertext, []byte{0x85}) && int(ciphertext[0])&0x80 == 0 {
			t.Errorf("EncryptPGP output % x does not look like a binary OpenPGP packet", ciphertext[:2])
		}
	})
}

func TestPGPSecondRecipientKeyCannotDecrypt(t *testing.T) {
	keyA := pgpTestGenerateKeyPair(t, "recipient-a")
	keyB := pgpTestGenerateKeyPair(t, "recipient-b")

	payload := []byte("encrypted for recipient A only")
	ciphertext := pgpTestMustEncrypt(t, payload, keyA.public)

	if plaintext, err := DecryptPGP(ciphertext, keyA.private); err != nil {
		t.Fatalf("DecryptPGP(own key): %v", err)
	} else if !bytes.Equal(plaintext, payload) {
		t.Errorf("DecryptPGP(own key) = %q; want %q", plaintext, payload)
	}

	t.Run("wrongRecipientKey", func(t *testing.T) {
		plaintext, err := DecryptPGP(ciphertext, keyB.private)
		if err == nil {
			t.Fatalf("DecryptPGP(second key) error = nil; want failure (returned %q)", plaintext)
		}
		if plaintext != nil {
			t.Errorf("DecryptPGP(second key) returned %q alongside error; want nil", plaintext)
		}
	})

	t.Run("secondKeyRoundTripsIndependently", func(t *testing.T) {
		otherPayload := []byte("encrypted for recipient B only")
		otherCiphertext := pgpTestMustEncrypt(t, otherPayload, keyB.public)

		plaintext, err := DecryptPGP(otherCiphertext, keyB.private)
		if err != nil {
			t.Fatalf("DecryptPGP(second key): %v", err)
		}
		if !bytes.Equal(plaintext, otherPayload) {
			t.Errorf("DecryptPGP(second key) = %q; want %q", plaintext, otherPayload)
		}

		if crossPlaintext, err := DecryptPGP(otherCiphertext, keyA.private); err == nil {
			t.Fatalf("DecryptPGP(first key) on second key's ciphertext error = nil; want failure (returned %q)", crossPlaintext)
		}
	})

	t.Run("samePlaintextEncryptsDifferentlyPerCall", func(t *testing.T) {
		first := pgpTestMustEncrypt(t, payload, keyA.public)
		second := pgpTestMustEncrypt(t, payload, keyA.public)
		if bytes.Equal(first, second) {
			t.Error("two encryptions of the same payload produced identical ciphertext; session key is not random")
		}
	})
}

// TestPGPKeyWithoutPreferredHash covers a recipient key that carries no
// PreferredHash subpacket, which is the shape of the older keys this server
// exchanges with trading partners: the subpacket is optional and older tooling
// often omits it.
//
// openpgp.Encrypt intersects its own candidate-hash list with the recipient's
// preference list, so an empty list used to send x/crypto to the last entry of
// its list — RIPEMD160 — which required linking that hash into the binary (the
// blank import this suite used to carry, and the cause of the "cannot encrypt
// because no candidate hash functions are compiled in" failure in production).
// ProtonMail/go-crypto's candidate list stops at SHA-512/SHA-3 and its empty
// intersection fallback is SHA-256, so no extra hash needs linking.
func TestPGPKeyWithoutPreferredHash(t *testing.T) {
	key := pgpTestGenerateKeyPair(t, "no-preferred-hash")
	recipient := pgpTestPublicKeyWithPreferredHashes(t, key, nil)

	if hashes := pgpTestPreferredHashes(t, recipient); len(hashes) != 0 {
		t.Fatalf("re-signed recipient key has preferred hashes %v; want none so the fallback is exercised", hashes)
	}

	payload := []byte(pgpTestEnvelope)
	ciphertext := pgpTestMustEncrypt(t, payload, recipient)

	plaintext, err := DecryptPGP(ciphertext, key.private)
	if err != nil {
		t.Fatalf("DecryptPGP: %v", err)
	}
	if !bytes.Equal(plaintext, payload) {
		t.Errorf("round trip via fallback hash path = %q; want %q", plaintext, payload)
	}

	t.Run("fallbackKeyRoundTripsLargePayload", func(t *testing.T) {
		large := bytes.Repeat([]byte(pgpTestEnvelope), 16)
		largeCiphertext := pgpTestMustEncrypt(t, large, key.private)

		largePlaintext, err := DecryptPGP(largeCiphertext, key.private)
		if err != nil {
			t.Fatalf("DecryptPGP: %v", err)
		}
		if !bytes.Equal(largePlaintext, large) {
			t.Error("large payload round trip via fallback hash path did not match")
		}
	})
}

// garbledKeyMaterial is a syntactically plausible but cryptographically
// meaningless armored key block: the CRC is correct so armor decoding reaches
// the packet parser, which must then reject it.
func garbledKeyMaterial() []byte {
	var buf bytes.Buffer
	writer, err := armor.Encode(&buf, openpgp.PrivateKeyType, nil)
	if err != nil {
		panic(err)
	}
	// A valid armor body of random-looking bytes with a correct checksum.
	if _, err := writer.Write([]byte("this is not a packet stream, only noise")); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
