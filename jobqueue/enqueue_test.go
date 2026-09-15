package jobqueue

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/internal/dbgen"
)

// This file tests the ONE enqueue function both triggers converge on
// (EnqueuePayload, enqueue.go) with no database at all: the database seams are
// package variables and are replaced with stubs here. That is deliberate - the
// queue's own contract (registration, the copy on the channel, the identity the
// transports need, the in-flight skip, and the rollback when journaling fails)
// is what the missing trigger got wrong, and it must be testable without MySQL.
//
// The database-facing halves are covered live instead: see the run recorded in
// .hermes/reports/feeder-live-run.md, where a payload inserted through
// api.PayloadInsert was processed by a real StartDispatcher.

// testPayloadRow is the tPayload row a stub load returns.
func testPayloadRow(id int64) dbgen.Tpayload {
	return dbgen.Tpayload{
		ID:              id,
		InsertStamp:     time.Now(),
		User:            "Administrator",
		Payload:         sql.NullString{String: "<x12>the payload</x12>", Valid: true},
		Originalid:      sql.NullString{String: "test-original-id", Valid: true},
		Renderplugin:    "org.remitt.plugin.render.PreRenderedPlugin",
		Renderoption:    "x12",
		Transportplugin: "org.remitt.plugin.transport.StoreFile",
		Transportoption: sql.NullString{},
		Payloadstate:    sql.NullString{String: payloadStateValid, Valid: true},
	}
}

// queueStubs replaces every database seam the queue uses and records what was
// asked of it.
type queueStubs struct {
	rows       map[int64]dbgen.Tpayload
	loadErr    map[int64]error
	processor  map[int64]int64 // payload id -> the tProcessor id journaled
	insertErr  error
	inserts    []dbgen.InsertProcessorParams
	finishes   []int64
	states     []dbgen.SetPayloadStateParams
	threadIDs  []dbgen.SetProcessorThreadIdParams
	unassigned []int64
	pollErr    error
}

// install swaps the seams and restores them (and the queue) when the test ends.
func (s *queueStubs) install(t *testing.T) *queueStubs {
	t.Helper()
	if s.rows == nil {
		s.rows = map[int64]dbgen.Tpayload{}
	}
	if s.processor == nil {
		s.processor = map[int64]int64{}
	}

	savedGet, savedUnassigned := dbGetPayload, dbUnassignedPayloadIDs
	savedInsert, savedThreadID := dbInsertProcessor, dbSetProcessorThreadID
	savedFinish, savedState := dbFinishProcessor, dbSetPayloadState

	dbGetPayload = func(ctx context.Context, id int64) (dbgen.Tpayload, error) {
		if err, ok := s.loadErr[id]; ok {
			return dbgen.Tpayload{}, err
		}
		row, ok := s.rows[id]
		if !ok {
			return dbgen.Tpayload{}, errors.New("stub: no such payload")
		}
		return row, nil
	}
	dbUnassignedPayloadIDs = func(ctx context.Context) ([]int64, error) {
		return s.unassigned, s.pollErr
	}
	dbInsertProcessor = func(ctx context.Context, arg dbgen.InsertProcessorParams) (int64, error) {
		if s.insertErr != nil {
			return 0, s.insertErr
		}
		s.inserts = append(s.inserts, arg)
		if id, ok := s.processor[int64(arg.PayloadID)]; ok {
			return id, nil
		}
		return int64(len(s.inserts)) * 1000, nil
	}
	dbSetProcessorThreadID = func(ctx context.Context, arg dbgen.SetProcessorThreadIdParams) error {
		s.threadIDs = append(s.threadIDs, arg)
		return nil
	}
	dbFinishProcessor = func(ctx context.Context, id int64) error {
		s.finishes = append(s.finishes, id)
		return nil
	}
	dbSetPayloadState = func(ctx context.Context, arg dbgen.SetPayloadStateParams) error {
		s.states = append(s.states, arg)
		return nil
	}

	t.Cleanup(func() {
		dbGetPayload, dbUnassignedPayloadIDs = savedGet, savedUnassigned
		dbInsertProcessor, dbSetProcessorThreadID = savedInsert, savedThreadID
		dbFinishProcessor, dbSetPayloadState = savedFinish, savedState
	})
	return s
}

// resetQueue empties the in-memory queue and the channel so each test starts
// from the state a freshly started process is in.
func resetQueue(t *testing.T) {
	t.Helper()
	jobQueueLock.Lock()
	jobQueue = map[int64]*JobQueueItem{}
	jobQueueLock.Unlock()
	for {
		select {
		case <-jobQueueChannel:
			continue
		default:
		}
		break
	}
}

// queuedItem looks the item up the way the worker does.
func queuedItem(t *testing.T, jobID int64) *JobQueueItem {
	t.Helper()
	jobQueueLock.RLock()
	defer jobQueueLock.RUnlock()
	return jobQueue[jobID]
}

func queuedCount() int {
	jobQueueLock.RLock()
	defer jobQueueLock.RUnlock()
	return len(jobQueue)
}

// TestEnqueuePayloadRegistersDispatchesAndAttachesIdentity is the test the
// missing trigger needed: one call must build the item with a usable lock,
// register it in jobQueue, send a COPY on jobQueueChannel that agrees with the
// map entry, journal the tProcessor row, and carry the database identity the
// storefile transports need.
func TestEnqueuePayloadRegistersDispatchesAndAttachesIdentity(t *testing.T) {
	resetQueue(t)
	stubs := (&queueStubs{
		rows:      map[int64]dbgen.Tpayload{42: testPayloadRow(42)},
		processor: map[int64]int64{42: 7700},
	}).install(t)

	jobID, err := EnqueuePayload(context.Background(), 42)
	if err != nil {
		t.Fatalf("EnqueuePayload(payload 42) = %v; want a job id", err)
	}

	// 1. Registered in the map the worker looks up by ID.
	item := queuedItem(t, jobID)
	if item == nil {
		t.Fatalf("jobQueue has no entry for job %d: the worker reads jobQueue[work.ID] and would nil-dereference", jobID)
	}
	if item.PayloadID != 42 {
		t.Errorf("item.PayloadID = %d; want 42", item.PayloadID)
	}
	if item.ProcessorID != 7700 {
		t.Errorf("item.ProcessorID = %d; want the journaled tProcessor id 7700", item.ProcessorID)
	}
	if item.Status != jobStatusMap[JobStatusQueued] {
		t.Errorf("item.Status = %q; want %q", item.Status, jobStatusMap[JobStatusQueued])
	}
	if string(item.Payload) != "<x12>the payload</x12>" {
		t.Errorf("item.Payload = %q; want the row's payload bytes", item.Payload)
	}
	if item.User != "Administrator" || item.RenderPlugin != "org.remitt.plugin.render.PreRenderedPlugin" ||
		item.RenderOption != "x12" || item.TransportPlugin != "org.remitt.plugin.transport.StoreFile" {
		t.Errorf("item does not carry the row: %+v", item)
	}

	// 2. The lock is initialised: AppendLog takes it, and a nil pointer here is
	// the panic that used to wait for the first job's first log line.
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("item.AppendLog panicked: the item was built without lock: new(sync.RWMutex) (%v)", r)
			}
		}()
		item.AppendLog("enqueued")
	}()

	// 3. A COPY went on the channel, and it agrees with the map entry.
	select {
	case copy := <-jobQueueChannel:
		if copy.ID != item.ID {
			t.Errorf("channel copy ID = %d; want the registered job %d", copy.ID, item.ID)
		}
		if copy.PayloadID != 42 || copy.ProcessorID != 7700 {
			t.Errorf("channel copy identity = (payload %d, processor %d); want (42, 7700) to match the map entry",
				copy.PayloadID, copy.ProcessorID)
		}
		if copy.User != item.User {
			t.Errorf("channel copy user = %q; want %q", copy.User, item.User)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("nothing arrived on jobQueueChannel: the dispatcher would never see the job")
	}

	// 4. The identity the plugins read out of the context.
	ctx := attachJobIdentity(context.Background(), item)
	identity, ok := common.JobIdentityFromContext(ctx)
	if !ok {
		t.Fatal("the work context carries no job identity: tFileStore's payloadId/processorId foreign keys cannot be satisfied")
	}
	if identity.PayloadID != 42 {
		t.Errorf("identity.PayloadID = %d; want 42", identity.PayloadID)
	}
	if identity.ProcessorID != 7700 {
		t.Errorf("identity.ProcessorID = %d; want 7700", identity.ProcessorID)
	}
	if identity.JobID != jobID {
		t.Errorf("identity.JobID = %d; want %d", identity.JobID, jobID)
	}

	// 5. The journal row: one tProcessor insert for this payload, stage render,
	// carrying the render plugin and the input the Java's
	// migratePayloadToProcessor wrote.
	if len(stubs.inserts) != 1 {
		t.Fatalf("tProcessor inserts = %d; want 1", len(stubs.inserts))
	}
	ins := stubs.inserts[0]
	if ins.PayloadID != 42 {
		t.Errorf("journaled payloadId = %d; want 42", ins.PayloadID)
	}
	if !ins.Stage.Valid || ins.Stage.String != processorStageRender {
		t.Errorf("journaled stage = %+v; want %q", ins.Stage, processorStageRender)
	}
	if ins.Plugin != "org.remitt.plugin.render.PreRenderedPlugin" {
		t.Errorf("journaled plugin = %q; want the payload's render plugin", ins.Plugin)
	}
	if ins.ThreadID != 0 {
		t.Errorf("journaled threadId = %d; want 0 (no worker has taken the job yet)", ins.ThreadID)
	}
	if !ins.PInput.Valid || ins.PInput.String != "<x12>the payload</x12>" {
		t.Errorf("journaled pInput = %+v; want the payload bytes", ins.PInput)
	}
}

// TestEnqueuePayloadSkipsPayloadAlreadyInFlight is the in-flight skip both
// triggers rely on: whatever queued the payload first, the second attempt must
// not journal a second tProcessor row and must not dispatch a second job.
func TestEnqueuePayloadSkipsPayloadAlreadyInFlight(t *testing.T) {
	resetQueue(t)
	stubs := (&queueStubs{
		rows:      map[int64]dbgen.Tpayload{42: testPayloadRow(42)},
		processor: map[int64]int64{42: 7700},
	}).install(t)

	first, err := EnqueuePayload(context.Background(), 42)
	if err != nil {
		t.Fatalf("first EnqueuePayload(42) = %v; want success", err)
	}

	second, err := EnqueuePayload(context.Background(), 42)
	if !errors.Is(err, ErrPayloadInFlight) {
		t.Fatalf("second EnqueuePayload(42) = (%d, %v); want ErrPayloadInFlight", second, err)
	}
	if second != 0 {
		t.Errorf("second enqueue returned job id %d; want 0 for a skipped payload", second)
	}

	if n := queuedCount(); n != 1 {
		t.Errorf("jobQueue holds %d items; want 1 (the payload is already queued)", n)
	}
	if len(stubs.inserts) != 1 {
		t.Errorf("tProcessor inserts = %d; want 1 - a second row would let the payload be processed twice", len(stubs.inserts))
	}
	if got := queuedItem(t, first); got == nil || got.ProcessorID != 7700 {
		t.Errorf("the already queued job was disturbed: %+v", got)
	}

	// Exactly one item on the channel: the skip did not dispatch anything.
	select {
	case copy := <-jobQueueChannel:
		if copy.ID != first {
			t.Errorf("channel carried job %d; want only the first job %d", copy.ID, first)
		}
	default:
		t.Error("the first enqueue left nothing on jobQueueChannel")
	}
	select {
	case copy := <-jobQueueChannel:
		t.Errorf("a second copy (job %d) reached the channel: the in-flight payload was dispatched twice", copy.ID)
	default:
	}
}

// TestEnqueuePayloadTreatsARunningJobAsInFlight pins the other half of "in
// flight": a job a worker has already started is not re-enqueued either.
func TestEnqueuePayloadTreatsARunningJobAsInFlight(t *testing.T) {
	resetQueue(t)
	(&queueStubs{
		rows:      map[int64]dbgen.Tpayload{42: testPayloadRow(42)},
		processor: map[int64]int64{42: 7700},
	}).install(t)

	jobID, err := EnqueuePayload(context.Background(), 42)
	if err != nil {
		t.Fatalf("EnqueuePayload(42) = %v; want success", err)
	}
	item := queuedItem(t, jobID)
	item.Lock()
	item.Status = jobStatusMap[JobStatusRunning]
	item.Unlock()

	if _, err := EnqueuePayload(context.Background(), 42); !errors.Is(err, ErrPayloadInFlight) {
		t.Fatalf("EnqueuePayload(42) with the job RUNNING = %v; want ErrPayloadInFlight", err)
	}
	if n := queuedCount(); n != 1 {
		t.Errorf("jobQueue holds %d items; want 1", n)
	}
}

// TestEnqueuePayloadJournalFailureUndoesTheRegistration: if the tProcessor row
// cannot be written there is no job, and the payload must stay eligible for a
// later poll - so the queue entry has to go away again.
func TestEnqueuePayloadJournalFailureUndoesTheRegistration(t *testing.T) {
	resetQueue(t)
	(&queueStubs{
		rows:      map[int64]dbgen.Tpayload{42: testPayloadRow(42)},
		insertErr: errors.New("stub: tProcessor insert failed"),
	}).install(t)

	if _, err := EnqueuePayload(context.Background(), 42); err == nil {
		t.Fatal("EnqueuePayload(42) succeeded although journaling the tProcessor row failed")
	}
	if n := queuedCount(); n != 0 {
		t.Errorf("jobQueue holds %d items after a failed journal; want 0 so the payload can be polled again", n)
	}
	select {
	case copy := <-jobQueueChannel:
		t.Errorf("job %d reached the channel although its journal failed", copy.ID)
	default:
	}
}

// TestEnqueuePayloadRejectsAnUnloadablePayload: the queue reports what went
// wrong and leaves the queue alone.
func TestEnqueuePayloadRejectsAnUnloadablePayload(t *testing.T) {
	resetQueue(t)
	(&queueStubs{
		loadErr: map[int64]error{7: errors.New("stub: no such payload")},
	}).install(t)

	if _, err := EnqueuePayload(context.Background(), 7); err == nil {
		t.Fatal("EnqueuePayload(7) succeeded although the payload could not be loaded")
	}
	if _, err := EnqueuePayload(context.Background(), 0); err == nil {
		t.Fatal("EnqueuePayload(0) succeeded; a payload id of 0 is not a row")
	}
	if n := queuedCount(); n != 0 {
		t.Errorf("jobQueue holds %d items; want 0", n)
	}
}

// TestNoDatabaseIsAnErrorNotAPanic: every seam reports a missing database
// instead of dereferencing a nil *dbgen.Queries. The live server initialises the
// database before the dispatcher starts, but a poller that lost its database
// must log, not panic the process.
func TestNoDatabaseIsAnErrorNotAPanic(t *testing.T) {
	resetQueue(t)
	if _, err := EnqueuePayload(context.Background(), 42); !errors.Is(err, errNoDatabase) {
		t.Errorf("EnqueuePayload with no database = %v; want errNoDatabase", err)
	}
	if _, err := pollOnce(context.Background()); !errors.Is(err, errNoDatabase) {
		t.Errorf("pollOnce with no database = %v; want errNoDatabase", err)
	}
}

// TestFinishAndFailJournalTheTerminalState covers how a payload's state moves:
// success stamps the tProcessor row's tsEnd and marks the payload 'completed',
// failure does the same and marks it 'failed' - either way 'valid' is gone, so
// the poll query will not pick the payload up again.
func TestFinishAndFailJournalTheTerminalState(t *testing.T) {
	resetQueue(t)
	stubs := (&queueStubs{}).install(t)

	done := &JobQueueItem{ID: 1, PayloadID: 42, ProcessorID: 7700, lock: new(sync.RWMutex)}
	done.Finish()
	if done.Status != jobStatusMap[JobStatusSuccess] {
		t.Errorf("Finish(): status = %q; want %q", done.Status, jobStatusMap[JobStatusSuccess])
	}

	failed := &JobQueueItem{ID: 2, PayloadID: 43, ProcessorID: 7701, lock: new(sync.RWMutex)}
	failed.Fail(errors.New("boom"))

	if len(stubs.finishes) != 2 {
		t.Fatalf("tProcessor tsEnd updates = %d; want 2 (one per job)", len(stubs.finishes))
	}
	if stubs.finishes[0] != 7700 || stubs.finishes[1] != 7701 {
		t.Errorf("tsEnd stamped on processors %v; want [7700 7701]", stubs.finishes)
	}
	if len(stubs.states) != 2 {
		t.Fatalf("tPayload state updates = %d; want 2", len(stubs.states))
	}
	if got := stubs.states[0]; got.ID != 42 || got.PayloadState.String != payloadStateCompleted {
		t.Errorf("after Finish: payload %d -> %+v; want payload 42 -> %q", got.ID, got.PayloadState, payloadStateCompleted)
	}
	if got := stubs.states[1]; got.ID != 43 || got.PayloadState.String != payloadStateFailed {
		t.Errorf("after Fail: payload %d -> %+v; want payload 43 -> %q", got.ID, got.PayloadState, payloadStateFailed)
	}

	// A hand-built item with no identity (the live driver's own items) must not
	// touch the database at all.
	stubs.finishes, stubs.states = nil, nil
	(&JobQueueItem{ID: 3, lock: new(sync.RWMutex)}).Finish()
	if len(stubs.finishes) != 0 || len(stubs.states) != 0 {
		t.Errorf("an item with no database identity journaled anyway: finishes=%v states=%v", stubs.finishes, stubs.states)
	}
}

// TestJournalThreadRecordsTheWorker: the worker's "// Journal update" comment
// becomes a real write of its id onto the journaled row.
func TestJournalThreadRecordsTheWorker(t *testing.T) {
	resetQueue(t)
	stubs := (&queueStubs{}).install(t)

	item := &JobQueueItem{ID: 1, PayloadID: 42, ProcessorID: 7700, lock: new(sync.RWMutex)}
	item.journalThread(2)

	if len(stubs.threadIDs) != 1 {
		t.Fatalf("threadId writes = %d; want 1", len(stubs.threadIDs))
	}
	if got := stubs.threadIDs[0]; got.ID != 7700 || got.ThreadID != 2 {
		t.Errorf("wrote threadId %d to processor %d; want threadId 2 to processor 7700", got.ThreadID, got.ID)
	}
}

// TestJobIdentityIsZeroWhenUnknown: a job with no payload/processor id must
// present zeroes (a miss for the consumers) rather than a wrapped negative.
func TestJobIdentityIsZeroWhenUnknown(t *testing.T) {
	ctx := attachJobIdentity(context.Background(), &JobQueueItem{ID: 9, PayloadID: -1, ProcessorID: -1, lock: new(sync.RWMutex)})
	identity, ok := common.JobIdentityFromContext(ctx)
	if !ok {
		t.Fatal("no identity attached")
	}
	if identity.PayloadID != 0 || identity.ProcessorID != 0 {
		t.Errorf("identity = %+v; want zeroed ids for an unknown payload/processor", identity)
	}
	if identity.JobID != 9 {
		t.Errorf("identity.JobID = %d; want 9", identity.JobID)
	}
}
