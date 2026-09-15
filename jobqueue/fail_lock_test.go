package jobqueue

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestFailDoesNotDeadlockOnTheJobLock pins the fix for a self-deadlock: Fail held
// the job's write lock and then called the exported AppendLog, which takes the
// SAME non-reentrant sync.RWMutex, so marking a job FAILED blocked the caller
// forever while holding the job's own lock. Every failure path in executeJob
// returns through Fail (including the worker's own i.Fail), so a failing job hung
// instead of failing - which is exactly the behaviour anything that must "fail
// the job and say why" depends on.
func TestFailDoesNotDeadlockOnTheJobLock(t *testing.T) {
	w := &JobQueueItem{ID: 7, lock: new(sync.RWMutex)}

	done := make(chan struct{})
	go func() {
		w.Fail(errors.New("boom"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Fail() did not return within 5s: it is deadlocking on the job's own lock")
	}

	if w.Status != jobStatusMap[JobStatusFailed] {
		t.Errorf("Status = %q; want %q", w.Status, jobStatusMap[JobStatusFailed])
	}
	if !w.Completed.Valid {
		t.Error("Completed was not set by Fail()")
	}
	if w.Message != "boom" {
		t.Errorf("Message = %q; want %q", w.Message, "boom")
	}
	if len(w.Log) != 1 {
		t.Fatalf("Log has %d entries; want 1 (Fail must still record the reason)", len(w.Log))
	}
	if !strings.Contains(w.Log[0], "boom") {
		t.Errorf("Log[0] = %q; want it to contain the failure reason", w.Log[0])
	}

	// The lock must be RELEASED, not merely not-deadlocked: a later call must work.
	released := make(chan struct{})
	go func() {
		w.AppendLog("after the failure")
		close(released)
	}()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("AppendLog() blocked after Fail() returned: the lock was not released")
	}
	if len(w.Log) != 2 {
		t.Errorf("Log has %d entries after a second call; want 2", len(w.Log))
	}
}

// TestJobQueueItemLockMustBeInitialised documents a trap for whoever writes the
// missing queue feeder. The per-item `lock` is a POINTER that no production code
// ever sets, and nothing in the tree constructs a JobQueueItem at all today (see
// the missing-trigger note in TODO.md), so the first call to AppendLog, Fail,
// Finish or Cancel on a freshly built item panics with a nil dereference. The
// feeder must set lock: new(sync.RWMutex) - this test exists so that is a known
// requirement rather than a crash on the first job failure.
func TestJobQueueItemLockMustBeInitialised(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("an item built without a lock did NOT panic: if the lock is now initialised " +
				"automatically, delete this test and the warning it documents")
		}
	}()
	(&JobQueueItem{ID: 8}).AppendLog("no lock")
}
