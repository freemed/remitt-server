package transport

// selfconfig_test.go pins the self-configuration every transport plugin now
// performs for itself out of tUserConfig (selfconfig.go).
//
// Why these tests exist: nothing in production calls SetOptions - jobqueue
// passes a context (which carries the job's user) and nothing else
// (jobqueue/jobqueue.go:297,325) - so a transport plugin that only ever learned
// its endpoint from SetOptions could not deliver anything. The eligibility
// plugins already read their own configuration out of tUserConfig
// (eligibility/optum.go:170, eligibility/stedi.go:175); these tests pin the same
// behaviour for the transports, with no SetOptions call anywhere in the
// delivery tests.
//
// What is pinned, per plugin:
//   - the caller's own tUserConfig rows, under the Java FQCN the legacy
//     database stores, really configure the plugin and deliver a payload;
//   - the registered short name resolves as a namespace too, and the FQCN wins
//     when both forms carry the same option;
//   - another user's rows and another plugin's namespace are NOT applied
//     (and the lookup is issued for the caller's username);
//   - a stored value that cannot be read as an int is reported, naming the
//     option, instead of being dropped;
//   - explicitly-set options win over stored ones, per option;
//   - a process with no database does not panic and keeps working exactly as
//     it did before self-configuration existed;
//   - the namespaces each plugin matches on are the ones the registry actually
//     registers.
//
// The tiny database stub (withStubQueries) and the in-process SSH/SFTP server
// (startSftpTestServer) come from storefile_test.go / sftp_test.go, so this file
// adds no new test infrastructure.

import (
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/freemed/remitt-server/model"
)

// configRowColumns matches the SELECT in internal/dbgen/config.sql.go
// (getConfigValues): "SELECT user, cnamespace, coption, cvalue FROM tUserConfig
// WHERE user = ?".
var configRowColumns = []string{"user", "cnamespace", "coption", "cvalue"}

// configRow builds one tUserConfig row as GetConfigValues selects it.
func configRow(user, namespace, option, value string) []driver.Value {
	return []driver.Value{user, namespace, option, value}
}

// selfConfigPlugin describes the SFTP-family transports that self-configure:
// the option key names each one reads, and the file it writes on the server.
type selfConfigPlugin struct {
	short     string // registered short name, which is also a namespace form
	javaClass string // Java class name, stored as the FQCN namespace
	host      string
	port      string
	username  string
	password  string
	path      string
	uploaded  string // the name the plugin writes on the remote server

	// notConfigured is what the plugin reports when it has no configuration at
	// all: sftp and gatewayedi validate their settings, claimlogic dials the
	// empty endpoint and lets the failure speak.
	notConfigured string
}

func selfConfigPlugins() []selfConfigPlugin {
	return []selfConfigPlugin{
		{
			short: "sftp", javaClass: "SftpTransport",
			host: "sftpHost", port: "sftpPort", username: "sftpUsername",
			password: "sftpPassword", path: "sftpPath",
			uploaded: "payload.x12",
			notConfigured: "sftp: missing host, port, or username",
		},
		{
			short: "gatewayedi", javaClass: "GatewayEdiTransport",
			host: "gatewayEdiHost", port: "gatewayEdiPort", username: "gatewayEdiUsername",
			password: "gatewayEdiPassword", path: "gatewayEdiPath",
			uploaded: "payload.x12.zip",
			notConfigured: "gatewayedi: missing host, port, or username",
		},
		{
			short: "claimlogic", javaClass: "ClaimLogicTransport",
			host: "claimlogicHost", port: "claimlogicPort", username: "claimlogicUsername",
			password: "claimlogicPassword", path: "claimlogicPath",
			uploaded: "payload.x12.zip",
			notConfigured: ":0",
		},
	}
}

// fqcn is the namespace the legacy database and the UI store for this plugin.
func (p selfConfigPlugin) fqcn() string { return JavaPluginPrefix + p.javaClass }

// serverRows returns the rows a user would have in tUserConfig after
// configuring this plugin to deliver to srv through the given namespace.
func (p selfConfigPlugin) serverRows(user, namespace string, srv *sftpTestServer) [][]driver.Value {
	return [][]driver.Value{
		configRow(user, namespace, p.host, srv.host),
		configRow(user, namespace, p.port, strconv.Itoa(srv.port)),
		configRow(user, namespace, p.username, testSftpUser),
		configRow(user, namespace, p.password, testSftpPass),
		configRow(user, namespace, p.path, srv.dir),
	}
}

// instantiate builds a fresh plugin under the name the jobqueue would carry.
func (p selfConfigPlugin) instantiate(t *testing.T) Transporter {
	t.Helper()
	m, err := InstantiateTransporter(p.short)
	if err != nil {
		t.Fatalf("InstantiateTransporter(%q) = %v; want a plugin", p.short, err)
	}
	return m
}

// ---------------------------------------------------------------------------
// Delivery straight out of the database, with no SetOptions anywhere
// ---------------------------------------------------------------------------

// TestSelfConfig_TransportDeliversFromUserConfig is the point of the whole
// change: a user's configuration in tUserConfig is enough for the plugin to
// deliver a payload. Neither the context nor the options are set by hand
// beyond SetContext, which is exactly what jobqueue does.
func TestSelfConfig_TransportDeliversFromUserConfig(t *testing.T) {
	payload := []byte("ISA*00*          *00*          *ZZ*PAYER          *ZZ*SUBMITTER      *240101*1200*^*00501*000000001*0*P*:~")

	for _, p := range selfConfigPlugins() {
		t.Run(p.short, func(t *testing.T) {
			srv := startSftpTestServer(t)
			withHostKeyPolicy(t, "", true) // the payload path is what is under test, not the host key policy
			withStubQueries(t, nil, configRowColumns, p.serverRows("alice", p.fqcn(), srv))

			m := p.instantiate(t)
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			// Deliberately no SetOptions: nothing in production calls it.

			if err := m.Transport("payload.x12", payload); err != nil {
				t.Fatalf("Transport() with the user's tUserConfig rows and no SetOptions = %v; want the delivery to succeed", err)
			}
			if got := srv.acceptedConnections(); got == 0 {
				t.Error("the SSH server accepted no connection; the endpoint came from nowhere")
			}
			if _, err := os.Stat(filepath.Join(srv.dir, p.uploaded)); err != nil {
				t.Errorf("the server did not receive %q from the database-configured endpoint: %v", p.uploaded, err)
			}
		})
	}
}

// TestSelfConfig_NoStoredConfigurationLeavesThePluginUnconfigured is the
// control for the test above: the very same call with no rows for the user must
// NOT deliver anything. Without this, a plugin that ignored tUserConfig and
// dialled something hardcoded would pass the delivery test.
func TestSelfConfig_NoStoredConfigurationLeavesThePluginUnconfigured(t *testing.T) {
	for _, p := range selfConfigPlugins() {
		t.Run(p.short, func(t *testing.T) {
			withHostKeyPolicy(t, "", true)
			withStubQueries(t, nil, configRowColumns, nil) // a database with no rows for this user

			m := p.instantiate(t)
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			err := m.Transport("payload.x12", []byte("ISA*00*"))
			if err == nil {
				t.Fatal("Transport() with no stored configuration and no SetOptions returned a nil error; want the plugin to report its configuration gap")
			}
			if !strings.Contains(err.Error(), p.notConfigured) {
				t.Errorf("Transport() error = %q; want it to contain %q", err.Error(), p.notConfigured)
			}
		})
	}
}

// TestSelfConfig_ScriptRunsFromUserConfig pins the same behaviour for the
// script transport, whose options (script/timeout) are not an endpoint.
func TestSelfConfig_ScriptRunsFromUserConfig(t *testing.T) {
	withStubQueries(t, nil, configRowColumns, [][]driver.Value{
		configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "script", "log('running from tUserConfig');"),
		configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "timeout", "5"),
	})

	m, err := InstantiateTransporter("script")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	// Deliberately no SetOptions.
	if err := m.Transport("payload.x12", []byte("ISA*00*")); err != nil {
		t.Fatalf("Transport() with the user's stored script and no SetOptions = %v; want the script to run", err)
	}
}

// TestSelfConfig_ScriptWithoutStoredScriptIsUnconfigured is the control for the
// test above.
func TestSelfConfig_ScriptWithoutStoredScriptIsUnconfigured(t *testing.T) {
	withStubQueries(t, nil, configRowColumns, nil)

	m, err := InstantiateTransporter("script")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	err = m.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with no stored script returned a nil error; want the missing-script error")
	}
	if !strings.Contains(err.Error(), "script: no script option given") {
		t.Errorf("Transport() error = %q; want it to report the missing script", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Namespace matching
// ---------------------------------------------------------------------------

// TestSelfConfig_ShortNameNamespaceAlsoResolves pins the second namespace form:
// rows stored under the registered short name ("sftp") configure the plugin
// exactly like the FQCN ones do. The registry carries both names for the same
// plugin (map.go), so the configuration must resolve under both.
func TestSelfConfig_ShortNameNamespaceAlsoResolves(t *testing.T) {
	payload := []byte("ISA*00*")

	for _, p := range selfConfigPlugins() {
		t.Run(p.short, func(t *testing.T) {
			srv := startSftpTestServer(t)
			withHostKeyPolicy(t, "", true)
			withStubQueries(t, nil, configRowColumns, p.serverRows("alice", p.short, srv))

			m := p.instantiate(t)
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			if err := m.Transport("payload.x12", payload); err != nil {
				t.Fatalf("Transport() with rows under the short-name namespace %q = %v; want the delivery to succeed", p.short, err)
			}
			if _, err := os.Stat(filepath.Join(srv.dir, p.uploaded)); err != nil {
				t.Errorf("the server did not receive %q from the short-name-namespace configuration: %v", p.uploaded, err)
			}
		})
	}
}

// TestSelfConfig_FQCNNamespaceWinsOverShortName pins the documented precedence
// between the two namespace forms for the SAME option: the Java FQCN is the
// form the legacy database actually stores, so when both forms carry the
// option, the FQCN's value is the one used. The FQCN port is the real server's;
// the short-name port is a closed one, so the upload can only succeed if the
// FQCN row won.
func TestSelfConfig_FQCNNamespaceWinsOverShortName(t *testing.T) {
	_, closedPort, _ := localSSHBait(t)
	payload := []byte("ISA*00*")

	for _, p := range selfConfigPlugins() {
		t.Run(p.short, func(t *testing.T) {
			srv := startSftpTestServer(t)
			withHostKeyPolicy(t, "", true)

			rows := p.serverRows("alice", p.fqcn(), srv)
			rows = append(rows,
				// Same option, other namespace form: a port nothing listens on.
				configRow("alice", p.short, p.port, strconv.Itoa(closedPort)),
				configRow("alice", p.short, p.host, srv.host),
				configRow("alice", p.short, p.path, srv.dir),
			)
			withStubQueries(t, nil, configRowColumns, rows)

			m := p.instantiate(t)
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			if err := m.Transport("payload.x12", payload); err != nil {
				t.Fatalf("Transport() with the same option under both namespace forms = %v; want the FQCN (%s) to win", err, p.fqcn())
			}
			if _, err := os.Stat(filepath.Join(srv.dir, p.uploaded)); err != nil {
				t.Errorf("the server did not receive %q; the short-name row overrode the FQCN row: %v", p.uploaded, err)
			}
		})
	}
}

// TestSelfConfig_NamespacesMatchTheRegistry pins that the namespaces each
// plugin matches on are the ones the plugin registry actually registers: the
// FQCN first (what the legacy database stores), the short name second, and both
// resolving to the same plugin type. Without this, a plugin could quietly match
// a namespace nothing writes to and never be configured from the database.
func TestSelfConfig_NamespacesMatchTheRegistry(t *testing.T) {
	type entry struct {
		short     string
		javaClass string
		proto     userConfigurableTransporter
	}
	entries := []entry{
		{"sftp", "SftpTransport", &Sftp{}},
		{"gatewayedi", "GatewayEdiTransport", &GatewayEdi{}},
		{"claimlogic", "ClaimLogicTransport", &ClaimLogic{}},
		{"script", "ScriptedHttpTransport", &Script{}},
	}

	for _, e := range entries {
		t.Run(e.short, func(t *testing.T) {
			namespaces := e.proto.userConfigNamespaces()
			if len(namespaces) != 2 {
				t.Fatalf("userConfigNamespaces() = %v; want exactly the FQCN and the short name", namespaces)
			}
			if want := JavaPluginPrefix + e.javaClass; namespaces[0] != want {
				t.Errorf("userConfigNamespaces()[0] = %q; want the Java FQCN %q (the form the legacy database stores)", namespaces[0], want)
			}
			if namespaces[1] != e.short {
				t.Errorf("userConfigNamespaces()[1] = %q; want the registered short name %q", namespaces[1], e.short)
			}

			short, err := InstantiateTransporter(e.short)
			if err != nil {
				t.Fatalf("InstantiateTransporter(%q) = %v; want the short name to be registered", e.short, err)
			}
			java, err := InstantiateTransporter(namespaces[0])
			if err != nil {
				t.Fatalf("InstantiateTransporter(%q) = %v; want the FQCN namespace to be registered", namespaces[0], err)
			}
			if fmt.Sprintf("%T", short) != fmt.Sprintf("%T", java) {
				t.Errorf("the FQCN %q resolves to %T while %q resolves to %T; both names must be the same plugin", namespaces[0], java, e.short, short)
			}
			if got := fmt.Sprintf("%T", e.proto); got != fmt.Sprintf("%T", short) {
				t.Errorf("userConfigNamespaces() is declared by %s, which is not what %q instantiates (%s)", got, e.short, fmt.Sprintf("%T", short))
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Rows that must NOT be applied
// ---------------------------------------------------------------------------

// TestSelfConfig_IgnoresAnotherUsersRows pins that the configuration is looked
// up for the caller (the user in the context) and not for anybody else: when
// only another user's rows describe a working endpoint, the plugin stays
// unconfigured and nothing reaches that endpoint.
func TestSelfConfig_IgnoresAnotherUsersRows(t *testing.T) {
	for _, p := range selfConfigPlugins() {
		t.Run(p.short, func(t *testing.T) {
			srv := startSftpTestServer(t)
			withHostKeyPolicy(t, "", true)

			// A complete, working configuration - but it belongs to mallory.
			db := withStubQueries(t, nil, configRowColumns, p.serverRows("mallory", p.fqcn(), srv))

			m := p.instantiate(t)
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			err := m.Transport("payload.x12", []byte("ISA*00*"))
			if err == nil {
				t.Fatal("Transport() delivered using another user's configuration; want it to stay unconfigured")
			}
			if !strings.Contains(err.Error(), p.notConfigured) {
				t.Errorf("Transport() error = %q; want it to contain %q", err.Error(), p.notConfigured)
			}
			if got := srv.acceptedConnections(); got != 0 {
				t.Errorf("the endpoint named in another user's rows accepted %d connection(s); want 0", got)
			}

			// And the lookup itself was issued for the caller.
			calls := db.queryCalls()
			if len(calls) != 1 {
				t.Fatalf("recorded %d queries; want exactly 1 (the tUserConfig lookup)", len(calls))
			}
			if len(calls[0].Args) != 1 {
				t.Fatalf("the tUserConfig lookup was issued with %d arguments; want 1 (the username)", len(calls[0].Args))
			}
			if got, want := calls[0].Args[0], driver.Value("alice"); got != want {
				t.Errorf("the tUserConfig lookup was issued for %#v; want the caller %#v (from user.FromContext)", got, want)
			}
		})
	}
}

// TestSelfConfig_IgnoresAnotherPluginsRows pins the namespace boundary: a
// complete configuration stored for a DIFFERENT plugin - and an option under a
// namespace nothing registers - is not applied to this one.
func TestSelfConfig_IgnoresAnotherPluginsRows(t *testing.T) {
	for _, p := range selfConfigPlugins() {
		t.Run(p.short, func(t *testing.T) {
			srv := startSftpTestServer(t)
			withHostKeyPolicy(t, "", true)

			other := selfConfigPlugins()
			rows := [][]driver.Value{}
			for _, o := range other {
				if o.short == p.short {
					continue
				}
				rows = append(rows, o.serverRows("alice", o.fqcn(), srv)...)
				rows = append(rows, o.serverRows("alice", o.short, srv)...)
			}
			// Same option names, namespace nothing registers.
			for _, option := range []string{p.host, p.username, p.password, p.path} {
				rows = append(rows, configRow("alice", "org.remitt.plugin.transport.NotAPlugin", option, "elsewhere.example.invalid"))
			}
			db := withStubQueries(t, nil, configRowColumns, rows)

			m := p.instantiate(t)
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			err := m.Transport("payload.x12", []byte("ISA*00*"))
			if err == nil {
				t.Fatal("Transport() used another plugin's rows; want it to stay unconfigured")
			}
			if !strings.Contains(err.Error(), p.notConfigured) {
				t.Errorf("Transport() error = %q; want it to contain %q", err.Error(), p.notConfigured)
			}
			if got := srv.acceptedConnections(); got != 0 {
				t.Errorf("the endpoint named in another plugin's rows accepted %d connection(s); want 0", got)
			}

			// The user's rows WERE consulted - they were read and rejected, not
			// never looked at.
			calls := db.queryCalls()
			if len(calls) != 1 || len(calls[0].Args) != 1 || calls[0].Args[0] != driver.Value("alice") {
				t.Errorf("configuration lookups = %#v; want exactly one, issued for the context user %q", calls, "alice")
			}
		})
	}
}

// TestSelfConfig_IgnoresForeignOptionsInItsOwnNamespace pins the leftover keys
// the legacy seed stores in the Script transport's namespace: all of
// 'org.remitt.plugin.transport.ScriptedHttpTransport' carries username/password
// (migrations/001_legacy.up.sql:68-69), which this plugin does not declare.
// They must be ignored - not applied, and not an error - so the plugin reports
// the missing script it actually needs.
func TestSelfConfig_IgnoresForeignOptionsInItsOwnNamespace(t *testing.T) {
	t.Run("the foreign keys alone do not configure it", func(t *testing.T) {
		withStubQueries(t, nil, configRowColumns, [][]driver.Value{
			configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "username", "user"),
			configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "password", "pass"),
		})

		m, err := InstantiateTransporter("script")
		if err != nil {
			t.Fatal(err)
		}
		if err := m.SetContext(ctxWithUser("alice")); err != nil {
			t.Fatal(err)
		}
		err = m.Transport("payload.x12", []byte("ISA*00*"))
		if err == nil {
			t.Fatal("Transport() returned nil; want the missing-script error")
		}
		if !strings.Contains(err.Error(), "script: no script option given") {
			t.Errorf("Transport() error = %q; want the plugin to ignore username/password and report the missing script", err.Error())
		}
	})

	t.Run("the declared options still apply alongside them", func(t *testing.T) {
		withStubQueries(t, nil, configRowColumns, [][]driver.Value{
			configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "username", "user"),
			configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "password", "pass"),
			configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "script", "log('alongside foreign keys');"),
			configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "timeout", "5"),
		})

		m, err := InstantiateTransporter("script")
		if err != nil {
			t.Fatal(err)
		}
		if err := m.SetContext(ctxWithUser("alice")); err != nil {
			t.Fatal(err)
		}
		if err := m.Transport("payload.x12", []byte("ISA*00*")); err != nil {
			t.Fatalf("Transport() = %v; want the declared script/timeout to be applied even though the namespace also holds username/password", err)
		}
	})
}

// TestSelfConfig_SeedRowsParseRatherThanFail pins that the rows
// migrations/001_legacy.up.sql:63-67 really seeds for Administrator are
// readable: the empty strings stay empty strings and the text port parses as an
// int. A user with only the seed rows must still be told the endpoint is
// missing (host and username are empty), not that their configuration could not
// be read.
func TestSelfConfig_SeedRowsParseRatherThanFail(t *testing.T) {
	db := withStubQueries(t, nil, configRowColumns, [][]driver.Value{
		configRow("Administrator", JavaPluginPrefix+"SftpTransport", "sftpHost", ""),
		configRow("Administrator", JavaPluginPrefix+"SftpTransport", "sftpPath", ""),
		configRow("Administrator", JavaPluginPrefix+"SftpTransport", "sftpPort", "22"),
		configRow("Administrator", JavaPluginPrefix+"SftpTransport", "sftpUsername", ""),
		configRow("Administrator", JavaPluginPrefix+"SftpTransport", "sftpPassword", ""),
	})

	m, err := InstantiateTransporter("sftp")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetContext(ctxWithUser("Administrator")); err != nil {
		t.Fatal(err)
	}
	err = m.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with the seed rows returned a nil error; want the missing-endpoint error")
	}
	if !strings.Contains(err.Error(), "sftp: missing host, port, or username") {
		t.Errorf("Transport() error = %q; want the seed values read (port 22 from text) and the empty endpoint reported", err.Error())
	}
	if strings.Contains(err.Error(), "not an integer") {
		t.Errorf("Transport() error = %q; the seeded text value '22' for sftpPort must be read, not rejected", err.Error())
	}

	// The seed rows were really read: a plugin that never looked them up would
	// report the same missing endpoint for the wrong reason.
	calls := db.queryCalls()
	if len(calls) != 1 || len(calls[0].Args) != 1 || calls[0].Args[0] != driver.Value("Administrator") {
		t.Errorf("configuration lookups = %#v; want exactly one, issued for the context user %q", calls, "Administrator")
	}
}

// ---------------------------------------------------------------------------
// Wrong-typed stored options
// ---------------------------------------------------------------------------

// TestSelfConfig_ReportsWrongTypedStoredOption pins that a stored value which
// cannot be read as the option's type is reported, naming the option, rather
// than being dropped silently: a stored cookie for a port must never leave the
// plugin looking merely unconfigured, with an error that never mentions the
// option at fault.
func TestSelfConfig_ReportsWrongTypedStoredOption(t *testing.T) {
	tests := []struct {
		plugin string
		fqcn   string
		// store builds the rows holding one wrong-typed value, alongside valid
		// values for everything else, so the only thing that can fail is the
		// option under test.
		store    func(namespace string) [][]driver.Value
		wantName string
	}{
		{
			plugin: "sftp", fqcn: JavaPluginPrefix + "SftpTransport", wantName: "sftpPort",
			store: func(ns string) [][]driver.Value {
				return [][]driver.Value{
					configRow("alice", ns, "sftpHost", "127.0.0.1"),
					configRow("alice", ns, "sftpUsername", testSftpUser),
					configRow("alice", ns, "sftpPassword", testSftpPass),
					configRow("alice", ns, "sftpPath", "/outbound"),
					configRow("alice", ns, "sftpPort", "not-a-port"),
				}
			},
		},
		{
			plugin: "gatewayedi", fqcn: JavaPluginPrefix + "GatewayEdiTransport", wantName: "gatewayEdiPort",
			store: func(ns string) [][]driver.Value {
				return [][]driver.Value{
					configRow("alice", ns, "gatewayEdiHost", "127.0.0.1"),
					configRow("alice", ns, "gatewayEdiUsername", testSftpUser),
					configRow("alice", ns, "gatewayEdiPassword", testSftpPass),
					configRow("alice", ns, "gatewayEdiPath", "/outbound"),
					configRow("alice", ns, "gatewayEdiPort", "twenty-two"),
				}
			},
		},
		{
			// The legacy seed stores '22' as TEXT and it is read; an EMPTY
			// stored port is a value that cannot be read as an int, and is
			// reported by name rather than left looking like "20-unset".
			plugin: "claimlogic", fqcn: JavaPluginPrefix + "ClaimLogicTransport", wantName: "claimlogicPort",
			store: func(ns string) [][]driver.Value {
				return [][]driver.Value{
					configRow("alice", ns, "claimlogicHost", "127.0.0.1"),
					configRow("alice", ns, "claimlogicUsername", testSftpUser),
					configRow("alice", ns, "claimlogicPassword", testSftpPass),
					configRow("alice", ns, "claimlogicPath", "/outbound"),
					configRow("alice", ns, "claimlogicPort", ""),
				}
			},
		},
		{
			plugin: "script", fqcn: JavaPluginPrefix + "ScriptedHttpTransport", wantName: "timeout",
			store: func(ns string) [][]driver.Value {
				return [][]driver.Value{
					configRow("alice", ns, "script", "log('x');"),
					configRow("alice", ns, "timeout", "soon"),
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.plugin, func(t *testing.T) {
			_, _, accepted := localSSHBait(t)
			withHostKeyPolicy(t, "", true)
			withStubQueries(t, nil, configRowColumns, tt.store(tt.fqcn))

			m, err := InstantiateTransporter(tt.plugin)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			err = m.Transport("payload.x12", []byte("ISA*00*"))
			if err == nil {
				t.Fatalf("Transport() with a wrong-typed stored %s returned a nil error; want the value reported", tt.wantName)
			}
			if !strings.Contains(err.Error(), tt.wantName) {
				t.Errorf("Transport() error = %q; want it to name the option %q", err.Error(), tt.wantName)
			}
			if !strings.Contains(err.Error(), "not an integer") {
				t.Errorf("Transport() error = %q; want it to say the stored value is not an integer", err.Error())
			}
			if !strings.HasPrefix(err.Error(), tt.plugin+":") {
				t.Errorf("Transport() error = %q; want it tagged with the %s plugin prefix", err.Error(), tt.plugin)
			}

			// The reader learned the configuration was bad, before any dial.
			awaitNoConnection(t, accepted, 300*time.Millisecond)
		})
	}
}

// TestSelfConfig_QueryFailureIsReported pins that a database which is present
// but unusable is a configuration error: the plugin cannot know how it was
// configured, so it must say so instead of delivering to whatever SetOptions
// happened to hold or dialling an empty endpoint.
func TestSelfConfig_QueryFailureIsReported(t *testing.T) {
	withHostKeyPolicy(t, "", true)
	withStubQueries(t, &stubDB{queryErr: errStubQuery}, configRowColumns, nil)

	m, err := InstantiateTransporter("sftp")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	err = m.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with a failing configuration lookup returned a nil error; want the failure reported")
	}
	if !strings.Contains(err.Error(), "alice") || !strings.Contains(err.Error(), errStubQuery.Error()) {
		t.Errorf("Transport() error = %q; want it to name the user and wrap %q", err.Error(), errStubQuery.Error())
	}
}

// ---------------------------------------------------------------------------
// Precedence: explicitly-set options beat stored ones, per option
// ---------------------------------------------------------------------------

// TestSelfConfig_ExplicitOptionsWinOverStoredOnes pins the precedence rule: an
// option the caller set through SetOptions always wins over the value stored for
// the user, and the database supplies the options the caller did not set. The
// stored rows point at the in-process server with one destination directory; the
// explicit option names another one, so the uploaded file says which won - while
// the host, port and credentials can only have come from the database.
func TestSelfConfig_ExplicitOptionsWinOverStoredOnes(t *testing.T) {
	payload := []byte("ISA*00*")

	for _, p := range selfConfigPlugins() {
		t.Run(p.short, func(t *testing.T) {
			srv := startSftpTestServer(t)
			withHostKeyPolicy(t, "", true)

			storedDir := t.TempDir()
			rows := [][]driver.Value{
				configRow("alice", p.fqcn(), p.host, srv.host),
				configRow("alice", p.fqcn(), p.port, strconv.Itoa(srv.port)),
				configRow("alice", p.fqcn(), p.username, testSftpUser),
				configRow("alice", p.fqcn(), p.password, testSftpPass),
				configRow("alice", p.fqcn(), p.path, storedDir),
			}
			withStubQueries(t, nil, configRowColumns, rows)

			explicitDir := t.TempDir()
			m := p.instantiate(t)
			if err := m.SetContext(ctxWithUser("alice")); err != nil {
				t.Fatal(err)
			}
			if err := m.SetOptions(map[string]any{p.path: explicitDir}); err != nil {
				t.Fatalf("SetOptions(%s) = %v; want nil", p.path, err)
			}
			if err := m.Transport("payload.x12", payload); err != nil {
				t.Fatalf("Transport() = %v; want the delivery to succeed (the database supplies host/port/credentials, the caller supplies the path)", err)
			}

			if _, err := os.Stat(filepath.Join(explicitDir, p.uploaded)); err != nil {
				t.Errorf("the payload did not land in the explicitly configured %s (%s): %v", p.path, explicitDir, err)
			}
			if _, err := os.Stat(filepath.Join(storedDir, p.uploaded)); err == nil {
				t.Errorf("the payload landed in the stored %s (%s); an explicitly set option must win", p.path, storedDir)
			}
		})
	}
}

// TestSelfConfig_ExplicitScriptWinsOverStoredScript pins the same precedence
// for the script transport, where the observable is whether the script that runs
// is valid: the stored script is not, the explicitly set one is, and the call
// succeeds only if the explicit one won.
func TestSelfConfig_ExplicitScriptWinsOverStoredScript(t *testing.T) {
	withStubQueries(t, nil, configRowColumns, [][]driver.Value{
		configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "script", "this is (not valid javascript"),
		configRow("alice", JavaPluginPrefix+"ScriptedHttpTransport", "timeout", "5"),
	})

	m, err := InstantiateTransporter("script")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	err = m.Transport("payload.x12", []byte("ISA*00*"))
	if err == nil {
		t.Fatal("Transport() with only the stored, invalid script returned nil; the stored script must be what runs when nothing is set explicitly")
	}
	if strings.Contains(err.Error(), "no script option given") {
		t.Fatalf("Transport() error = %q; the stored script was never applied", err.Error())
	}

	m2, err := InstantiateTransporter("script")
	if err != nil {
		t.Fatal(err)
	}
	if err := m2.SetContext(ctxWithUser("alice")); err != nil {
		t.Fatal(err)
	}
	if err := m2.SetOptions(map[string]any{"script": "log('explicit');", "timeout": 5}); err != nil {
		t.Fatal(err)
	}
	if err := m2.Transport("payload.x12", []byte("ISA*00*")); err != nil {
		t.Fatalf("Transport() = %v; want the explicitly set script to win over the stored one", err)
	}
}

// ---------------------------------------------------------------------------
// No database
// ---------------------------------------------------------------------------

// TestSelfConfig_NilDatabaseDoesNotPanic pins the degraded mode: a process that
// never initialised its database must not panic and must not behave worse than
// it did before self-configuration existed. There is simply no stored
// configuration, so the plugin reports its own configuration gap - and explicit
// options, the path direct callers and tests use, keep working untouched.
func TestSelfConfig_NilDatabaseDoesNotPanic(t *testing.T) {
	prevQueries, prevDb := model.Queries, model.SqlDb
	model.Queries, model.SqlDb = nil, nil
	t.Cleanup(func() { model.Queries, model.SqlDb = prevQueries, prevDb })

	t.Run("unconfigured plugins report their own gap", func(t *testing.T) {
		withHostKeyPolicy(t, "", true)
		for _, tt := range []struct{ name, want string }{
			{"sftp", "sftp: missing host, port, or username"},
			{"gatewayedi", "gatewayedi: missing host, port, or username"},
			{"claimlogic", ":0"},
			{"script", "script: no script option given"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m, err := InstantiateTransporter(tt.name)
				if err != nil {
					t.Fatal(err)
				}
				if err := m.SetContext(ctxWithUser("alice")); err != nil {
					t.Fatal(err)
				}
				// A panic here fails the test: the plugin must return, not unwind.
				err = m.Transport("payload.x12", []byte("ISA*00*"))
				if err == nil {
					t.Fatal("Transport() with model.Queries == nil and no options returned a nil error; want a configuration error")
				}
				if !strings.Contains(err.Error(), tt.want) {
					t.Errorf("Transport() error = %q; want it to contain %q", err.Error(), tt.want)
				}
			})
		}
	})

	t.Run("explicit options still deliver without a database", func(t *testing.T) {
		for _, p := range selfConfigPlugins() {
			t.Run(p.short, func(t *testing.T) {
				srv := startSftpTestServer(t)
				withHostKeyPolicy(t, "", true)

				m := p.instantiate(t)
				if err := m.SetContext(ctxWithUser("alice")); err != nil {
					t.Fatal(err)
				}
				if err := m.SetOptions(map[string]any{
					p.host:     srv.host,
					p.port:     srv.port,
					p.username: testSftpUser,
					p.password: testSftpPass,
					p.path:     srv.dir,
				}); err != nil {
					t.Fatalf("SetOptions() = %v; want nil", err)
				}
				if err := m.Transport("payload.x12", []byte("ISA*00*")); err != nil {
					t.Fatalf("Transport() with full explicit options and no database = %v; want the delivery to succeed", err)
				}
				if _, err := os.Stat(filepath.Join(srv.dir, p.uploaded)); err != nil {
					t.Errorf("the server did not receive %q: %v", p.uploaded, err)
				}
			})
		}
	})
}
