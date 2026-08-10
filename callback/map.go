package callback

import (
	"context"
	"errors"
	"sync"

	"github.com/freemed/remitt-server/model"
)

var (
	callbackRegistry     = map[string]func() CallbackSender{}
	callbackRegistryLock = new(sync.Mutex)

	// defaultCallbackSender is the package-level default callback sender.
	defaultCallbackSender CallbackSender = &noopCallbackSender{}
)

// RegisterCallback adds a new CallbackSender factory to the registry.
func RegisterCallback(name string, factory func() CallbackSender) {
	callbackRegistryLock.Lock()
	defer callbackRegistryLock.Unlock()
	callbackRegistry[name] = factory
}

// InstantiateCallback instantiates a CallbackSender by name.
func InstantiateCallback(name string) (CallbackSender, error) {
	callbackRegistryLock.Lock()
	defer callbackRegistryLock.Unlock()
	f, found := callbackRegistry[name]
	if !found {
		return nil, errors.New("unable to locate callback sender " + name)
	}
	return f(), nil
}

// SetDefaultCallbackSender sets the package-level default callback sender.
func SetDefaultCallbackSender(sender CallbackSender) {
	if sender != nil {
		defaultCallbackSender = sender
	}
}

// DefaultCallbackSender returns the current default callback sender.
func DefaultCallbackSender() CallbackSender {
	return defaultCallbackSender
}

// noopCallbackSender is a no-op implementation used when no default is set.
type noopCallbackSender struct{}

func (n *noopCallbackSender) SendResult(_ context.Context, _ *model.UserModel, _ JobResult) error {
	return nil
}
