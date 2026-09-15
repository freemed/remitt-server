package scooper

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/crypto"
	"golang.org/x/crypto/openpgp"
	"golang.org/x/crypto/openpgp/armor"

	// golang.org/x/crypto/openpgp falls back to RIPEMD160 (the last entry of
	// its candidate list) when a recipient key carries no preferred-hash
	// subpacket, and the hash must be linked into the binary for
	// openpgp.Encrypt to succeed. Linking it here keeps these fixtures on the
	// same crypto.EncryptPGP / crypto.DecryptPGP path the scooper uses.
	_ "golang.org/x/crypto/ripemd160"
)

// scooperTestKeyPair generates an OpenPGP key pair and returns the armored
// public and private keys, the shape tKeyring stores.
func scooperTestKeyPair(t *testing.T) (publicKey, privateKey []byte) {
	t.Helper()

	entity, err := openpgp.NewEntity("Scooper Test", "", "scooper-test@example.invalid", nil)
	if err != nil {
		t.Fatalf("openpgp.NewEntity: %v", err)
	}

	var pubBuf bytes.Buffer
	pubWriter, err := armor.Encode(&pubBuf, openpgp.PublicKeyType, nil)
	if err != nil {
		t.Fatalf("armor.Encode(public): %v", err)
	}
	if err := entity.Serialize(pubWriter); err != nil {
		t.Fatalf("Serialize(public): %v", err)
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
		t.Fatalf("SerializePrivate: %v", err)
	}
	if err := privWriter.Close(); err != nil {
		t.Fatalf("close private armor writer: %v", err)
	}

	return pubBuf.Bytes(), privBuf.Bytes()
}

// scooperArmorPGPMessage wraps a binary OpenPGP message in ASCII armor, the way
// a payer that armors its remittance files would.
func scooperArmorPGPMessage(t *testing.T, binaryMessage []byte) []byte {
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

// scooperBareArmorBlock is an armor block with a plausible header but no
// usable OpenPGP packet inside.
const scooperBareArmorBlock = "-----BEGIN PGP MESSAGE-----\n\nAAAA\n-----END PGP MESSAGE-----\n"

// newConfiguredGatewayEdiScooper builds a GatewayEdiSftpScooper with the
// parameters the plugin loader would hand over from tUserConfig.
func newConfiguredGatewayEdiScooper(t *testing.T, user string) *GatewayEdiSftpScooper {
	t.Helper()

	g := &GatewayEdiSftpScooper{}
	if err := g.SetParameters(map[string]string{
		"sftpHost":     "sftp.gatewayedi.invalid",
		"sftpPort":     "22",
		"sftpUsername": "sftpuser",
		"sftpPassword": "sftppass",
		"sftpPath":     "remits",
	}); err != nil {
		t.Fatalf("SetParameters() error = %v; want nil", err)
	}
	if err := g.SetUsername(user); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}
	return g
}

// TestGatewayEdiSftpScooper_PostProcess_PassesThroughNonPgpContent covers the
// first branch of the only logic gatewayedi.go runs before touching the
// network: content that is not PGP ciphertext is returned verbatim and no key
// lookup happens. model.SqlDb is left nil to prove the database is never
// consulted on this path.
func TestGatewayEdiSftpScooper_PostProcess_PassesThroughNonPgpContent(t *testing.T) {
	withNilSqlDb(t)

	g := &GatewayEdiSftpScooper{}

	tests := []struct {
		name string
		data []byte
	}{
		{"x12Payload", []byte("ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER       *240101*1200*^*00501*000000001*0*P*:~")},
		{"plainText", []byte("Remittance advice for claim 12345\n")},
		{"plainXml", []byte("<remittance><claim id=\"12345\"/></remittance>")},
		{"emptyPayload", []byte{}},
		{"nilPayload", nil},
		{"armorMarkerNotAtStartOfMessage", []byte("X-GatewayEDI-Note: this is not -----BEGIN PGP MESSAGE-----\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if looksEncrypted(tt.data) {
				t.Fatalf("fixture precondition failed: looksEncrypted(%q) = true; this fixture does not exercise the passthrough branch", tt.data)
			}

			got, err := g.PostProcess(tt.data, "remit.edi")
			if err != nil {
				t.Fatalf("PostProcess() error = %v; want nil", err)
			}
			if !bytes.Equal(got, tt.data) {
				t.Errorf("PostProcess() = %q; want the input unchanged (%q)", got, tt.data)
			}
		})
	}
}

// TestGatewayEdiSftpScooper_PostProcess_LooksUpKeyByUserAndName pins the keyring
// contract: the GatewayEDI key is looked up by the run's username and the fixed
// key name "GatewayEDI" (matching GEDI_KEYNAME in
// GatewayEdiSftpScooper.java:47), and an absent key is reported with both pieces
// of information — distinguishable by errors.Is from a lookup that failed.
func TestGatewayEdiSftpScooper_PostProcess_LooksUpKeyByUserAndName(t *testing.T) {
	fake := installFakeScooperDB(t)
	fake.setKeyringRows() // key is not present in tKeyring

	armored := scooperArmorPGPMessage(t, []byte("not a real pgp packet"))

	g := &GatewayEdiSftpScooper{}
	if err := g.SetUsername("user42"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	got, err := g.PostProcess(armored, "remit.pgp")
	if err == nil {
		t.Fatal("PostProcess() error = nil; want a missing-key error")
	}
	if got != nil {
		t.Errorf("PostProcess() = %q; want nil alongside the error", got)
	}

	wantMessage := "gatewayedi: key '" + GatewayEdiKeyName + "' not found for user 'user42'"
	if !strings.Contains(err.Error(), wantMessage) {
		t.Errorf("PostProcess() error = %q; want it to contain %q", err.Error(), wantMessage)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("PostProcess() error = %v; want it to wrap sql.ErrNoRows so an absent key is distinguishable from a failed lookup", err)
	}

	if n := fake.keyringQueryCount(); n != 1 {
		t.Errorf("tKeyring queries = %d; want exactly 1", n)
	}
	args := fake.keyringQueryArgs()
	want := []any{"user42", GatewayEdiKeyName}
	if len(args) != len(want) {
		t.Fatalf("tKeyring query args = %v; want %v", args, want)
	}
	for i := range want {
		if fmt.Sprint(args[i]) != fmt.Sprint(want[i]) {
			t.Errorf("tKeyring query arg %d = %v; want %v", i, args[i], want[i])
		}
	}
}

// TestGatewayEdiSftpScooper_PostProcess_DecryptsArmoredMessage is the happy
// path end to end minus the SFTP download: an armored PGP message encrypted to
// the user's stored GatewayEDI key comes back as plaintext.
func TestGatewayEdiSftpScooper_PostProcess_DecryptsArmoredMessage(t *testing.T) {
	const plaintext = "ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~"

	publicKey, privateKey := scooperTestKeyPair(t)

	ciphertext, err := crypto.EncryptPGP([]byte(plaintext), publicKey)
	if err != nil {
		t.Fatalf("crypto.EncryptPGP: %v", err)
	}
	armored := scooperArmorPGPMessage(t, ciphertext)
	if !looksEncrypted(armored) {
		t.Fatal("fixture precondition failed: the armored message is not detected as PGP ciphertext")
	}

	fake := installFakeScooperDB(t)
	fake.setKeyringRows(fakeKeyringRecord(7, "user1", GatewayEdiKeyName, privateKey, publicKey))

	g := &GatewayEdiSftpScooper{}
	if err := g.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	got, err := g.PostProcess(armored, "remit.pgp")
	if err != nil {
		t.Fatalf("PostProcess() error = %v; want nil", err)
	}
	if string(got) != plaintext {
		t.Errorf("PostProcess() = %q; want the decrypted payload %q", got, plaintext)
	}
}

// TestGatewayEdiSftpScooper_PostProcess_PropagatesDecryptErrors covers the
// failure modes downstream of the key lookup: every one must be reported as a
// "gatewayedi: pgp decrypt" error rather than silently returning ciphertext.
func TestGatewayEdiSftpScooper_PostProcess_PropagatesDecryptErrors(t *testing.T) {
	publicKey, privateKey := scooperTestKeyPair(t)
	_, otherPrivateKey := scooperTestKeyPair(t)

	ciphertext, err := crypto.EncryptPGP([]byte("remittance payload"), publicKey)
	if err != nil {
		t.Fatalf("crypto.EncryptPGP: %v", err)
	}
	armored := scooperArmorPGPMessage(t, ciphertext)

	tests := []struct {
		name     string
		data     []byte
		key      []byte
		wantPart string
	}{
		{
			name:     "wrongPrivateKey",
			data:     armored,
			key:      otherPrivateKey,
			wantPart: "gatewayedi: pgp decrypt:",
		},
		{
			name:     "garbagePrivateKey",
			data:     armored,
			key:      []byte("this is not an armored private key"),
			wantPart: "gatewayedi: pgp decrypt: pgp: read keyring:",
		},
		{
			name:     "emptyPrivateKey",
			data:     armored,
			key:      nil,
			wantPart: "gatewayedi: pgp decrypt: pgp: read keyring:",
		},
		{
			name:     "armoredBlockWithNoPacket",
			data:     []byte(scooperBareArmorBlock),
			key:      privateKey,
			wantPart: "gatewayedi: pgp decrypt:",
		},
		{
			name:     "publicKeyBlockTreatedAsMessage",
			data:     publicKey,
			key:      privateKey,
			wantPart: "gatewayedi: pgp decrypt:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !looksEncrypted(tt.data) {
				t.Fatalf("fixture precondition failed: looksEncrypted(data) = false, so the decrypt branch is not reached")
			}

			fake := installFakeScooperDB(t)
			fake.setKeyringRows(fakeKeyringRecord(1, "user1", GatewayEdiKeyName, tt.key, publicKey))

			g := &GatewayEdiSftpScooper{}
			if err := g.SetUsername("user1"); err != nil {
				t.Fatalf("SetUsername() error = %v; want nil", err)
			}

			got, err := g.PostProcess(tt.data, "remit.pgp")
			if err == nil {
				t.Fatalf("PostProcess() error = nil (returned %q); want a decrypt failure", got)
			}
			if got != nil {
				t.Errorf("PostProcess() = %q; want nil alongside the error", got)
			}
			if !strings.Contains(err.Error(), tt.wantPart) {
				t.Errorf("PostProcess() error = %q; want it to contain %q", err.Error(), tt.wantPart)
			}
		})
	}
}

// TestGatewayEdiSftpScooper_PostProcess_DecryptsBinaryAndPreambleArmoredPayloads
// is the regression test for the decryption gate.
//
// Decryption used to be gated on crypto.IsPGPEncrypted, which only matches a
// payload whose first non-blank characters are an armor header
// (crypto/pgp.go:81-85). crypto.EncryptPGP — the function this codebase uses to
// produce and parse PGP — emits a *binary* message, as do most real PGP
// producers unless explicitly armored, and a relay can put a transport preamble
// ahead of an armored block. Both forms were therefore handed back as raw
// ciphertext with the user's key never consulted, so remittance files were
// stored undecrypted (the Java original decrypts unconditionally,
// GatewayEdiSftpScooper.java:65-70).
func TestGatewayEdiSftpScooper_PostProcess_DecryptsBinaryAndPreambleArmoredPayloads(t *testing.T) {
	const plaintext = "ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~"

	publicKey, privateKey := scooperTestKeyPair(t)

	ciphertext, err := crypto.EncryptPGP([]byte(plaintext), publicKey)
	if err != nil {
		t.Fatalf("crypto.EncryptPGP: %v", err)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{"binaryPgpMessage", ciphertext},
		{"armoredMessageBehindPreamble", append([]byte("Received: from payer.example.invalid\n"), scooperArmorPGPMessage(t, ciphertext)...)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !looksEncrypted(tt.data) {
				t.Fatalf("fixture precondition failed: looksEncrypted = false, so the decrypt branch is not reached")
			}

			fake := installFakeScooperDB(t)
			fake.setKeyringRows(fakeKeyringRecord(1, "user1", GatewayEdiKeyName, privateKey, publicKey))

			g := &GatewayEdiSftpScooper{}
			if err := g.SetUsername("user1"); err != nil {
				t.Fatalf("SetUsername() error = %v; want nil", err)
			}

			got, err := g.PostProcess(tt.data, "remit.pgp")
			if err != nil {
				t.Fatalf("PostProcess() error = %v; want the payload decrypted", err)
			}
			if string(got) != plaintext {
				t.Errorf("PostProcess() = %q; want the decrypted payload %q", got, plaintext)
			}
			if n := fake.keyringQueryCount(); n != 1 {
				t.Errorf("tKeyring queries = %d; want 1 (the user's key must be consulted)", n)
			}
		})
	}
}

// TestGatewayEdiSftpScooper_PostProcess_WrapsKeyLookupFailure is the regression
// test for the dropped cause: gatewayedi.go replaced the tKeyring lookup error
// with a "key not found" message that did not wrap it, so a connection or
// permission failure was indistinguishable from an absent key and errors.Is
// could not see the database error.
func TestGatewayEdiSftpScooper_PostProcess_WrapsKeyLookupFailure(t *testing.T) {
	fake := installFakeScooperDB(t)
	sentinel := errors.New("fake keyring failure")
	fake.setKeyringQueryError(sentinel)

	armored := scooperArmorPGPMessage(t, []byte("not a real pgp packet"))

	g := &GatewayEdiSftpScooper{}
	if err := g.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	got, err := g.PostProcess(armored, "remit.pgp")
	if err == nil {
		t.Fatal("PostProcess() error = nil; want the keyring failure to be reported")
	}
	if got != nil {
		t.Errorf("PostProcess() = %q; want nil alongside the error", got)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("PostProcess() error = %v; want it to wrap the driver error so errors.Is works", err)
	}
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Errorf("PostProcess() error = %q; want it to mention the database failure %q", err.Error(), sentinel.Error())
	}
	if !strings.Contains(err.Error(), GatewayEdiKeyName) || !strings.Contains(err.Error(), "user1") {
		t.Errorf("PostProcess() error = %q; want it to name the key %q and the user", err.Error(), GatewayEdiKeyName)
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Errorf("PostProcess() error = %v looks like an absent key; want the lookup failure reported as such", err)
	}
}

// TestGatewayEdiSftpScooper_ScoopRunsThePostProcessOverride is the regression
// test for the Go-embedding defect.
//
// GatewayEdiSftpScooper embeds SftpScooper by value, and Go has no virtual
// dispatch for embedded structs: a plain g.Scoop() would run
// (*SftpScooper).Scoop with the embedded SftpScooper as receiver, statically
// calling (*SftpScooper).PostProcess, so the decrypting override was never used
// while scooping and GatewayEDI remittances were persisted exactly as
// downloaded (the Java original relies on that override being called,
// GatewayEdiSftpScooper.java:64-70).
//
// The run is driven end to end — parameters, dedupe query, file download,
// transform, tScooper insert — through the in-memory SFTP session, so the
// assertion is on what a Scoop actually stores. No SSH or SFTP server is
// started.
func TestGatewayEdiSftpScooper_ScoopRunsThePostProcessOverride(t *testing.T) {
	const plaintext = "ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~"

	publicKey, privateKey := scooperTestKeyPair(t)
	ciphertext, err := crypto.EncryptPGP([]byte(plaintext), publicKey)
	if err != nil {
		t.Fatalf("crypto.EncryptPGP: %v", err)
	}

	fake := installFakeScooperDB(t)
	fake.setKeyringRows(fakeKeyringRecord(1, "user1", GatewayEdiKeyName, privateKey, publicKey))

	g := newConfiguredGatewayEdiScooper(t, "user1")
	session := withFakeSftpSession(&g.SftpScooper, map[string][]byte{"remit.pgp": ciphertext})

	results, err := g.Scoop()
	if err != nil {
		t.Fatalf("Scoop() error = %v; want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("Scoop() returned %d results; want 1", len(results))
	}
	if string(results[0].Content) != plaintext {
		t.Errorf("Scoop() returned content %q; want the decrypted remittance — the PostProcess override did not run", results[0].Content)
	}
	if n := fake.keyringQueryCount(); n != 1 {
		t.Errorf("tKeyring queries during the scoop = %d; want exactly 1 (the override decrypts)", n)
	}

	if got := fake.insertCount(); got != 1 {
		t.Fatalf("tScooper inserts = %d; want exactly 1", got)
	}
	args := fake.insertedArgs()
	if len(args) != 7 {
		t.Fatalf("tScooper insert args = %v; want 7 values", args)
	}
	if got := fmt.Sprint(args[5]); got != "remit.pgp" {
		t.Errorf("inserted filename = %v; want remit.pgp", got)
	}
	stored, ok := args[6].([]byte)
	if !ok {
		t.Fatalf("inserted content is %T; want []byte", args[6])
	}
	if string(stored) != plaintext {
		t.Errorf("stored content = %q; want the decrypted remittance — the GatewayEDI PostProcess override was bypassed", stored)
	}
	if !session.wasClosed() {
		t.Error("the SFTP session was not closed after the scoop")
	}

	// Control: the same transport and the same ciphertext through the base
	// scooper store the bytes unchanged and never consult the keyring. That is
	// the behaviour the override exists to avoid, so it must differ from the
	// GatewayEDI run above.
	baseFake := installFakeScooperDB(t)
	base := newConfiguredSftpScooper(t, "user1", "sftp.example.invalid", 22, "remits")
	withFakeSftpSession(base, map[string][]byte{"remit.pgp": ciphertext})

	baseResults, err := base.Scoop()
	if err != nil {
		t.Fatalf("base Scoop() error = %v; want nil", err)
	}
	if len(baseResults) != 1 {
		t.Fatalf("base Scoop() returned %d results; want 1", len(baseResults))
	}
	if !bytes.Equal(baseResults[0].Content, ciphertext) {
		t.Errorf("base Scoop() content = %q; want the ciphertext unchanged without the override", baseResults[0].Content)
	}
	if n := baseFake.keyringQueryCount(); n != 0 {
		t.Errorf("base tKeyring queries = %d; want 0", n)
	}
}

// TestGatewayEdiScooper_JavaDefaultsAreDocumentedAndOptIn covers the last porting
// gap: the Java GatewayEdiSftpScooper overrides getHost/getPort/getPath with the
// vendor endpoint (sftp.gatewayedi.com, 22, "remits" —
// GatewayEdiSftpScooper.java:52-62), while the Go port has no such overrides.
//
// The corrected contract is not "hardcode the vendor endpoint": a fresh
// GatewayEDI scooper stays unconfigured and says so, and the documented Java
// values take effect only when a caller passes them explicitly.
func TestGatewayEdiScooper_JavaDefaultsAreDocumentedAndOptIn(t *testing.T) {
	if GatewayEdiJavaDefaultHost != "sftp.gatewayedi.com" || GatewayEdiJavaDefaultPort != 22 || GatewayEdiJavaDefaultPath != "remits" {
		t.Fatalf("documented Java defaults = %q/%d/%q; want the values hardcoded in GatewayEdiSftpScooper.java:52-62",
			GatewayEdiJavaDefaultHost, GatewayEdiJavaDefaultPort, GatewayEdiJavaDefaultPath)
	}

	withNilSqlDb(t)

	s, err := InstantiateScooper(GatewayEdiScooperClass)
	if err != nil {
		t.Fatalf("InstantiateScooper(%q) error = %v", GatewayEdiScooperClass, err)
	}
	g, ok := s.(*GatewayEdiSftpScooper)
	if !ok {
		t.Fatalf("InstantiateScooper(%q) = %T; want *scooper.GatewayEdiSftpScooper", GatewayEdiScooperClass, s)
	}
	if err := g.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	// No silent defaults: a registry-built scooper with no parameters is
	// unconfigured, and says so rather than dialling a vendor endpoint.
	if g.SftpScooper.host != "" || g.SftpScooper.port != 0 || g.SftpScooper.sftpPath != "" {
		t.Fatalf("fresh GatewayEDI scooper has host=%q port=%d path=%q; want all empty (no default may be assumed silently)",
			g.SftpScooper.host, g.SftpScooper.port, g.SftpScooper.sftpPath)
	}

	results, err := g.Scoop()
	if err == nil {
		t.Fatal("Scoop() error = nil; want the host/port configuration error for an unconfigured GatewayEDI scooper")
	}
	if !strings.Contains(err.Error(), "sftpscooper: host/port not configured") {
		t.Errorf("Scoop() error = %q; want the generic host/port error", err.Error())
	}
	if results != nil {
		t.Errorf("Scoop() results = %v; want nil alongside the error", results)
	}

	// Opting in explicitly configures the scooper; credentials still come from
	// configuration.
	params := GatewayEdiJavaDefaultParams()
	params["sftpUsername"] = "sftpuser"
	params["sftpPassword"] = "sftppass"
	if err := g.SetParameters(params); err != nil {
		t.Fatalf("SetParameters(defaults) error = %v; want nil", err)
	}
	if g.SftpScooper.host != GatewayEdiJavaDefaultHost || g.SftpScooper.port != GatewayEdiJavaDefaultPort || g.SftpScooper.sftpPath != GatewayEdiJavaDefaultPath {
		t.Errorf("after opting in: host=%q port=%d path=%q; want %q/%d/%q",
			g.SftpScooper.host, g.SftpScooper.port, g.SftpScooper.sftpPath,
			GatewayEdiJavaDefaultHost, GatewayEdiJavaDefaultPort, GatewayEdiJavaDefaultPath)
	}
	// validateConfig only inspects configuration, so this asserts the scooper is
	// usable without contacting the vendor endpoint.
	if err := g.SftpScooper.validateConfig(); err != nil {
		t.Errorf("validateConfig() after opting in = %v; want nil", err)
	}
}

// TestGatewayEdiSftpScooper_PostProcessWithUninitializedDatabaseReturnsError is
// the regression test for the missing guard at its own call site: decrypting a
// PGP payload in a process where model.InitDb has not run must report the
// database rather than nil-dereference model.SqlDb.
func TestGatewayEdiSftpScooper_PostProcessWithUninitializedDatabaseReturnsError(t *testing.T) {
	withNilSqlDb(t)

	g := &GatewayEdiSftpScooper{}
	if err := g.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	armored := []byte(scooperBareArmorBlock)
	if !looksEncrypted(armored) {
		t.Fatal("fixture precondition failed: the payload must be detected as PGP ciphertext to reach the key lookup")
	}

	got, err := g.PostProcess(armored, "remit.pgp")
	if err == nil {
		t.Fatal("PostProcess() error = nil; want a database-not-initialised error instead of a nil dereference")
	}
	if got != nil {
		t.Errorf("PostProcess() = %q; want nil alongside the error", got)
	}
	if !strings.Contains(err.Error(), "gatewayedi: database not initialized") {
		t.Errorf("PostProcess() error = %q; want it to contain %q", err.Error(), "gatewayedi: database not initialized")
	}
}
