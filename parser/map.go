package parser

import (
	"errors"
	"sync"
)

var (
	parserRegistry     = map[string]func() Parser{}
	parserRegistryLock = new(sync.Mutex)
)

func RegisterParser(name string, m func() Parser) {
	parserRegistryLock.Lock()
	defer parserRegistryLock.Unlock()
	parserRegistry[name] = m
}

func InstantiateParser(name string) (Parser, error) {
	f, found := parserRegistry[name]
	if !found {
		return nil, errors.New("unable to locate parser " + name)
	}
	return f(), nil
}
