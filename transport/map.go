package transport

import (
	"errors"
	"sync"
)

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

// InstantiateTransporter instantiates a Transporter by name
func InstantiateTransporter(name string) (m Transporter, err error) {
	var f func() Transporter
	var found bool
	if f, found = transporterRegistry[name]; !found {
		err = errors.New("unable to locate transporter " + name)
		return
	}
	m = f()
	err = nil
	return
}
