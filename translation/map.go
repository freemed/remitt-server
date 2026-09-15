package translation

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// JavaPluginPrefix is the Java package every translation plugin in the legacy
// system lived in. The values the legacy database stores are those Java class
// names, not short names: tTranslation.plugin (the seed is
// 'org.remitt.plugin.translation.X12Xml', migrations/001_legacy.up.sql:319-323),
// tPlugins.plugin (001_legacy.up.sql:217-220) and the UI harness
// (ui/testHarness.html:81-89) all carry FQCNs, and p_ResolveTranslationPlugin
// returns the tTranslation.plugin value verbatim, so each plugin is registered
// under its short name AND under its Java class name - exactly the arrangement
// transport/map.go:14-34 established for the transport registry.
const JavaPluginPrefix = "org.remitt.plugin.translation."

var (
	translatorRegistry     = map[string]func() Translator{}
	translatorRegistryLock = new(sync.Mutex)
)

// RegisterTranslator adds a new Translator instance to the registry
func RegisterTranslator(name string, m func() Translator) {
	translatorRegistryLock.Lock()
	defer translatorRegistryLock.Unlock()
	translatorRegistry[name] = m
}

// registerJavaTranslator registers a translator factory under the Java class
// name (JavaPluginPrefix + class) of the plugin it was ported from, so the
// values the legacy database and the UI store resolve. The short name stays
// registered as well; both keys must always point at the same factory.
func registerJavaTranslator(class string, m func() Translator) {
	RegisterTranslator(JavaPluginPrefix+class, m)
}

// isJavaAlias reports whether a registry key is a Java FQCN alias rather than a
// short name.
func isJavaAlias(name string) bool {
	return strings.HasPrefix(name, JavaPluginPrefix)
}

// registeredTranslatorNames returns a sorted snapshot of the registry keys.
// The registry and its mutex are package-private, so this is also the only way
// a test in another file can enumerate them.
func registeredTranslatorNames() []string {
	translatorRegistryLock.Lock()
	names := make([]string, 0, len(translatorRegistry))
	for k := range translatorRegistry {
		names = append(names, k)
	}
	translatorRegistryLock.Unlock()
	sort.Strings(names)
	return names
}

// InstantiateTranslator instantiates a Translator by name. Both the short name
// and the Java FQCN resolve (see registerJavaTranslator).
func InstantiateTranslator(name string) (m Translator, err error) {
	// The registry is written by RegisterTranslator (from plugin init
	// functions and from callers) while job workers read it here; an
	// unsynchronised concurrent map read and map write is a fatal runtime
	// error, so every access takes the same lock. The factory itself is called
	// outside the lock so plugin construction can never deadlock against a
	// concurrent registration.
	translatorRegistryLock.Lock()
	var f func() Translator
	var found bool
	if f, found = translatorRegistry[name]; !found {
		translatorRegistryLock.Unlock()
		err = errors.New("unable to locate translator " + name)
		return
	}
	translatorRegistryLock.Unlock()

	m = f()
	err = nil
	return
}

// ResolveTranslator resolves the translation plugin which converts between
// in and out.
//
// The Java FQCN aliases are registry keys too, so a naive walk could return
// either form for the same plugin and (map iteration being unordered) hand a
// different name to the caller on every run. Resolution therefore answers with
// the SHORT name whenever a short-named plugin matches, and only falls back to
// FQCN-only registrations when nothing short-named does - callers log and pass
// the returned name straight to InstantiateTranslator, which accepts both.
func ResolveTranslator(in, out string) (string, error) {
	names := registeredTranslatorNames()
	for _, aliases := range []bool{false, true} {
		for _, name := range names {
			if isJavaAlias(name) != aliases {
				continue
			}
			m, err := InstantiateTranslator(name)
			if err != nil {
				continue
			}
			if m.Resolver(in, out) {
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("unable to resolve translator between '%s' and '%s'", in, out)
}
