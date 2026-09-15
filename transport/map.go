package transport

import (
	"errors"
	"sync"
)

// JavaPluginPrefix is the Java package every transport plugin in the legacy
// system lived in. The values stored in the legacy database (tPlugin.name in
// migrations/001_legacy.up.sql) and in the UI harness (ui/testHarness.html) are
// Java class names such as "org.remitt.plugin.transport.SftpTransport", and
// jobqueue passes the stored value straight to InstantiateTransporter, so each
// plugin is registered under its short name AND under its Java class name.
const JavaPluginPrefix = "org.remitt.plugin.transport."

var (
	transporterRegistry     = map[string]func() Transporter{}
	transporterRegistryLock = new(sync.Mutex)
)

// RegisterTransporter adds a new Transporter instance to the registry
func RegisterTransporter(name string, m func() Transporter) {
	transporterRegistryLock.Lock()
	defer transporterRegistryLock.Unlock()
	transporterRegistry[name] = m
}

// registerJavaTransporter registers a transport factory under the Java class
// name (JavaPluginPrefix + class) of the plugin it was ported from, so the
// values the legacy database and the UI store resolve. The short name stays
// registered as well; both keys must always point at the same factory.
func registerJavaTransporter(class string, m func() Transporter) {
	RegisterTransporter(JavaPluginPrefix+class, m)
}

// InstantiateTransporter instantiates a Transporter by name
func InstantiateTransporter(name string) (m Transporter, err error) {
	// The registry is written by RegisterTransporter (from plugin init
	// functions and from callers) while job workers read it here; an
	// unsynchronised concurrent map read and map write is a fatal runtime
	// error, so every access takes the same lock. The factory itself is called
	// outside the lock so plugin construction can never deadlock against a
	// concurrent registration.
	transporterRegistryLock.Lock()
	var f func() Transporter
	var found bool
	if f, found = transporterRegistry[name]; !found {
		transporterRegistryLock.Unlock()
		err = errors.New("unable to locate transporter " + name)
		return
	}
	transporterRegistryLock.Unlock()

	m = f()
	err = nil
	return
}
