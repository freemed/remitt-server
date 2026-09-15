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
//
// The lookup takes scooperRegistryLock, the same lock RegisterScooper writes
// under: an unsynchronised map read racing a registration is a runtime fatal
// error ("concurrent map read and map write"), not a recoverable panic. The
// factory is called after the lock is released, so a slow or re-entrant factory
// cannot block registrations.
func InstantiateScooper(name string) (Scooper, error) {
	scooperRegistryLock.Lock()
	f, found := scooperRegistry[name]
	scooperRegistryLock.Unlock()

	if !found {
		return nil, errors.New("unable to locate scooper " + name)
	}
	return f(), nil
}
