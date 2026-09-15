//go:build transport_race_probe

package transport

// registry_race_probe_test.go is a TAG-GATED probe (build tag
// "transport_race_probe"), deliberately excluded from the default test suite:
// it demonstrates a fatal data race in the registry and therefore cannot live
// in the always-on suite.
//
// Run it explicitly:
//
//	go test -tags transport_race_probe -race -run TestRegistryRace -v .
//
// Expected result today (map.go:21-27 reads the registry without taking
// transporterRegistryLock while RegisterTransporter writes it under the lock):
// either "fatal error: concurrent map read and map write" (no -race) or a
// "WARNING: DATA RACE" report (-race). The map_test.go suite pins the registry's
// FUNCTIONAL contract; this probe pins the concurrency defect.

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

	// Readers: InstantiateTransporter does NOT hold the lock (map.go:21-27).
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
