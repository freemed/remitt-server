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
// afterwards on purpose: mixing them with writes is covered by the pinned bug
// below, which crashes the process.
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

// TestKnownBug_InstantiateScooperReadsRegistryWithoutLock pins the locking
// asymmetry in scooper/map.go: RegisterScooper writes under
// scooperRegistryLock (map.go:15-17) but InstantiateScooper reads the map with
// no lock at all (map.go:22). Concurrent registration and instantiation is
// therefore an unsynchronised map read/write, which the Go runtime aborts with
// "fatal error: concurrent map read and map write" — an unrecoverable crash,
// not a recoverable panic (see the process-level reproduction below).
//
// This test proves the asymmetry without triggering the crash: with the write
// lock held, a guarded reader would block, while the current unguarded reader
// returns immediately.
func TestKnownBug_InstantiateScooperReadsRegistryWithoutLock(t *testing.T) {
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
		// Current behaviour: the lookup proceeded while the write lock was
		// held, so it is not synchronised with RegisterScooper.
	case <-time.After(500 * time.Millisecond):
		scooperRegistryLock.Unlock()
		t.Fatal("InstantiateScooper blocked while scooperRegistryLock was held; the read is synchronised now, so this bug appears to be fixed")
	}
}

// scooperChildEnv marks the child process of
// TestKnownBug_ConcurrentInstantiateAndRegisterCrashesProcess.
const scooperChildEnv = "REMITT_SCOOPER_REGISTRY_RACE_CHILD"

// scooperHammerRegistry mixes concurrent registrations with concurrent lookups
// until the runtime aborts the process. It returning at all means the race was
// not detected.
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
			RegisterScooper(fmt.Sprintf("child.hammer.%d", i), func() Scooper { return &SftpScooper{} })
		}
	}()

	time.Sleep(3 * time.Second)
	close(stop)
	wg.Wait()
}

// TestKnownBug_ConcurrentInstantiateAndRegisterCrashesProcess reproduces the
// crash caused by the unguarded registry read. The failure is a runtime fatal
// error, which kills the test process and cannot be recovered or asserted from
// inside it, so the race is exercised in a re-executed child of this same test
// binary and the parent asserts on the child's exit status and output.
func TestKnownBug_ConcurrentInstantiateAndRegisterCrashesProcess(t *testing.T) {
	if os.Getenv(scooperChildEnv) == "1" {
		// Child mode: hammer the registry. If the runtime detects the race the
		// process dies here; reaching the end means no crash was observed.
		scooperHammerRegistry()
		return
	}

	const attempts = 3
	for attempt := 1; attempt <= attempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), scooperChildEnv+"=1")
		out, err := cmd.CombinedOutput()
		cancel()

		if err == nil {
			continue // no crash detected this attempt; retry the race
		}
		if !strings.Contains(string(out), "concurrent map read and map write") {
			t.Fatalf("child exited with %v but without the concurrent map error; output:\n%s", err, out)
		}
		return // reproduced
	}

	t.Fatalf("no child process crashed in %d attempts; the unguarded registry read may have been fixed", attempts)
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
