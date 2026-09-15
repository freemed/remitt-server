package common

// ssh_test.go pins the SSH host key policy (ssh.go): known_hosts when a path is
// configured, an explicit and loudly-warned insecure opt-in, known_hosts taking
// precedence when both are set, and a fail-closed error naming both options
// when neither is configured.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// withConfig installs a configuration for the duration of one test.
func withConfig(t *testing.T, knownHostsPath string, insecureIgnoreHostKey bool) {
	t.Helper()

	prev := config.Config
	cfg := config.AppConfig{}
	if prev != nil {
		cfg = *prev
	}
	cfg.Paths.KnownHostsPath = knownHostsPath
	cfg.SftpInsecureIgnoreHostKey = insecureIgnoreHostKey
	config.Config = &cfg
	t.Cleanup(func() { config.Config = prev })
}

func testPublicKey(t *testing.T) ssh.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh public key: %v", err)
	}
	return sshPub
}

func writeKnownHostsFile(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}
	return path
}

// TestHostKeyCallback_FailsClosedWithoutPolicy is the fail-closed contract:
// with neither option configured no callback can be produced, and the error
// names both options so an operator knows exactly what to set.
func TestHostKeyCallback_FailsClosedWithoutPolicy(t *testing.T) {
	callback, err := HostKeyCallbackFor("", false)
	if err == nil {
		t.Fatal("HostKeyCallbackFor(\"\", false) returned a nil error; want the fail-closed configuration error")
	}
	if callback != nil {
		t.Error("HostKeyCallbackFor(\"\", false) returned a callback alongside the error; want nil")
	}
	for _, want := range []string{
		"ssh host key verification is not configured",
		"paths.known-hosts",
		"sftp-insecure-ignore-hostkey",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q; want it to name %q", err.Error(), want)
		}
	}

	// An unloaded configuration is the same state, from the config-reading
	// entry point.
	prev := config.Config
	config.Config = nil
	t.Cleanup(func() { config.Config = prev })
	callback, err = HostKeyCallback()
	if err == nil || callback != nil {
		t.Fatalf("HostKeyCallback() with no configuration = (%v, %v); want (nil, error)", callback, err)
	}
	if !strings.Contains(err.Error(), "ssh host key verification is not configured") {
		t.Errorf("HostKeyCallback() error = %q; want the fail-closed configuration error", err.Error())
	}
}

// TestHostKeyCallback_InsecureOptInIsExplicitAndLoud pins the bypass: it has to
// be asked for, it is announced in the log, and without a known_hosts file it
// is the only way to obtain a callback.
func TestHostKeyCallback_InsecureOptInIsExplicitAndLoud(t *testing.T) {
	var logged bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	withConfig(t, "", true)

	callback, err := HostKeyCallback()
	if err != nil {
		t.Fatalf("HostKeyCallback() with the insecure opt-in error = %v; want a callback", err)
	}
	if callback == nil {
		t.Fatal("HostKeyCallback() with the insecure opt-in returned a nil callback")
	}
	if !strings.Contains(logged.String(), "HOST KEYS ARE NOT VERIFIED") {
		t.Errorf("log = %q; want the loud warning that host keys are not verified", logged.String())
	}
	if !strings.Contains(logged.String(), ConfigKeySftpInsecureIgnoreHostKey) {
		t.Errorf("log = %q; want it to name %q", logged.String(), ConfigKeySftpInsecureIgnoreHostKey)
	}

	// The callback accepts any host key - that is what the opt-in means.
	if err := callback("example.invalid:22", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, testPublicKey(t)); err != nil {
		t.Errorf("insecure callback rejected a key (%v); want any key accepted", err)
	}
}

// TestHostKeyCallback_KnownHostsIsUsedAndWinsOverTheOptIn pins that a
// configured known_hosts file is what verifies the peer, even when the opt-in
// is also set: an unknown host is then an error rather than a free pass.
func TestHostKeyCallback_KnownHostsIsUsedAndWinsOverTheOptIn(t *testing.T) {
	path := writeKnownHostsFile(t, knownhosts.Line([]string{"example.test"}, testPublicKey(t))+"\n")

	for _, insecure := range []bool{false, true} {
		withConfig(t, path, insecure)

		callback, err := HostKeyCallback()
		if err != nil {
			t.Fatalf("HostKeyCallback() with known_hosts %q (insecure=%v) error = %v; want a callback", path, insecure, err)
		}
		if callback == nil {
			t.Fatalf("HostKeyCallback() with known_hosts %q returned a nil callback", path)
		}

		// A key that is not the recorded one must be rejected: this is the
		// behaviour ssh.InsecureIgnoreHostKey() does not have.
		if err := callback("example.test:22", &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 22}, testPublicKey(t)); err == nil {
			t.Errorf("callback with known_hosts %q (insecure=%v) accepted an unknown key", path, insecure)
		}
	}
}

// TestHostKeyCallback_UnusableKnownHostsFileIsAnError pins that a known_hosts
// file which cannot be read or used is reported - never silently downgraded to
// "trust the peer".
func TestHostKeyCallback_UnusableKnownHostsFileIsAnError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	callback, err := HostKeyCallbackFor(missing, false)
	if err == nil {
		t.Fatal("HostKeyCallbackFor(missing path, false) returned a nil error; want it reported")
	}
	if callback != nil {
		t.Error("HostKeyCallbackFor(missing path, false) returned a callback alongside the error; want nil")
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error = %q; want it to name %q", err.Error(), missing)
	}

	// An unparseable line is an error too (knownhosts.New parses the file).
	bad := writeKnownHostsFile(t, "this is not a known_hosts line\n")
	callback, err = HostKeyCallbackFor(bad, true)
	if err == nil || callback != nil {
		t.Fatalf("HostKeyCallbackFor(unparseable file, true) = (%v, %v); want (nil, error)", callback, err)
	}
	if !strings.Contains(err.Error(), bad) {
		t.Errorf("error = %q; want it to name %q", err.Error(), bad)
	}
}
