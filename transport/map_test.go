package transport

// map_test.go pins the plugin REGISTRY contract (map.go) and the Transporter
// interface contract (interface.go).
//
// What this file pins:
//   - the exact set of names registered by the six transport plugins: each
//     plugin registers its short name AND the Java FQCN the legacy database
//     stores (e.g. "sftp" and
//     "org.remitt.plugin.transport.SftpTransport")
//   - every registered name resolves to a non-nil Transporter of the expected
//     concrete type (registry keys are SHORT names plus FQCN aliases)
//   - an unknown name fails with an error AND a nil plugin (never a nil
//     plugin with a nil error)
//   - every plugin implements Transporter, has a stable InputFormat, a
//     non-nil Options() list and tolerates SetOptions/SetContext
//   - every plugin fails with an ERROR (never a panic, never a nil error)
//     when handed empty configuration / no user in context
//
// RegisterTransporter/InstantiateTransporter carry the Java plugin name in
// migrations/001_legacy.up.sql as an FQCN
// (e.g. "org.remitt.plugin.transport.SftpTransport"); jobqueue passes the
// stored value straight to InstantiateTransporter, so the registry contains
// both the short names and those FQCNs, which
// TestRegistry_JavaFQCNNamesResolveToTheSamePlugins pins.

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
)

// Compile-time assertion of the interface.go contract: every plugin type
// implemented in this package satisfies Transporter.
var (
	_ Transporter = (*Script)(nil)
	_ Transporter = (*Sftp)(nil)
	_ Transporter = (*ClaimLogic)(nil)
	_ Transporter = (*GatewayEdi)(nil)
	_ Transporter = (*StoreFile)(nil)
	_ Transporter = (*StoreFilePdf)(nil)
)

// registeredNames returns a sorted snapshot of the registry keys. The registry
// and its mutex are package-private, so this is the only way to enumerate them.
func registeredNames() []string {
	transporterRegistryLock.Lock()
	names := make([]string, 0, len(transporterRegistry))
	for k := range transporterRegistry {
		names = append(names, k)
	}
	transporterRegistryLock.Unlock()
	sort.Strings(names)
	return names
}

func TestRegistry_ContainsExactlyTheExpectedPluginNames(t *testing.T) {
	// Six short names plus the six Java FQCN aliases the legacy database and
	// the UI store (migrations/001_legacy.up.sql:222-227).
	want := []string{
		"claimlogic",
		"gatewayedi",
		"org.remitt.plugin.transport.ClaimLogicTransport",
		"org.remitt.plugin.transport.GatewayEdiTransport",
		"org.remitt.plugin.transport.ScriptedHttpTransport",
		"org.remitt.plugin.transport.SftpTransport",
		"org.remitt.plugin.transport.StoreFile",
		"org.remitt.plugin.transport.StoreFilePdf",
		"script",
		"sftp",
		"storefile",
		"storefilepdf",
	}
	got := registeredNames()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("registry keys = %v; want %v", got, want)
	}
}

func TestRegistry_EachNameResolvesToExpectedConcreteType(t *testing.T) {
	tests := []struct {
		name  string
		check func(Transporter) bool
		label string
	}{
		{"script", func(m Transporter) bool { _, ok := m.(*Script); return ok }, "*Script"},
		{"sftp", func(m Transporter) bool { _, ok := m.(*Sftp); return ok }, "*Sftp"},
		{"claimlogic", func(m Transporter) bool { _, ok := m.(*ClaimLogic); return ok }, "*ClaimLogic"},
		{"gatewayedi", func(m Transporter) bool { _, ok := m.(*GatewayEdi); return ok }, "*GatewayEdi"},
		{"storefile", func(m Transporter) bool { _, ok := m.(*StoreFile); return ok }, "*StoreFile"},
		{"storefilepdf", func(m Transporter) bool { _, ok := m.(*StoreFilePdf); return ok }, "*StoreFilePdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := InstantiateTransporter(tt.name)
			if err != nil {
				t.Fatalf("InstantiateTransporter(%q) = %v; want a plugin", tt.name, err)
			}
			if m == nil {
				t.Fatalf("InstantiateTransporter(%q) returned a nil plugin with a nil error", tt.name)
			}
			if !tt.check(m) {
				t.Fatalf("InstantiateTransporter(%q) = %T; want %s", tt.name, m, tt.label)
			}
		})
	}
}

func TestRegistry_FactoryReturnsAFreshInstancePerCall(t *testing.T) {
	a, err := InstantiateTransporter("sftp")
	if err != nil {
		t.Fatal(err)
	}
	b, err := InstantiateTransporter("sftp")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("InstantiateTransporter(\"sftp\") returned the same instance twice (%p); each call must build a new plugin", a)
	}
}

func TestRegistry_UnknownNameReturnsErrorAndNilPlugin(t *testing.T) {
	for _, name := range []string{"", "nosuchtransport", "SFTP", "sftp ", " script", "org.remitt.plugin.transport.Nope"} {
		t.Run("name="+name, func(t *testing.T) {
			m, err := InstantiateTransporter(name)
			if err == nil {
				t.Fatalf("InstantiateTransporter(%q) returned a nil error; unknown names must fail", name)
			}
			if m != nil {
				t.Fatalf("InstantiateTransporter(%q) returned a non-nil plugin (%T) alongside its error", name, m)
			}
			if !strings.Contains(err.Error(), "unable to locate transporter") {
				t.Fatalf("InstantiateTransporter(%q) error = %q; want it to mention %q", name, err.Error(), "unable to locate transporter")
			}
		})
	}
}

// TestRegistry_JavaFQCNNamesResolveToTheSamePlugins asserts the names stored in
// the legacy seed (migrations/001_legacy.up.sql:222-227) and in the UI harness
// (ui/testHarness.html) resolve: jobqueue passes w.TransportPlugin straight to
// InstantiateTransporter, so a payload configured with the seeded Java class
// name must instantiate its plugin. The FQCN aliases are the real Java class
// names verified against migrations/001_legacy.up.sql and
// ../remitt/src/main/java/org/remitt/plugin/transport/ - note the two
// storefile plugins are StoreFile / StoreFilePdf, NOT StoreFileTransport /
// StoreFilePdfTransport.
//
// (This test replaces TestRegistry_JavaFQCNNamesDoNotResolve, which pinned the
// pre-fix behaviour: the FQCNs never resolved.)
func TestRegistry_JavaFQCNNamesResolveToTheSamePlugins(t *testing.T) {
	tests := []struct {
		fqcn  string
		short string
		check func(Transporter) bool
		label string
	}{
		{"org.remitt.plugin.transport.SftpTransport", "sftp", func(m Transporter) bool { _, ok := m.(*Sftp); return ok }, "*Sftp"},
		{"org.remitt.plugin.transport.ScriptedHttpTransport", "script", func(m Transporter) bool { _, ok := m.(*Script); return ok }, "*Script"},
		{"org.remitt.plugin.transport.ClaimLogicTransport", "claimlogic", func(m Transporter) bool { _, ok := m.(*ClaimLogic); return ok }, "*ClaimLogic"},
		{"org.remitt.plugin.transport.GatewayEdiTransport", "gatewayedi", func(m Transporter) bool { _, ok := m.(*GatewayEdi); return ok }, "*GatewayEdi"},
		{"org.remitt.plugin.transport.StoreFile", "storefile", func(m Transporter) bool { _, ok := m.(*StoreFile); return ok }, "*StoreFile"},
		{"org.remitt.plugin.transport.StoreFilePdf", "storefilepdf", func(m Transporter) bool { _, ok := m.(*StoreFilePdf); return ok }, "*StoreFilePdf"},
	}
	for _, tt := range tests {
		t.Run(tt.fqcn, func(t *testing.T) {
			m, err := InstantiateTransporter(tt.fqcn)
			if err != nil {
				t.Fatalf("InstantiateTransporter(%q) = %v; the seeded legacy name must resolve to the plugin registered as %q", tt.fqcn, err, tt.short)
			}
			if m == nil {
				t.Fatalf("InstantiateTransporter(%q) returned a nil plugin with a nil error", tt.fqcn)
			}
			if !tt.check(m) {
				t.Fatalf("InstantiateTransporter(%q) = %T; want %s", tt.fqcn, m, tt.label)
			}
			// The alias must behave exactly like the short name: same concrete
			// type, fresh instance per call.
			short, err := InstantiateTransporter(tt.short)
			if err != nil {
				t.Fatalf("InstantiateTransporter(%q) = %v; want a plugin", tt.short, err)
			}
			if !tt.check(short) {
				t.Fatalf("InstantiateTransporter(%q) = %T; want %s (the alias and the short name must agree)", tt.short, short, tt.label)
			}
			again, err := InstantiateTransporter(tt.fqcn)
			if err != nil {
				t.Fatalf("InstantiateTransporter(%q) on the second call = %v; want a plugin", tt.fqcn, err)
			}
			if again == m {
				t.Errorf("InstantiateTransporter(%q) returned the same instance twice (%p); each call must build a new plugin", tt.fqcn, m)
			}
		})
	}

	// The aliases are registry keys in their own right, not a fallback path.
	registered := make(map[string]bool, len(registeredNames()))
	for _, n := range registeredNames() {
		registered[n] = true
	}
	for _, tt := range tests {
		if !registered[tt.fqcn] {
			t.Errorf("%q is not a registry key; registered names are %v", tt.fqcn, registeredNames())
		}
	}

	// An unknown class in the same namespace is still an error, not a panic
	// and not a nil plugin with a nil error.
	for _, fqcn := range []string{JavaPluginPrefix + "NoSuchTransport", JavaPluginPrefix, ""} {
		m, err := InstantiateTransporter(fqcn)
		if err == nil {
			t.Fatalf("InstantiateTransporter(%q) returned a nil error; unknown names must fail", fqcn)
		}
		if m != nil {
			t.Fatalf("InstantiateTransporter(%q) = %T with err %v; want a nil plugin", fqcn, m, err)
		}
	}
}

func TestRegistry_RegisterTransporterAddsAndReplaces(t *testing.T) {
	const name = "transport_test_dynamic_plugin"
	// Ensure the registry is left exactly as found (there is no unregister API).
	cleanup := func() {
		transporterRegistryLock.Lock()
		delete(transporterRegistry, name)
		transporterRegistryLock.Unlock()
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := InstantiateTransporter(name); err == nil {
		t.Fatalf("test precondition failed: %q is already registered", name)
	}

	RegisterTransporter(name, func() Transporter { return &StoreFile{} })
	m, err := InstantiateTransporter(name)
	if err != nil {
		t.Fatalf("InstantiateTransporter(%q) after RegisterTransporter = %v; want a plugin", name, err)
	}
	if _, ok := m.(*StoreFile); !ok {
		t.Fatalf("InstantiateTransporter(%q) = %T; want *StoreFile", name, m)
	}

	// Re-registering the same name replaces the factory (last write wins).
	RegisterTransporter(name, func() Transporter { return &StoreFilePdf{} })
	m, err = InstantiateTransporter(name)
	if err != nil {
		t.Fatalf("InstantiateTransporter(%q) after re-register = %v; want a plugin", name, err)
	}
	if _, ok := m.(*StoreFilePdf); !ok {
		t.Fatalf("InstantiateTransporter(%q) after re-register = %T; want *StoreFilePdf (last registration must win)", name, m)
	}
}

func TestTransporterContract_OptionsAndInputFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		opts   []string
	}{
		{"script", "x12", []string{"script", "timeout"}},
		{"sftp", "x12", []string{"sftpUsername", "sftpPassword", "sftpHost", "sftpPort", "sftpPath"}},
		{"claimlogic", "x12", []string{"claimlogicHost", "claimlogicPort", "claimlogicUsername", "claimlogicPassword", "claimlogicPath"}},
		{"gatewayedi", "x12", []string{"gatewayEdiHost", "gatewayEdiPort", "gatewayEdiUsername", "gatewayEdiPassword", "gatewayEdiPath"}},
		{"storefile", "*", []string{}},
		{"storefilepdf", "pdf", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := InstantiateTransporter(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.InputFormat(); got != tt.format {
				t.Errorf("InputFormat() = %q; want %q", got, tt.format)
			}
			got := m.Options()
			if got == nil {
				t.Fatalf("Options() = nil; want a (possibly empty) slice")
			}
			if strings.Join(got, ",") != strings.Join(tt.opts, ",") {
				t.Errorf("Options() = %v; want %v", got, tt.opts)
			}
		})
	}
}

// TestTransporterContract_SetOptionsAndSetContextAreAccepted walks every
// registered plugin through the lifecycle jobqueue uses (SetContext +
// SetOptions) and asserts neither panics nor errors for the empty case.
func TestTransporterContract_SetOptionsAndSetContextAreAccepted(t *testing.T) {
	for _, name := range registeredNames() {
		t.Run(name, func(t *testing.T) {
			m, err := InstantiateTransporter(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.SetContext(context.Background()); err != nil {
				t.Errorf("SetContext(context.Background()) = %v; want nil", err)
			}
			if err := m.SetOptions(map[string]any{}); err != nil {
				t.Errorf("SetOptions(map[string]any{}) = %v; want nil", err)
			}
			if err := m.SetOptions(nil); err != nil {
				t.Errorf("SetOptions(nil) = %v; want nil", err)
			}
		})
	}
}

// TestTransporterContract_MissingConfigurationFailsWithErrorNotPanic asserts
// that every plugin, with no configuration and no user in context, returns a
// descriptive error instead of panicking or performing network I/O.
func TestTransporterContract_MissingConfigurationFailsWithErrorNotPanic(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		// claimlogic validates nothing up front and reads the user first.
		{"claimlogic", "claimlogic: unable to retrieve user from context"},
		// sftp and gatewayedi validate configuration before anything else.
		{"sftp", "sftp: missing host, port, or username"},
		{"gatewayedi", "gatewayedi: missing host, port, or username"},
		// the two storefile variants read the user from context.
		{"storefile", "storefile: unable to retrieve user from context"},
		{"storefilepdf", "storefilepdf: unable to retrieve user from context"},
		// script reads the user from context before anything else.
		{"script", "script: unable to retrieve user from context"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := InstantiateTransporter(tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.SetContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err := m.SetOptions(map[string]any{}); err != nil {
				t.Fatal(err)
			}
			err = m.Transport("payload.bin", []byte("payload"))
			if err == nil {
				t.Fatalf("Transport() with no configuration returned a nil error; want %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Fatalf("Transport() error = %q; want %q", err.Error(), tt.want)
			}
		})
	}
}

// TestTransporterContract_NilContextReturnsError makes sure a plugin whose
// context was never set (SetContext not called, or called with nil) fails
// cleanly rather than dereferencing a nil context.
func TestTransporterContract_NilContextReturnsError(t *testing.T) {
	tests := []string{"claimlogic", "gatewayedi", "script", "storefile", "storefilepdf"}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			m, err := InstantiateTransporter(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.SetContext(nil); err != nil {
				t.Fatal(err)
			}
			// Valid-looking options so the ctx check (not config validation)
			// is what rejects the call; no host is ever dialled because the
			// context lookup happens first.
			switch p := m.(type) {
			case *ClaimLogic:
				p.SetOptions(map[string]any{
					"claimlogicHost": "127.0.0.1", "claimlogicPort": 1,
					"claimlogicUsername": "u", "claimlogicPassword": "p", "claimlogicPath": "/tmp",
				})
			case *GatewayEdi:
				p.SetOptions(map[string]any{
					"gatewayEdiHost": "127.0.0.1", "gatewayEdiPort": 1,
					"gatewayEdiUsername": "u", "gatewayEdiPassword": "p", "gatewayEdiPath": "/tmp",
				})
			}
			if err := m.Transport("payload.bin", []byte("payload")); err == nil {
				t.Fatalf("Transport() with a nil context returned a nil error; want an error")
			}
		})
	}
}

// TestTransporterContract_UserFromContextIsThePayloadOwner pins that the
// plugin reads the identity from the CONTEXT (user.NewContext), which is what
// jobqueue.executeJob injects, rather than from options.
func TestTransporterContract_UserFromContextIsThePayloadOwner(t *testing.T) {
	u := &model.UserModel{Username: "alice", Id: 7}
	ctx := user.NewContext(context.Background(), u)

	// claimlogic is covered in claimlogic_test.go: it reaches ssh.Dial once a
	// user is present, so it cannot be exercised here without a dial attempt.
	// The storefile variants need model.Queries installed (they persist the
	// payload) - the stub keeps that in-process.
	for _, name := range []string{"gatewayedi", "storefile", "storefilepdf", "script"} {
		t.Run(name, func(t *testing.T) {
			if name == "storefile" || name == "storefilepdf" {
				withStubQueries(t, nil, nil, nil)
			}
			m, err := InstantiateTransporter(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.SetContext(ctx); err != nil {
				t.Fatal(err)
			}
			if err := m.SetOptions(map[string]any{}); err != nil {
				t.Fatal(err)
			}
			err = m.Transport("payload.bin", []byte("payload"))
			if err != nil && strings.Contains(err.Error(), "unable to retrieve user from context") {
				t.Fatalf("Transport() = %q even though the context carries a user; the plugin is not reading user.FromContext", err.Error())
			}
		})
	}
}
