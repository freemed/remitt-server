package translation

import (
	"errors"
	"fmt"
	"sync"
)

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

// InstantiateTranslator instantiates a Translator by name
func InstantiateTranslator(name string) (m Translator, err error) {
	var f func() Translator
	var found bool
	if f, found = translatorRegistry[name]; !found {
		err = errors.New("unable to locate translator " + name)
		return
	}
	m = f()
	err = nil
	return
}

// ResolveTranslator resolves the translation plugin which converts between
// in and out.
func ResolveTranslator(in, out string) (string, error) {
	translatorRegistryLock.Lock()
	defer translatorRegistryLock.Unlock()
	for k, v := range translatorRegistry {
		if v().Resolver(in, out) {
			return k, nil
		}
	}
	return "", fmt.Errorf("unable to resolve translator between '%s' and '%s'", in, out)
}
