// The queue's safety net: a poller that feeds the worker pool from the
// database.
//
// The Java original had exactly one trigger, this one: ControlThread.run()
// slept SLEEP_TIME (500 ms, ControlThread.java:69) and called work()
// (:77-100), which asked getUnassignedPayloads() for the payloads the database
// says are valid and which no tProcessor row has claimed yet (:557-583, SQL at
// :563-567) and handed each to a free stage thread (:587-655).
//
// This port has two triggers, by the owner's decision: api.PayloadInsert
// enqueues the row it just inserted (so a submission never waits for the next
// pass), and this poller is the safety net for everything else - a payload
// written by another process (the soap adapter, an import script, a direct
// INSERT), a payload whose on-insert enqueue failed, and work that was waiting
// when the server restarted, since the queue itself is in memory and comes back
// empty.
//
// Both triggers converge on EnqueuePayload, so the poller inherits its
// behaviour: the tProcessor row it journals is what stops a payload from being
// polled twice, and its in-flight check skips a payload the insert path already
// queued (and vice versa).

package jobqueue

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/freemed/remitt-server/config"
)

// DefaultPollInterval is the Java's SLEEP_TIME default
// (ControlThread.java:69: `protected int SLEEP_TIME = 500;`), which is also the
// default of the `queue.poll-interval-ms` setting.
const DefaultPollInterval = 500 * time.Millisecond

// pollCycle is the body of one poller pass. It is a variable so a test can
// drive the loop with a stub: the loop's contract - a failing pass is logged
// and retried, never fatal - is what is under test, and reproducing "the
// database is down for one cycle" should not need a database.
var pollCycle = pollOnce

// PollInterval is how long the poller waits between passes over tPayload.
//
// Configured by `queue.poll-interval-ms`; anything <= 0 (including a process
// with no configuration loaded at all, which is what a unit test has) falls
// back to DefaultPollInterval, the Java's 500 ms.
func PollInterval() time.Duration {
	if config.Config == nil || config.Config.Queue.PollIntervalMs <= 0 {
		return DefaultPollInterval
	}
	return time.Duration(config.Config.Queue.PollIntervalMs) * time.Millisecond
}

// pollIntervalMillis reports the configured value for logging, resolving the
// same default PollInterval does.
func pollIntervalMillis() int {
	if config.Config == nil || config.Config.Queue.PollIntervalMs <= 0 {
		return int(DefaultPollInterval / time.Millisecond)
	}
	return config.Config.Queue.PollIntervalMs
}

// PollEnabled reports whether the poller should run at all
// (`queue.poll-enabled`). With no configuration loaded the poller is on: that
// is the Java's behaviour, where ControlThread always polled and a deployment
// had to be configured out of it.
func PollEnabled() bool {
	if config.Config == nil {
		return true
	}
	return config.Config.Queue.PollEnabled
}

// StartPoller starts the poller with the configured interval. It returns
// immediately; the poller runs until ctx is cancelled.
func StartPoller(ctx context.Context) {
	StartPollerWithInterval(ctx, PollInterval())
}

// StartPollerWithInterval starts the poller on an explicit interval. A
// non-positive interval means the default.
//
// The loop is deliberately unkillable by data: a poll whose query fails (the
// database is down, a table is missing, an id refers to a row that was deleted
// between the query and the load) is logged and the next pass is attempted on
// schedule. A poller that stops on the first error is worse than no poller,
// because the failure is then invisible: nothing is retried and nothing is
// logged again.
func StartPollerWithInterval(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	if ctx == nil {
		ctx = context.Background()
	}
	go func() {
		log.Printf("jobqueue: payload poller started (interval %s)", interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Print("jobqueue: payload poller stopped (context cancelled)")
				return
			case <-ticker.C:
				n, err := pollCycle(ctx)
				if err != nil {
					log.Printf("jobqueue: poll failed, retrying in %s: %s", interval, err.Error())
					continue
				}
				if n > 0 {
					log.Printf("jobqueue: poller enqueued %d payload(s) that had no job yet", n)
				}
			}
		}
	}()
}

// pollOnce is one pass: the database's answer to "which payloads still need a
// job", each one enqueued through the same EnqueuePayload the insert path uses.
//
// The returned count is how many payloads this pass actually enqueued. A
// payload that is already in flight is SKIPPED, not an error - that is the
// expected outcome when the insert path beat the poller to it - and a payload
// that fails to enqueue is logged and the pass carries on with the rest, so one
// bad row cannot starve the queue.
func pollOnce(ctx context.Context) (int, error) {
	ids, err := dbUnassignedPayloadIDs(ctx)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, id := range ids {
		jobID, err := EnqueuePayload(ctx, id)
		if err != nil {
			if errors.Is(err, ErrPayloadInFlight) {
				log.Printf("jobqueue: poller skipping payload %d: already in flight", id)
				continue
			}
			log.Printf("jobqueue: poller could not enqueue payload %d: %s", id, err.Error())
			continue
		}
		log.Printf("jobqueue: poller enqueued payload %d as job %d", id, jobID)
		enqueued++
	}
	return enqueued, nil
}
