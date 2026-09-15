package common

import (
	"fmt"
	"log"

	"github.com/freemed/remitt-server/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	// ConfigKeyKnownHosts is the configuration key (and the YAML path under
	// `paths`) naming the OpenSSH known_hosts file a host key is verified
	// against.
	ConfigKeyKnownHosts = "paths.known-hosts"
	// ConfigKeySftpInsecureIgnoreHostKey is the configuration key (top level)
	// that explicitly opts out of host key verification.
	ConfigKeySftpInsecureIgnoreHostKey = "sftp-insecure-ignore-hostkey"

	// hostKeyNotConfigured is the fail-closed message prefix. It is a
	// constant so callers and tests can match on it without restating the
	// wording; the full error names both options (see hostKeyPolicyError).
	hostKeyNotConfigured = "ssh host key verification is not configured"
)

// hostKeyPolicyError is the fail-closed error: neither a known_hosts file nor
// the explicit insecure opt-in is configured, so no trust decision can be made
// and the connection must not be attempted.
func hostKeyPolicyError() error {
	return fmt.Errorf(
		"%s: set '%s' to an OpenSSH known_hosts file, or explicitly accept unverified host keys with '%s: true'",
		hostKeyNotConfigured, ConfigKeyKnownHosts, ConfigKeySftpInsecureIgnoreHostKey)
}

// HostKeyCallback returns the ssh.HostKeyCallback every SSH/SFTP connection in
// this program must use, according to the loaded configuration:
//
//   - paths.known-hosts set: the host key is checked against that known_hosts
//     file (knownhosts.New). A file that cannot be read or parsed is an error,
//     not a silent fallback to trusting the peer.
//   - sftp-insecure-ignore-hostkey: true and no known_hosts file: verification
//     is skipped, with a loud warning - this is the first-contact/testing
//     bypass and must be a deliberate operator decision.
//   - both set: the known_hosts file wins; the opt-in is a bypass, never an
//     override of real verification.
//   - neither set: an error naming both options. This is the fail-closed
//     default: an unconfigured policy never silently becomes "trust anything".
func HostKeyCallback() (ssh.HostKeyCallback, error) {
	if config.Config == nil {
		// An unloaded configuration has neither option set either: report the
		// same fail-closed error, noting why.
		return nil, fmt.Errorf("%w (no configuration is loaded)", hostKeyPolicyError())
	}
	return HostKeyCallbackFor(config.Config.Paths.KnownHostsPath, config.Config.SftpInsecureIgnoreHostKey)
}

// HostKeyCallbackFor implements the policy above for an explicit pair of
// settings, without consulting the global configuration. It exists so the
// policy can be exercised (and configured per call) without a loaded
// configuration; production code goes through HostKeyCallback.
func HostKeyCallbackFor(knownHostsPath string, insecureIgnoreHostKey bool) (ssh.HostKeyCallback, error) {
	// known_hosts is preferred whenever it is configured, even if the
	// insecure opt-in is set as well: an operator who has captured host keys
	// gets them verified.
	if knownHostsPath != "" {
		callback, err := knownhosts.New(knownHostsPath)
		if err != nil {
			return nil, fmt.Errorf(
				"ssh host key verification: cannot use known_hosts file %q: %w", knownHostsPath, err)
		}
		return callback, nil
	}

	if insecureIgnoreHostKey {
		log.Printf("WARNING: SSH HOST KEYS ARE NOT VERIFIED: '%s: true' is set and no '%s' file is configured, "+
			"so the host key of every SSH/SFTP peer is accepted without any check (this is the first-contact/testing bypass)",
			ConfigKeySftpInsecureIgnoreHostKey, ConfigKeyKnownHosts)
		return ssh.InsecureIgnoreHostKey(), nil
	}

	return nil, hostKeyPolicyError()
}
