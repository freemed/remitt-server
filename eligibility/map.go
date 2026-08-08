package eligibility

import (
	"errors"
	"sync"
)

var (
	checkerRegistry     = map[string]func() EligibilityChecker{}
	checkerRegistryLock = new(sync.Mutex)
)

// RegisterChecker adds a new EligibilityChecker factory to the registry.
func RegisterChecker(name string, m func() EligibilityChecker) {
	checkerRegistryLock.Lock()
	defer checkerRegistryLock.Unlock()
	checkerRegistry[name] = m
}

// InstantiateChecker instantiates an EligibilityChecker by name.
func InstantiateChecker(name string) (EligibilityChecker, error) {
	f, found := checkerRegistry[name]
	if !found {
		return nil, errors.New("unable to locate eligibility checker " + name)
	}
	return f(), nil
}
