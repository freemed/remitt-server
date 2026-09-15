package scooper

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
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

// TestGatewayEdiSftpScooper_PostProcess_PassesThroughNonPgpContent covers the
// first branch of the only logic gatewayedi.go runs before touching the
// network: content that does not look PGP-encrypted is returned verbatim and no
// key lookup happens. model.SqlDb is left nil to prove the database is never
// consulted on this path (gatewayedi.go:26-30).
func TestGatewayEdiSftpScooper_PostProcess_PassesThroughNonPgpContent(t *testing.T) {
	withNilSqlDb(t)

	g := &GatewayEdiSftpScooper{}

	tests := []struct {
		name string
		data []byte
	}{
		{"x12Payload", []byte("ISA*00*          *00*          *ZZ*SENDER         *ZZ*RECEIVER       *240101*1200*^*00501*000000001*0*P*:~")},
		{"plainText", []byte("Remittance advice for claim 12345\n")},
		{"emptyPayload", []byte{}},
		{"nilPayload", nil},
		{"armorMarkerNotAtStartOfMessage", []byte("X-GatewayEDI-Note: this is not -----BEGIN PGP MESSAGE-----\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if crypto.IsPGPEncrypted(tt.data) {
				t.Fatalf("fixture precondition failed: IsPGPEncrypted(%q) = true; this fixture does not exercise the passthrough branch", tt.data)
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
// key name "GatewayEDI" (gatewayedi.go:33-41, matching GEDI_KEYNAME in
// GatewayEdiSftpScooper.java:47), and a missing key is reported with both
// pieces of information instead of an empty failure.
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
	if !crypto.IsPGPEncrypted(armored) {
		t.Fatal("fixture precondition failed: the armored message is not detected as PGP-encrypted")
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
			if !crypto.IsPGPEncrypted(tt.data) {
				t.Fatalf("fixture precondition failed: IsPGPEncrypted(data) = false, so the decrypt branch is not reached")
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

// TestKnownBug_GatewayEdiBinaryPgpPayloadsAreNotDecrypted pins a real defect.
//
// gatewayedi.go:28 gates decryption on crypto.IsPGPEncrypted, which only
// matches data whose first non-blank characters are an armor header
// (crypto/pgp.go:81-85). crypto.EncryptPGP — the function this codebase uses to
// produce and parse PGP — emits a *binary* message, as do most real PGP
// producers unless explicitly armored. Such a file is therefore handed back to
// the caller as raw ciphertext and the user's key is never consulted, so
// remittance files are stored undecrypted. The Java original decrypts
// unconditionally (GatewayEdiSftpScooper.java:65-70). Documenting current
// behaviour; the desired behaviour is decryption of both forms.
func TestKnownBug_GatewayEdiBinaryPgpPayloadsAreNotDecrypted(t *testing.T) {
	publicKey, privateKey := scooperTestKeyPair(t)

	ciphertext, err := crypto.EncryptPGP([]byte("remittance payload"), publicKey)
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
			if crypto.IsPGPEncrypted(tt.data) {
				t.Fatalf("fixture precondition failed: IsPGPEncrypted = true, this payload is no longer affected by the gate")
			}

			fake := installFakeScooperDB(t)
			fake.setKeyringRows(fakeKeyringRecord(1, "user1", GatewayEdiKeyName, privateKey, publicKey))

			g := &GatewayEdiSftpScooper{}
			if err := g.SetUsername("user1"); err != nil {
				t.Fatalf("SetUsername() error = %v; want nil", err)
			}

			got, err := g.PostProcess(tt.data, "remit.pgp")
			if err != nil {
				t.Fatalf("PostProcess() error = %v; want nil (current behaviour: the payload is passed through)", err)
			}
			if !bytes.Equal(got, tt.data) {
				t.Errorf("PostProcess() = %q; want the ciphertext passed through unchanged (current behaviour)", got)
			}
			if n := fake.keyringQueryCount(); n != 0 {
				t.Errorf("tKeyring queries = %d; want 0 (the key is never looked up for this payload)", n)
			}
		})
	}
}

// TestKnownBug_KeyLookupErrorDropsUnderlyingCause pins a second defect:
// gatewayedi.go:39 replaces the database error with a formatted message that
// does not wrap it, so a connection or permission failure while reading
// tKeyring is indistinguishable from "the key does not exist". errors.Is cannot
// see the cause.
func TestKnownBug_KeyLookupErrorDropsUnderlyingCause(t *testing.T) {
	fake := installFakeScooperDB(t)
	sentinel := errors.New("fake keyring failure")
	fake.setKeyringQueryError(sentinel)

	armored := scooperArmorPGPMessage(t, []byte("not a real pgp packet"))

	g := &GatewayEdiSftpScooper{}
	if err := g.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	_, err := g.PostProcess(armored, "remit.pgp")
	if err == nil {
		t.Fatal("PostProcess() error = nil; want the keyring failure to be reported")
	}
	if errors.Is(err, sentinel) {
		t.Fatalf("PostProcess() error = %v wraps the driver error; the defect appears to have been fixed", err)
	}
	if strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("PostProcess() error = %q mentions the driver failure; the defect appears to have been fixed", err.Error())
	}
	if !strings.Contains(err.Error(), "not found for user 'user1'") {
		t.Errorf("PostProcess() error = %q; want the current message naming the key and user", err.Error())
	}
}

// TestKnownBug_GatewayEdiScoopDoesNotDispatchPostProcessOverride pins the
// Go-embedding defect that makes the decrypting PostProcess unreachable from a
// run.
//
// scooper/sftp.go:117 calls s.PostProcess(...) on the *SftpScooper receiver, and
// Go has no virtual dispatch for embedded structs: a call to
// (*GatewayEdiSftpScooper).Scoop is compiled into (*SftpScooper).Scoop, which
// statically calls (*SftpScooper).PostProcess. The override declared at
// gatewayedi.go:26 is therefore never used while scooping, so GatewayEDI files
// are persisted exactly as downloaded. The Java original relies on that
// override being called (GatewayEdiSftpScooper.java:64-70).
//
// The structural preconditions of the bug are asserted here; observing the
// bypass itself needs a live SFTP endpoint, which the skip at the end explains.
func TestKnownBug_GatewayEdiScoopDoesNotDispatchPostProcessOverride(t *testing.T) {
	outer := reflect.TypeOf(GatewayEdiSftpScooper{})
	if outer.NumField() != 1 {
		t.Fatalf("GatewayEdiSftpScooper has %d fields; want exactly 1 (the embedded SftpScooper)", outer.NumField())
	}
	field := outer.Field(0)
	if !field.Anonymous || field.Type != reflect.TypeOf(SftpScooper{}) {
		t.Fatalf("GatewayEdiSftpScooper field 0 is %s (anonymous=%v); want an anonymous value embed of scooper.SftpScooper", field.Type, field.Anonymous)
	}
	// A value embed is exactly what makes (*SftpScooper).Scoop receive
	// &g.SftpScooper and bind PostProcess statically.

	// The override itself is live: called directly it performs the keyring
	// lookup that the base implementation never does.
	fake := installFakeScooperDB(t)
	fake.setKeyringRows()

	armored := scooperArmorPGPMessage(t, []byte("not a real pgp packet"))
	g := &GatewayEdiSftpScooper{}
	if err := g.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}
	if _, err := g.PostProcess(armored, "remit.pgp"); err == nil {
		t.Fatal("PostProcess() error = nil; want the missing-key error from the GatewayEDI override")
	}
	if n := fake.keyringQueryCount(); n != 1 {
		t.Fatalf("tKeyring queries from a direct PostProcess call = %d; want 1 (the override is what performs the lookup)", n)
	}

	t.Skip("observing the bypass requires a live SSH/SFTP endpoint: sftp.go:73 calls ssh.Dial directly, no dialer is injectable, and the module's pkg/sftp provides a client only, so the file loop (and its s.PostProcess call at sftp.go:117) cannot be reached without standing up a real server")
}

// TestKnownBug_GatewayEdiScooperHasNoJavaConnectionDefaults pins a porting gap:
// the Java GatewayEdiSftpScooper overrides getHost/getPort/getPath with the
// vendor endpoint (sftp.gatewayedi.com, 22, "remits" —
// GatewayEdiSftpScooper.java:52-62). The Go port has neither those overrides nor
// any other defaults anywhere in the package, so a registry-instantiated
// GatewayEDI scooper is unconfigured and every Scoop() call fails with the
// generic host/port error until a caller supplies parameters. Documenting
// current behaviour; parameters here can only come from the plugin loader.
func TestKnownBug_GatewayEdiScooperHasNoJavaConnectionDefaults(t *testing.T) {
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

	if g.SftpScooper.host != "" || g.SftpScooper.port != 0 || g.SftpScooper.sftpPath != "" {
		t.Fatalf("fresh GatewayEDI scooper has host=%q port=%d path=%q; want all empty (no Java defaults in the Go port)",
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
}

// TestKnownBug_GatewayEdiPostProcessWithUninitializedDatabasePanics pins the
// same missing guard as the SftpScooper case, at its own call site:
// gatewayedi.go:33 dereferences model.SqlDb directly, so decrypting a PGP
// payload in a process where model.InitDb has not run panics instead of
// returning an error.
func TestKnownBug_GatewayEdiPostProcessWithUninitializedDatabasePanics(t *testing.T) {
	withNilSqlDb(t)

	g := &GatewayEdiSftpScooper{}
	if err := g.SetUsername("user1"); err != nil {
		t.Fatalf("SetUsername() error = %v; want nil", err)
	}

	armored := []byte(scooperBareArmorBlock)
	if !crypto.IsPGPEncrypted(armored) {
		t.Fatal("fixture precondition failed: the payload must be detected as PGP-encrypted to reach the key lookup")
	}

	var panicked any
	var err error
	func() {
		defer func() { panicked = recover() }()
		_, err = g.PostProcess(armored, "remit.pgp")
	}()

	if panicked == nil {
		t.Fatalf("PostProcess() returned err = %v without panicking; want the current behaviour (a nil model.SqlDb dereference at gatewayedi.go:33)", err)
	}
	if !strings.Contains(fmt.Sprint(panicked), "nil pointer dereference") {
		t.Errorf("PostProcess() panicked with %v; want a nil pointer dereference from the unguarded model.SqlDb use", panicked)
	}
}
