package render

import (
	"errors"
	"sync"
)

var (
	rendererRegistry     = map[string]func() Renderer{}
	rendererRegistryLock = new(sync.Mutex)
)

// RegisterRenderer adds a new Renderer factory to the registry.
func RegisterRenderer(name string, m func() Renderer) {
	rendererRegistryLock.Lock()
	defer rendererRegistryLock.Unlock()
	rendererRegistry[name] = m
}

// InstantiateRenderer instantiates a Renderer by name.
func InstantiateRenderer(name string) (Renderer, error) {
	f, found := rendererRegistry[name]
	if !found {
		return nil, errors.New("unable to locate renderer " + name)
	}
	return f(), nil
}
