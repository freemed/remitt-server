// Package sshsftp starts a real SSH server with a real SFTP subsystem on a
// loopback port, so a test can exercise the SFTP code paths of this repository
// against a real handshake, a real authentication and a real file transfer
// without a remote host.
//
// pkg/sftp ships a server as well as a client (sftp.NewServer), which is what
// makes this possible: the server's identity, its credentials and its
// filesystem all belong to the test. The host key is generated per Server, the
// credentials are the ones the Server reports, and the SFTP root is a temporary
// directory the harness owns (or the one WithRoot names). Nothing outside the
// loopback interface is ever contacted.
//
// Use it when a test has to observe the wire: host key verification, an
// authentication failure, an upload landing on the server, a download coming
// off it, or an error that is only produced by a real SSH/SFTP session. Tests
// that only pin configuration parsing, option mapping or fail-closed policy
// checks do not need it - a local listener or an injected fake session is
// cheaper and faster.
//
// Typical test:
//
//	srv := sshsftp.Start(t)
//	knownHosts, err := srv.WriteKnownHosts(t.TempDir())
//	if err != nil {
//		t.Fatal(err)
//	}
//	// Point paths.known-hosts at knownHosts (or give the code under test
//	// srv.SSHConfig(cb)) and configure the endpoint as srv.Host,
//	// srv.Port, srv.User, srv.Password, srv.Dir.
//
// A caller without a *testing.T (the cmd/sshsftp-harness tool, a manual
// verification) uses New and Close instead of Start and t.Cleanup.
package sshsftp

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Credentials the server accepts unless WithCredentials says otherwise. They
// are reported on Server, so a test never has to hardcode them.
const (
	DefaultUser     = "remitt"
	DefaultPassword = "remitt"
)

// Server is a running SSH server that serves an SFTP subsystem rooted at Dir.
//
// The exported fields are the connection details a test has to hand to the code
// under test: the address, the credentials and the SFTP root. The methods
// expose the host key, an uploaded-files view, a way to place files the client
// will download, and a known_hosts writer.
type Server struct {
	// Host is the loopback host the server listens on (always 127.0.0.1).
	Host string
	// Port is the TCP port the server listens on, chosen by the operating
	// system so parallel tests never collide.
	Port int
	// User and Password are the credentials the server accepts. Every other
	// user or password is rejected.
	User     string
	Password string
	// Dir is the SFTP root: what a client uploads lands under it, and what a
	// client downloads comes from it.
	Dir string

	signer   ssh.Signer
	listener net.Listener
	conns    atomic.Int64
	ownedDir bool
	closeOne sync.Once
}

// options carries what WithRoot and WithCredentials configure.
type options struct {
	user     string
	password string
	root     string
}

// Option configures New (and therefore Start).
type Option func(*options)

// WithCredentials changes the username and password the server accepts.
// WithCredentials is the way to make a test's own credentials the ones that
// work; the defaults are DefaultUser and DefaultPassword.
func WithCredentials(user, password string) Option {
	return func(o *options) {
		o.user = user
		o.password = password
	}
}

// WithRoot serves dir instead of a temporary directory. The directory is
// created if it does not exist, and it is left alone by Close: a caller that
// names a root owns it. It is what the cmd/sshsftp-harness tool uses to serve
// a directory a developer can look at after a delivery.
func WithRoot(dir string) Option {
	return func(o *options) { o.root = dir }
}

// Start starts a server on a loopback port, fails t if it cannot, and stops it
// when the test ends. It is the entry point for tests.
func Start(t testing.TB, opts ...Option) *Server {
	t.Helper()

	srv, err := New(opts...)
	if err != nil {
		t.Fatalf("sshsftp: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}

// New starts a server on a loopback port for a caller that has no *testing.T.
// The caller must Close it. Everything Start does except the failure and
// cleanup wiring is here.
func New(opts ...Option) (*Server, error) {
	cfg := options{user: DefaultUser, password: DefaultPassword}
	for _, opt := range opts {
		opt(&cfg)
	}

	dir := cfg.root
	ownedDir := false
	if dir == "" {
		temp, err := os.MkdirTemp("", "sshsftp-root-")
		if err != nil {
			return nil, fmt.Errorf("create sftp root: %w", err)
		}
		dir, ownedDir = temp, true
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create sftp root %q: %w", dir, err)
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		removeIf(ownedDir, dir)
		return nil, fmt.Errorf("generate host key: %w", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		removeIf(ownedDir, dir)
		return nil, fmt.Errorf("host key signer: %w", err)
	}

	user, password := cfg.user, cfg.password
	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if c.User() == user && string(pass) == password {
				return nil, nil
			}
			return nil, fmt.Errorf("sshsftp: access denied for %q", c.User())
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		removeIf(ownedDir, dir)
		return nil, fmt.Errorf("listen: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		removeIf(ownedDir, dir)
		return nil, fmt.Errorf("listener address %v is not a *net.TCPAddr", listener.Addr())
	}

	srv := &Server{
		Host:     "127.0.0.1",
		Port:     addr.Port,
		User:     user,
		Password: password,
		Dir:      dir,
		signer:   signer,
		listener: listener,
		ownedDir: ownedDir,
	}
	go srv.serve(serverConfig)

	return srv, nil
}

// removeIf removes dir when the harness created it.
func removeIf(owned bool, dir string) {
	if owned {
		_ = os.RemoveAll(dir)
	}
}

// Close stops the server. A root the harness created is removed; one named with
// WithRoot is left as it is. Close is safe to call more than once.
func (s *Server) Close() error {
	var err error
	s.closeOne.Do(func() {
		err = s.listener.Close()
		removeIf(s.ownedDir, s.Dir)
	})
	return err
}

// Address is the host:port form of the endpoint, as a transport or scooper
// would dial it.
func (s *Server) Address() string { return fmt.Sprintf("%s:%d", s.Host, s.Port) }

// HostKey is the public key the server presents as its host key.
func (s *Server) HostKey() ssh.PublicKey { return s.signer.PublicKey() }

// Fingerprint is the SHA256 fingerprint of HostKey, the string ssh(1) prints
// and an operator compares against.
func (s *Server) Fingerprint() string { return ssh.FingerprintSHA256(s.HostKey()) }

// KnownHostsLine is the entry ssh-keyscan(1) would print for this server: the
// endpoint followed by the host key. It is a line, not a file - see
// WriteKnownHosts.
func (s *Server) KnownHostsLine() string {
	return knownhosts.Line([]string{s.Address()}, s.HostKey())
}

// WriteKnownHosts writes KnownHostsLine to <dir>/known_hosts, creating dir if
// needed, and returns the file's path. Point host key verification at it
// (paths.known-hosts for this repository's transports and scoopers, or an
// ssh.HostKeyCallback from knownhosts.New) and the connection verifies against
// this exact server.
//
// The file deliberately lives in its own directory: dir is normally
// t.TempDir(), never Server.Dir, so the harness's control file can never be
// mistaken for a file the client uploaded or should download.
func (s *Server) WriteKnownHosts(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create known_hosts directory %q: %w", dir, err)
	}
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(s.KnownHostsLine()+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %q: %w", path, err)
	}
	return path, nil
}

// SSHConfig is the client configuration for this server: the harness's own
// credentials, the usual 10 second timeout, and hostKeyCallback exactly as
// given. Pass a callback from knownhosts.New over a file written by
// WriteKnownHosts to verify the real host key, or whatever callback the test is
// about (including a deliberately wrong one).
//
// It exists so tests do not each rebuild the same ssh.ClientConfig, and so the
// one seam that matters - the host key callback - is the one thing a caller
// supplies.
func (s *Server) SSHConfig(hostKeyCallback ssh.HostKeyCallback) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            s.User,
		Auth:            []ssh.AuthMethod{ssh.Password(s.Password)},
		Timeout:         10 * time.Second,
		HostKeyCallback: hostKeyCallback,
	}
}

// Uploaded reads a file the client uploaded. name is relative to the SFTP root
// and may contain subdirectories ("outbound/payment.x12"). It is an error if
// the file is not there, so a test says what it was looking for.
func (s *Server) Uploaded(name string) ([]byte, error) {
	content, err := os.ReadFile(s.localPath(name))
	if err != nil {
		return nil, fmt.Errorf("the client did not upload %q: %w", name, err)
	}
	return content, nil
}

// UploadedFiles is the uploaded-files view: every file under the SFTP root,
// keyed by its slash-separated path relative to the root. Use it when a test
// cares about what arrived rather than about one named file.
func (s *Server) UploadedFiles() (map[string][]byte, error) {
	uploaded := make(map[string][]byte)
	err := filepath.WalkDir(s.Dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(s.Dir, path)
		if err != nil {
			return err
		}
		uploaded[filepath.ToSlash(relative)] = content
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list uploaded files under %q: %w", s.Dir, err)
	}
	return uploaded, nil
}

// Put writes a file the client will download, creating any parent directories
// under the SFTP root. Name it exactly as the client will ask for it.
func (s *Server) Put(name string, content []byte) error {
	path := s.localPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create directory for %q: %w", name, err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", name, err)
	}
	return nil
}

// Connections is how many TCP connections the server has accepted. A test uses
// it to tell "the code under test refused to dial" from "the code under test
// dialled and the handshake failed": without it, both look like an error
// returned to the caller.
func (s *Server) Connections() int64 { return s.conns.Load() }

// localPath maps a client-visible name onto the SFTP root.
func (s *Server) localPath(name string) string {
	return filepath.Join(s.Dir, filepath.FromSlash(name))
}

// serve accepts connections until the listener is closed.
func (s *Server) serve(serverConfig *ssh.ServerConfig) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.conns.Add(1)
		go s.serveConn(conn, serverConfig)
	}
}

// serveConn completes the SSH handshake, then serves the session channels it
// carries. A connection that fails the handshake (a rejected password, a
// rejected host key on the client side) is simply closed.
func (s *Server) serveConn(conn net.Conn, serverConfig *ssh.ServerConfig) {
	sshConn, channels, requests, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		_ = conn.Close()
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "sshsftp: only session channels are served")
			continue
		}
		channel, channelRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go s.serveSession(channel, channelRequests)
	}
}

// serveSession answers "subsystem sftp" with pkg/sftp's own server, rooted at
// Dir, and refuses every other request. This is the whole SFTP subsystem: the
// real thing, not a stub.
func (s *Server) serveSession(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer channel.Close()

	for request := range requests {
		if request.Type == "subsystem" && len(request.Payload) >= 4 && string(request.Payload[4:]) == "sftp" {
			_ = request.Reply(true, nil)
			server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(s.Dir))
			if err != nil {
				return
			}
			_ = server.Serve()
			return
		}
		_ = request.Reply(false, nil)
	}
}
