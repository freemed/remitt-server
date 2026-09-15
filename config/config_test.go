package config

// config_test.go pins the SSH host key configuration surface: the two YAML keys
// added for the host key policy, and the deliberate absence of any default for
// either of them (SetDefaults must never enable the insecure bypass or invent a
// known_hosts path).

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppConfig_HostKeyPolicyKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "remitt.yml")
	body := "debug: true\n" +
		"paths:\n" +
		"  base: .\n" +
		"  known-hosts: /etc/remitt/known_hosts\n" +
		"sftp-insecure-ignore-hostkey: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	c, err := LoadConfigWithDefaults(path)
	if err != nil {
		t.Fatalf("LoadConfigWithDefaults() error = %v", err)
	}
	if got := c.Paths.KnownHostsPath; got != "/etc/remitt/known_hosts" {
		t.Errorf("Paths.KnownHostsPath = %q; want the value of the 'known-hosts' key", got)
	}
	if !c.SftpInsecureIgnoreHostKey {
		t.Error("SftpInsecureIgnoreHostKey = false; want the value of the 'sftp-insecure-ignore-hostkey' key")
	}

	// The shipped configuration shape (neither key present) must load as
	// "policy not configured", which is what makes the SSH code fail closed.
	plain := filepath.Join(dir, "plain.yml")
	if err := os.WriteFile(plain, []byte("debug: false\npaths:\n  base: .\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	c, err = LoadConfigWithDefaults(plain)
	if err != nil {
		t.Fatalf("LoadConfigWithDefaults() error = %v", err)
	}
	if c.Paths.KnownHostsPath != "" {
		t.Errorf("Paths.KnownHostsPath = %q with no key present; want empty", c.Paths.KnownHostsPath)
	}
	if c.SftpInsecureIgnoreHostKey {
		t.Error("SftpInsecureIgnoreHostKey = true with no key present; want false")
	}
}

func TestAppConfig_SetDefaults_LeavesHostKeyPolicyUnset(t *testing.T) {
	c := &AppConfig{}
	c.SetDefaults()

	if c.Paths.KnownHostsPath != "" {
		t.Errorf("SetDefaults() set Paths.KnownHostsPath = %q; there must be no default known_hosts file", c.Paths.KnownHostsPath)
	}
	if c.SftpInsecureIgnoreHostKey {
		t.Error("SetDefaults() enabled SftpInsecureIgnoreHostKey; host key verification must never be disabled by default")
	}
}
