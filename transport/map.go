package transmission

import (
	"errors"
	"sync"
)

var (
	transmitterRegistry     = map[string]func() Transmitter{}
	transmitterRegistryLock = new(sync.Mutex)
)

// RegisterTranslator adds a new Transmitter instance to the registry
func RegisterTransmitter(name string, m func() Transmitter) {
	transmitterRegistryLock.Lock()
	defer transmitterRegistryLock.Unlock()
	transmitterRegistry[name] = m
}

// InstantiateTransmitter instantiates a Transmitter by name
func InstantiateTransmitter(name string) (m Transmitter, err error) {
	var f func() Transmitter
	var found bool
	if f, found = transmitterRegistry[name]; !found {
		err = errors.New("unable to locate transmitter " + name)
		return
	}
	m = f()
	err = nil
	return
}
