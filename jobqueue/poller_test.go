package jobqueue

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/internal/dbgen"
)

// The poller is the safety net behind the on-insert trigger, so these tests pin
// the three things that make it one: it enqueues what the database says still
// needs a job, it SKIPS what is already in flight, and nothing that happens
// inside a pass can kill it.

// TestPollOnceEnqueuesWhatNeedsAJob: the poll query's answer is enqueued through
// the same EnqueuePayload the API uses.
func TestPollOnceEnqueuesWhatNeedsAJob(t *testing.T) {
	resetQueue(t)
	stubs := (&queueStubs{
		rows: map[int64]dbgen.Tpayload{
			11: testPayloadRow(11),
			12: testPayloadRow(12),
		},
		processor:  map[int64]int64{11: 1111, 12: 1212},
		unassigned: []int64{11, 12},
	}).install(t)

	n, err := pollOnce(context.Background())
	if err != nil {
		t.Fatalf("pollOnce() = %v; want no error", err)
	}
	if n != 2 {
		t.Errorf("pollOnce() enqueued %d payloads; want 2", n)
	}
	if got := queuedCount(); got != 2 {
		t.Errorf("jobQueue holds %d items; want 2", got)
	}
	if len(stubs.inserts) != 2 {
		t.Errorf("tProcessor inserts = %d; want one per payload, so neither can be polled twice", len(stubs.inserts))
	}
}

// TestPollOnceSkipsInFlightAndSurvivesOneBadPayload: the poller racing the
// on-insert trigger must do nothing for the payload that already has a job, and
// a payload that cannot be enqueued must not stop the rest of the pass.
func TestPollOnceSkipsInFlightAndSurvivesOneBadPayload(t *testing.T) {
	resetQueue(t)
	stubs := (&queueStubs{
		rows: map[int64]dbgen.Tpayload{
			11: testPayloadRow(11),
			12: testPayloadRow(12),
			13: testPayloadRow(13),
		},
		loadErr:    map[int64]error{12: errors.New("stub: payload 12 vanished between the poll and the load")},
		processor:  map[int64]int64{11: 1111, 13: 1313},
		unassigned: []int64{11, 12, 13},
	}).install(t)

	// The insert path wins the race for payload 11.
	if _, err := EnqueuePayload(context.Background(), 11); err != nil {
		t.Fatalf("EnqueuePayload(11) = %v; want success", err)
	}
	stubs.inserts = nil

	n, err := pollOnce(context.Background())
	if err != nil {
		t.Fatalf("pollOnce() = %v; want no error: one bad payload must not fail the pass", err)
	}
	if n != 1 {
		t.Errorf("pollOnce() enqueued %d payloads; want 1 (13; 11 is in flight and 12 cannot be loaded)", n)
	}
	if got := queuedCount(); got != 2 {
		t.Errorf("jobQueue holds %d items; want 2 (payload 11 from the insert path, payload 13 from the poller)", got)
	}
	if len(stubs.inserts) != 1 {
		t.Errorf("tProcessor inserts during the poll = %d; want 1 for payload 13 only", len(stubs.inserts))
	}
	if len(stubs.inserts) == 1 && stubs.inserts[0].PayloadID != 13 {
		t.Errorf("the poller journaled payload %d; want 13", stubs.inserts[0].PayloadID)
	}
}

// TestPollOnceReportsAQueryFailure: a failing poll query is an error the caller
// logs - and everything else about the poller depends on it being survivable.
func TestPollOnceReportsAQueryFailure(t *testing.T) {
	resetQueue(t)
	(&queueStubs{
		pollErr: errors.New("stub: connection refused"),
	}).install(t)

	if _, err := pollOnce(context.Background()); err == nil {
		t.Fatal("pollOnce() reported no error although the poll query failed")
	}
}

// TestPollerSurvivesAFailingPollCycle is the requirement that a poller which
// dies on its first failure is worse than none: the failure is then invisible.
// The stub fails two cycles and then succeeds, and the poller must keep
// running - and must still stop when its context is cancelled.
func TestPollerSurvivesAFailingPollCycle(t *testing.T) {
	saved := pollCycle
	defer func() { pollCycle = saved }()

	var cycles int64
	pollCycle = func(ctx context.Context) (int, error) {
		n := atomic.AddInt64(&cycles, 1)
		if n <= 2 {
			return 0, errors.New("stub: database is down")
		}
		return 1, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	StartPollerWithInterval(ctx, 2*time.Millisecond)

	deadline := time.Now().Add(5 * time.Second)
	for atomic.LoadInt64(&cycles) < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := atomic.LoadInt64(&cycles); got < 3 {
		t.Fatalf("the poller ran %d cycles and stopped: a failing poll cycle killed it", got)
	}

	// Cancelling must stop it, so nothing leaks a goroutine polling forever.
	cancel()
	time.Sleep(20 * time.Millisecond)
	before := atomic.LoadInt64(&cycles)
	time.Sleep(50 * time.Millisecond)
	if after := atomic.LoadInt64(&cycles); after != before {
		t.Errorf("the poller kept polling after its context was cancelled (%d -> %d cycles)", before, after)
	}
}

// TestPollIntervalAndEnablementDefaults: the poller is configurable, and its
// defaults are the Java original's - on, every 500 ms
// (ControlThread.java:69, :77-100).
func TestPollIntervalAndEnablementDefaults(t *testing.T) {
	saved := config.Config
	defer func() { config.Config = saved }()

	// No configuration loaded at all (a unit test, or a process that never
	// called SetDefaults): the Java's defaults apply.
	config.Config = nil
	if got := PollInterval(); got != 500*time.Millisecond {
		t.Errorf("PollInterval() with no config = %s; want the Java's 500ms", got)
	}
	if !PollEnabled() {
		t.Error("PollEnabled() with no config = false; want true (the Java always polled)")
	}

	// The shipped defaults.
	c := &config.AppConfig{}
	c.SetDefaults()
	config.Config = c
	if got := PollInterval(); got != 500*time.Millisecond {
		t.Errorf("PollInterval() after SetDefaults = %s; want 500ms", got)
	}
	if !PollEnabled() {
		t.Error("PollEnabled() after SetDefaults = false; want true")
	}

	// The configured values.
	c.Queue.PollIntervalMs = 1500
	if got := PollInterval(); got != 1500*time.Millisecond {
		t.Errorf("PollInterval() with queue.poll-interval-ms=1500 = %s; want 1.5s", got)
	}
	if got := pollIntervalMillis(); got != 1500 {
		t.Errorf("pollIntervalMillis() = %d; want 1500", got)
	}
	c.Queue.PollEnabled = false
	if PollEnabled() {
		t.Error("PollEnabled() with queue.poll-enabled=false = true")
	}

	// A nonsensical interval falls back rather than spinning on a zero ticker.
	c.Queue.PollIntervalMs = 0
	if got := PollInterval(); got != DefaultPollInterval {
		t.Errorf("PollInterval() with a 0 interval = %s; want the %s default", got, DefaultPollInterval)
	}
}
