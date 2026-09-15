//go:build transport_race_probe

package transport

// registry_race_probe_test.go is a TAG-GATED probe (build tag
// "transport_race_probe"), deliberately excluded from the default test suite:
// it hammers the registry from a writer goroutine and a reader loop, which is
// only meaningful under -race.
//
// Run it explicitly:
//
//	go test -tags transport_race_probe -race -run TestRegistryRace -v .
//
// The probe was written to demonstrate the defect that InstantiateTransporter
// read the registry without taking transporterRegistryLock while
// RegisterTransporter wrote it under the lock ("fatal error: concurrent map
// read and map write", or a "WARNING: DATA RACE" report). That is fixed -
// both sides now take the lock - so the probe is expected to pass cleanly
// under -race; a failure means the lock was lost again. The map_test.go suite
// pins the registry's FUNCTIONAL contract; this probe pins the concurrency
// contract.

import (
	"sync"
	"testing"
)

func TestRegistryRaceConcurrentRegisterAndInstantiate(t *testing.T) {
	const name = "race_probe_plugin"

	transporterRegistryLock.Lock()
	delete(transporterRegistry, name)
	transporterRegistryLock.Unlock()
	t.Cleanup(func() {
		transporterRegistryLock.Lock()
		delete(transporterRegistry, name)
		transporterRegistryLock.Unlock()
	})

	// Pre-register so the reader always finds the name; the writer then keeps
	// replacing it while readers look it up.
	RegisterTransporter(name, func() Transporter { return &StoreFile{} })

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: RegisterTransporter holds the lock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			RegisterTransporter(name, func() Transporter { return &StoreFile{} })
		}
	}()

	// Readers: InstantiateTransporter takes the lock for the registry lookup.
	for i := 0; i < 200000; i++ {
		if _, err := InstantiateTransporter(name); err != nil {
			t.Fatalf("InstantiateTransporter(%q) = %v", name, err)
		}
		if _, err := InstantiateTransporter("definitely_not_registered"); err == nil {
			t.Fatal("InstantiateTransporter on an unknown name returned a nil error")
		}
	}

	close(stop)
	wg.Wait()
}
