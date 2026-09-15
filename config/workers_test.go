package config

import "testing"

// TestSetDefaultsProvidesUsableWorkerThreads pins the fix for a silent stall: with
// no default, a configuration that omitted timing-iterations started
// StartDispatcher(0) - zero workers - so the dispatcher read the queue, then
// blocked forever waiting for a free worker, and every payload sat unprocessed
// with no error (observed 2026-09-15: a payload stuck at 'valid', tProcessor
// threadId 0, no log line saying anything was wrong).
func TestSetDefaultsProvidesUsableWorkerThreads(t *testing.T) {
	c := &AppConfig{}
	c.SetDefaults()

	if c.TimingIterations.NumWorkerThreads < 1 {
		t.Errorf("SetDefaults() left worker-threads at %d; the pipeline would stall silently "+
			"(the dispatcher accepts work and then waits for a worker that never exists)",
			c.TimingIterations.NumWorkerThreads)
	}
}

// TestSetDefaultsProvidesAQueuePoller mirrors the above for the other knob the
// pipeline needs to make progress on its own.
func TestSetDefaultsProvidesAQueuePoller(t *testing.T) {
	c := &AppConfig{}
	c.SetDefaults()

	if !c.Queue.PollEnabled {
		t.Error("SetDefaults() left queue.poll-enabled false; a payload inserted by any path " +
			"other than the API would never be picked up (the Java's ControlThread always polled)")
	}
	if c.Queue.PollIntervalMs != 500 {
		t.Errorf("SetDefaults() set queue.poll-interval-ms to %d; want 500, the Java's SLEEP_TIME",
			c.Queue.PollIntervalMs)
	}
}
