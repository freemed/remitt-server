// Command sshsftp-harness starts the in-process SSH + SFTP test server from
// test/harness/sshsftp on a loopback port and prints everything a developer
// needs to point a real transport configuration at it.
//
// It exists so a delivery can be watched end to end without a remote SFTP host:
// start it, copy the printed options (or the ready-to-paste known_hosts line)
// into a remitt.yml / tUserConfig, run the transport, and watch the file appear
// in the SFTP root. The server is the same one the tests use - a real SSH
// handshake, a real SFTP subsystem, pkg/sftp's own server implementation.
//
//	go run ./cmd/sshsftp-harness/
//	go run ./cmd/sshsftp-harness/ -root ./test/harness-inbox -user bob -password secret
//
// Nothing outside the loopback interface is contacted. With -root the directory
// is left in place when the tool stops; otherwise the SFTP root is a temporary
// directory that is removed on exit.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/freemed/remitt-server/test/harness/sshsftp"
)

var (
	root     = flag.String("root", "", "directory to serve as the SFTP root (default: a temporary directory, removed on exit)")
	user     = flag.String("user", sshsftp.DefaultUser, "username the server accepts")
	password = flag.String("password", sshsftp.DefaultPassword, "password the server accepts")
	interval = flag.Duration("watch-interval", time.Second, "how often to report files delivered into the SFTP root")
)

func main() {
	flag.Parse()

	opts := []sshsftp.Option{sshsftp.WithCredentials(*user, *password)}
	if *root != "" {
		opts = append(opts, sshsftp.WithRoot(*root))
	}

	srv, err := sshsftp.New(opts...)
	if err != nil {
		log.Fatalf("sshsftp-harness: %v", err)
	}
	defer func() { _ = srv.Close() }()

	printDetails(srv)

	// Report deliveries as they land, so a developer can see the transport's
	// upload succeed without watching the directory in another terminal.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	reported := make(map[string]bool)
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for {
		select {
		case <-stop:
			fmt.Println("\nstopped")
			return
		case <-ticker.C:
			reportNewFiles(srv, reported)
		}
	}
}

// printDetails writes the connection details, the known_hosts line and the
// transport options for this run.
func printDetails(srv *sshsftp.Server) {
	fmt.Printf("SSH + SFTP server listening on %s\n", srv.Address())
	fmt.Printf("  host key fingerprint : %s\n", srv.Fingerprint())
	fmt.Printf("  username             : %s\n", srv.User)
	fmt.Printf("  password             : %s\n", srv.Password)
	fmt.Printf("  SFTP root            : %s\n", srv.Dir)
	fmt.Println()
	fmt.Println("known_hosts line for this server (append it to paths.known-hosts, or paste it into an existing known_hosts file):")
	fmt.Printf("  %s\n", srv.KnownHostsLine())
	fmt.Println()
	fmt.Println("transport options for this server:")
	fmt.Printf("  sftpHost=%s sftpPort=%d sftpUsername=%s sftpPassword=%s sftpPath=%s\n",
		srv.Host, srv.Port, srv.User, srv.Password, srv.Dir)
	fmt.Println()
	fmt.Printf("watching %s for delivered files (Ctrl-C to stop)...\n", srv.Dir)
}

// reportNewFiles lists files that appeared in the SFTP root since the last
// call, which is what a delivery looks like from the server's side.
func reportNewFiles(srv *sshsftp.Server, reported map[string]bool) {
	files, err := srv.UploadedFiles()
	if err != nil {
		fmt.Printf("  ! cannot list the SFTP root: %v\n", err)
		return
	}

	names := make([]string, 0, len(files))
	for name := range files {
		if !reported[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		reported[name] = true
		fmt.Printf("  delivered %s (%d bytes)\n", name, len(files[name]))
	}
	fmt.Printf("  [%s] %d connection(s) accepted, %d file(s) in the SFTP root\n",
		time.Now().Format("15:04:05"), srv.Connections(), len(files))
}
