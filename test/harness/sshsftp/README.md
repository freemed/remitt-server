# sshsftp — in-process SSH + SFTP server for tests

A real SSH server with a real SFTP subsystem, on a loopback port, owned by the
test that starts it. No remote host, no Docker, no fixture server to keep alive.

```go
import "github.com/freemed/remitt-server/test/harness/sshsftp"

srv := sshsftp.Start(t)                       // real handshake, real SFTP root

knownHosts, err := srv.WriteKnownHosts(t.TempDir())
if err != nil {
	t.Fatal(err)
}
// Configure the code under test with it:
//   paths.known-hosts -> knownHosts
//   sftpHost -> srv.Host, sftpPort -> srv.Port
//   sftpUsername -> srv.User, sftpPassword -> srv.Password
//   sftpPath -> srv.Dir

// ... run the transport / scooper ...
uploaded, err := srv.Uploaded("payload.x12")   // what the client actually sent
```

Watching a real delivery by hand:

```sh
go run ./cmd/sshsftp-harness/
go run ./cmd/sshsftp-harness/ -root ./test/harness-inbox -user bob -password secret
```

It prints the address, the host key fingerprint, the credentials, a
ready-to-paste `known_hosts` line and the `sftpHost/sftpPort/sftpUsername/
sftpPassword/sftpPath` options, then reports files as they land in the SFTP
root. Point a `remitt.yml` (or a `tUserConfig` row) at those options and the
delivery can be watched end to end.

## Why this exists

`pkg/sftp` ships a server as well as a client (`sftp.NewServer`), which means
the repository's SFTP paths do not need a remote host to be tested:

- The transport plugin `sftp` (`transport/sftp.go`), and `gatewayedi` /
  `claimlogic` which build the same `ssh.ClientConfig` shape.
- The scoopers `SftpScooper` and `GatewayEdiSftpScooper` (`scooper/sftp.go`),
  which download from a payer's server.
- Host key policy: `paths.known-hosts`, the `sftp-insecure-ignore-hostkey`
  opt-in, and the fail-closed path when neither is configured
  (`common.HostKeyCallback`).

Before this harness those paths were either skipped as "requires a live SFTP
server" or pinned with listeners that never spoke SSH — which is how a
`nil` `HostKeyCallback` (every dial aborts with `ssh: must specify
HostKeyCallback`, nothing is ever delivered) and an undecrypted remittance
(a `PostProcess` override that never ran) both survived in the tree. A real
handshake makes both failures visible from a unit test.

`Start` and `New` never contact anything outside the loopback interface, and the
only filesystem they touch is a temporary directory the harness creates (or the
one `WithRoot` names).

## When to use it (and when not to)

Use it when the assertion is about the wire:

- the handshake completes, or is refused (wrong password, mismatched host key);
- an upload really lands, or a download really comes back, byte for byte;
- a failure has to be attributed to connecting rather than to the transfer.

Do not use it when the assertion is about configuration: option parsing,
parameter mapping, the fail-closed policy check or an injected session seam
(`scooper/sftp_test.go`'s `sessionOpener`) are all cheaper to test with a
listener or a fake. Reaching for a real server there only makes the suite
slower and more fragile.

## API

| Item | What it is |
|---|---|
| `Start(t, opts...) *Server` | Starts the server, fails `t` if it cannot, stops it with `t.Cleanup`. Test entry point. |
| `New(opts...) (*Server, error)` | Same, for a caller with no `*testing.T` (the cmd tool). Close it yourself. |
| `WithRoot(dir)` | Serve `dir` instead of a temporary root. `Close` leaves a caller's root alone. |
| `WithCredentials(user, pass)` | Change the accepted credentials. Defaults are `DefaultUser` / `DefaultPassword`. |
| `(*Server).Host`, `.Port` | The endpoint: `sftpHost` and `sftpPort`. |
| `(*Server).User`, `.Password` | The credentials the server accepts. |
| `(*Server).Dir` | The SFTP root: uploads land here, downloads come from here. |
| `(*Server).Address()` | `host:port`, the form a dial takes. |
| `(*Server).HostKey()` | The `ssh.PublicKey` the server presents. |
| `(*Server).Fingerprint()` | `SHA256:…` fingerprint of `HostKey()`. |
| `(*Server).KnownHostsLine()` | The `ssh-keyscan`-shaped line for this endpoint. |
| `(*Server).WriteKnownHosts(dir)` | Writes that line to `<dir>/known_hosts` and returns the path. |
| `(*Server).SSHConfig(callback)` | An `*ssh.ClientConfig` for this server with the given host key callback. |
| `(*Server).Uploaded(name)` | Reads a file the client uploaded (error if it is not there). |
| `(*Server).UploadedFiles()` | Every file in the SFTP root, keyed by relative path. |
| `(*Server).Put(name, content)` | Places a file the client will download, creating parents. |
| `(*Server).Connections()` | TCP connections accepted — tells "refused to dial" from "dialled and failed". |
| `(*Server).Close()` | Stops the server, removes a root it created. Idempotent. |

`WriteKnownHosts` is deliberately given its own directory (`t.TempDir()`, not
`Server.Dir`): the root is what a client sees and lists, and a control file in
it would be scooped up as though the client had delivered it.

## Notes

- The server accepts password authentication only, one host key generated per
  `Server`, and refuses every request that is not `subsystem sftp`.
- Ports are chosen by the operating system, so tests run in parallel safely.
- `knownhosts.Line` brackets the endpoint when it carries a port
  (`[127.0.0.1]:41999`), which is the form a client's verification normalizes to
  — the same shape `ssh-keyscan 127.0.0.1 -p 41999` produces.
- Tests: `go test -count=1 ./test/harness/...`.
