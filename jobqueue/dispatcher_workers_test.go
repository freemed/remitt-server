package jobqueue

import "testing"

// TestUsableWorkerCountNeverReturnsZero pins the fix for a silent stall found on
// 2026-09-15. With a non-positive worker count the dispatcher started NO workers,
// but still read jobQueueChannel and then blocked forever on
// `worker := <-WorkerQueue` waiting for one - so a payload was accepted, journaled
// into tProcessor, and never processed, with nothing logged as an error. A
// configuration that omitted timing-iterations produced exactly that.
//
// This tests the clamp as a pure function on purpose: starting a real dispatcher
// inside the test binary leaves a worker consuming jobQueueChannel for the rest of
// the run, which silently broke another test's channel assertions.
func TestUsableWorkerCountNeverReturnsZero(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"configured", 8, 8},
		{"one", 1, 1},
		{"zero is not usable", 0, 1},
		{"negative is not usable", -3, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := usableWorkerCount(tc.in); got != tc.want {
				t.Errorf("usableWorkerCount(%d) = %d; want %d (a zero worker pool stalls every job)",
					tc.in, got, tc.want)
			}
			if got := usableWorkerCount(tc.in); got < 1 {
				t.Fatalf("usableWorkerCount(%d) returned %d; the dispatcher would never hand work to a worker",
					tc.in, got)
			}
		})
	}
}
