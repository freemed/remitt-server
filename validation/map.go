package validation

import (
	"errors"
	"sync"
)

var (
	validatorRegistry     = map[string]func() Validator{}
	validatorRegistryLock = new(sync.Mutex)
)

// RegisterValidator adds a new Validator instance to the registry
func RegisterValidator(name string, m func() Validator) {
	validatorRegistryLock.Lock()
	defer validatorRegistryLock.Unlock()
	validatorRegistry[name] = m
}

// InstantiateValidator instantiates a Validator by name
func InstantiateValidator(name string) (Validator, error) {
	f, found := validatorRegistry[name]
	if !found {
		return nil, errors.New("unable to locate validator " + name)
	}
	return f(), nil
}
