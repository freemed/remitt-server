package scooper

import (
	"errors"
	"sync"
)

var (
	scooperRegistry     = map[string]func() Scooper{}
	scooperRegistryLock = new(sync.Mutex)
)

// RegisterScooper registers a scooper factory function under the given name.
func RegisterScooper(name string, m func() Scooper) {
	scooperRegistryLock.Lock()
	defer scooperRegistryLock.Unlock()
	scooperRegistry[name] = m
}

// InstantiateScooper creates a new scooper instance by registered name.
func InstantiateScooper(name string) (Scooper, error) {
	f, found := scooperRegistry[name]
	if !found {
		return nil, errors.New("unable to locate scooper " + name)
	}
	return f(), nil
}
