package scooper

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// registerTestScooper registers a factory under a test-only name and removes it
// again when the test ends, so the process-wide registry is left as found.
func registerTestScooper(t *testing.T, name string, factory func() Scooper) {
	t.Helper()

	RegisterScooper(name, factory)
	t.Cleanup(func() {
		scooperRegistryLock.Lock()
		defer scooperRegistryLock.Unlock()
		delete(scooperRegistry, name)
	})
}

// TestInstantiateScooper_RegisteredClasses is the registry happy path: both
// production classes must resolve to their concrete implementation type.
func TestInstantiateScooper_RegisteredClasses(t *testing.T) {
	tests := []struct {
		name      string
		class     string
		wantType  string
		extraWant func(t *testing.T, s Scooper)
	}{
		{
			name:     "SftpScooper",
			class:    SftpScooperClass,
			wantType: "*scooper.SftpScooper",
			extraWant: func(t *testing.T, s Scooper) {
				t.Helper()
				if _, ok := s.(*SftpScooper); !ok {
					t.Errorf("instantiated scooper is %T; want *scooper.SftpScooper", s)
				}
			},
		},
		{
			name:     "GatewayEdiSftpScooper",
			class:    GatewayEdiScooperClass,
			wantType: "*scooper.GatewayEdiSftpScooper",
			extraWant: func(t *testing.T, s Scooper) {
				t.Helper()
				g, ok := s.(*GatewayEdiSftpScooper)
				if !ok {
					t.Errorf("instantiated scooper is %T; want *scooper.GatewayEdiSftpScooper", s)
					return
				}
				// The registry must hand back the decrypting subclass, not a
				// plain SftpScooper.
				if g.GetEnabledConfigValue() != GatewayEdiScooperEnabled {
					t.Errorf("GetEnabledConfigValue() = %q; want %q", g.GetEnabledConfigValue(), GatewayEdiScooperEnabled)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := InstantiateScooper(tt.class)
			if err != nil {
				t.Fatalf("InstantiateScooper(%q) error = %v; want nil", tt.class, err)
			}
			if s == nil {
				t.Fatalf("InstantiateScooper(%q) returned a nil Scooper", tt.class)
			}
			if got := fmt.Sprintf("%T", s); got != tt.wantType {
				t.Errorf("InstantiateScooper(%q) type = %s; want %s", tt.class, got, tt.wantType)
			}
			tt.extraWant(t, s)
		})
	}
}

// TestInstantiateScooper_UnknownNames pins the failure mode for names that are
// not registered, including the short class names a caller might guess from the
// Go symbols.
func TestInstantiateScooper_UnknownNames(t *testing.T) {
	tests := []struct {
		name  string
		class string
	}{
		{"emptyName", ""},
		{"unknownDottedClass", "org.remitt.plugin.scooper.NoSuchScooper"},
		{"goTypeNameNotRegistered", "SftpScooper"},
		{"lowercasedClass", "org.remitt.plugin.scooper.sftpscooper"},
		{"keyringNameIsNotAClass", GatewayEdiKeyName},
		{"whitespacePaddedClass", " " + SftpScooperClass},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := InstantiateScooper(tt.class)
			if err == nil {
				t.Fatalf("InstantiateScooper(%q) error = nil; want a lookup error", tt.class)
			}
			if s != nil {
				t.Errorf("InstantiateScooper(%q) returned %T alongside the error; want nil", tt.class, s)
			}
			if want := "unable to locate scooper " + tt.class; !strings.Contains(err.Error(), want) {
				t.Errorf("InstantiateScooper(%q) error = %q; want it to contain %q", tt.class, err.Error(), want)
			}
		})
	}
}

// TestInstantiateScooper_ReturnsFreshInstance guards against a shared instance
// leaking state (username, context, parameters) between scoop runs.
func TestInstantiateScooper_ReturnsFreshInstance(t *testing.T) {
	first, err := InstantiateScooper(SftpScooperClass)
	if err != nil {
		t.Fatalf("InstantiateScooper(%q) error = %v", SftpScooperClass, err)
	}
	second, err := InstantiateScooper(SftpScooperClass)
	if err != nil {
		t.Fatalf("InstantiateScooper(%q) error = %v", SftpScooperClass, err)
	}
	if first == second {
		t.Fatal("InstantiateScooper returned the same instance twice; scooper runs would share state")
	}

	a, ok := first.(*SftpScooper)
	if !ok {
		t.Fatalf("first instance is %T; want *scooper.SftpScooper", first)
	}
	b, ok := second.(*SftpScooper)
	if !ok {
		t.Fatalf("second instance is %T; want *scooper.SftpScooper", second)
	}

	if err := a.SetUsername("user-a"); err != nil {
		t.Fatalf("SetUsername: %v", err)
	}
	if b.username != "" {
		t.Errorf("second instance username = %q; want empty (instances share state)", b.username)
	}
}

// TestRegisterScooper_LaterRegistrationWins pins the documented override
// behaviour of the registry map.
func TestRegisterScooper_LaterRegistrationWins(t *testing.T) {
	const name = "test.scooper.override"

	registerTestScooper(t, name, func() Scooper { return &SftpScooper{} })
	registerTestScooper(t, name, func() Scooper { return &GatewayEdiSftpScooper{} })

	s, err := InstantiateScooper(name)
	if err != nil {
		t.Fatalf("InstantiateScooper(%q) error = %v", name, err)
	}
	if _, ok := s.(*GatewayEdiSftpScooper); !ok {
		t.Errorf("InstantiateScooper(%q) = %T; want the most recent registration (*scooper.GatewayEdiSftpScooper)", name, s)
	}
}

// TestRegisterScooper_ConcurrentRegistrations exercises the write side of the
// registry mutex: many goroutines registering distinct names at once. Run with
// -race this fails if RegisterScooper ever stops locking. Lookups are done
// afterwards on purpose: mixing them with writes is covered by
// TestInstantiateScooper_ReadsRegistryUnderLock and its process-level sibling
// below.
func TestRegisterScooper_ConcurrentRegistrations(t *testing.T) {
	const workers = 16

	var wg sync.WaitGroup
	names := make([]string, 0, workers)

	for i := 0; i < workers; i++ {
		name := fmt.Sprintf("test.scooper.concurrent.%d", i)
		names = append(names, name)
		wg.Add(1)
		go func() {
			defer wg.Done()
			RegisterScooper(name, func() Scooper { return &SftpScooper{} })
		}()
	}
	wg.Wait()

	t.Cleanup(func() {
		scooperRegistryLock.Lock()
		defer scooperRegistryLock.Unlock()
		for _, name := range names {
			delete(scooperRegistry, name)
		}
	})

	for _, name := range names {
		if _, err := InstantiateScooper(name); err != nil {
			t.Errorf("InstantiateScooper(%q) after concurrent registration error = %v", name, err)
		}
	}
}

// TestInstantiateScooper_ReadsRegistryUnderLock is the regression test for the
// locking asymmetry in scooper/map.go: RegisterScooper writes the registry under
// scooperRegistryLock, so the lookup in InstantiateScooper must take the same
// lock. Concurrent registration and instantiation would otherwise be an
// unsynchronised map read/write, which the Go runtime aborts with
// "fatal error: concurrent map read and map write" — an unrecoverable crash, not
// a recoverable panic (see the process-level reproduction below).
//
// With the write lock held the lookup must block: that is what proves it shares
// the lock with RegisterScooper.
func TestInstantiateScooper_ReadsRegistryUnderLock(t *testing.T) {
	scooperRegistryLock.Lock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Deliberately the production lookup, racing the lock the test holds.
		_, _ = InstantiateScooper(SftpScooperClass)
	}()

	select {
	case <-done:
		scooperRegistryLock.Unlock()
		t.Fatal("InstantiateScooper returned while scooperRegistryLock was held; the registry read is not synchronised with RegisterScooper")
	case <-time.After(250 * time.Millisecond):
		// Expected: the lookup is waiting for the registry lock.
	}

	scooperRegistryLock.Unlock()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("InstantiateScooper never returned after the registry lock was released")
	}
}

// scooperChildEnv marks the child process of
// TestInstantiateScooper_ConcurrentRegistrationAndLookupIsRaceFree.
const scooperChildEnv = "REMITT_SCOOPER_REGISTRY_RACE_CHILD"

// scooperHammerMarker is printed by the child once the hammer has run to
// completion, so the parent can tell "survived the race" from "died before
// doing any work".
const scooperHammerMarker = "scooper registry hammer completed without a fatal error"

const (
	// scooperHammerDuration keeps the hammer long enough to collide many times
	// and short enough not to dominate the suite.
	scooperHammerDuration = 1 * time.Second

	// scooperHammerNames bounds the set of registered names: the hammer must
	// contend on the same map (re-registration is still a map write), not grow
	// it without limit for the length of the run.
	scooperHammerNames = 32
)

// scooperHammerRegistry mixes concurrent registrations with concurrent lookups
// for scooperHammerDuration. If the lookup is not synchronised with the write,
// the runtime aborts the process ("concurrent map read and map write") and this
// function never returns.
func scooperHammerRegistry() {
	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = InstantiateScooper(SftpScooperClass)
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			RegisterScooper(fmt.Sprintf("child.hammer.%d", i%scooperHammerNames), func() Scooper { return &SftpScooper{} })
		}
	}()

	time.Sleep(scooperHammerDuration)
	close(stop)
	wg.Wait()
}

// TestInstantiateScooper_ConcurrentRegistrationAndLookupIsRaceFree is the
// process-level regression test for the unguarded registry read: lookups and
// registrations run concurrently until the runtime aborts the process with
// "fatal error: concurrent map read and map write". That failure cannot be
// recovered or asserted from inside the process, so the race is exercised in a
// re-executed child of this same test binary and the parent asserts on the
// child's exit status and output. The child also prints a marker once the
// hammer finishes, so a child that died early cannot be mistaken for a
// synchronised registry.
func TestInstantiateScooper_ConcurrentRegistrationAndLookupIsRaceFree(t *testing.T) {
	if os.Getenv(scooperChildEnv) == "1" {
		// Child mode: hammer the registry. The runtime kills this process if
		// the read and the write are not synchronised.
		scooperHammerRegistry()
		fmt.Println(scooperHammerMarker)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(), scooperChildEnv+"=1")
	out, err := cmd.CombinedOutput()

	if err != nil {
		t.Fatalf("child hammering RegisterScooper/InstantiateScooper concurrently exited with %v; want a clean exit; output:\n%s", err, out)
	}
	if strings.Contains(string(out), "concurrent map read and map write") {
		t.Fatalf("child died on a concurrent map read and map write; output:\n%s", out)
	}
	if !strings.Contains(string(out), scooperHammerMarker) {
		t.Fatalf("child never finished the registry hammer, so the race was not exercised; output:\n%s", out)
	}
}

// TestKnownBug_RegistryAcceptsNilFactoryAndReturnsNilScooper pins a second
// footgun in scooper/map.go:21-27: RegisterScooper accepts any factory and
// InstantiateScooper reports success for a factory that returns nil, so a
// broken plugin yields "nil, nil" and the caller dereferences a nil Scooper
// later. This documents current behaviour, it is not the desired contract.
func TestKnownBug_RegistryAcceptsNilFactoryAndReturnsNilScooper(t *testing.T) {
	const name = "test.scooper.nilfactory"
	registerTestScooper(t, name, func() Scooper { return nil })

	s, err := InstantiateScooper(name)
	if err != nil {
		t.Fatalf("InstantiateScooper(%q) error = %v; want nil (current behaviour)", name, err)
	}
	if s != nil {
		t.Fatalf("InstantiateScooper(%q) = %T; want nil (current behaviour)", name, s)
	}

	// Evidence of the hazard: the returned value cannot be used at all.
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected a nil-factory scooper to panic when used; the registry cannot be relied on to reject it")
		}
	}()
	_ = s.GetEnabledConfigValue()
}
