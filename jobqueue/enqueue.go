package jobqueue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
)

// ErrPayloadInFlight is returned by EnqueuePayload when the payload already has
// an unfinished job in the queue. Both triggers use it: the poller treats it as
// "nothing to do, someone else has this one", and a caller that enqueues
// explicitly can tell it apart from a real failure.
var ErrPayloadInFlight = errors.New("jobqueue: payload already in flight in the job queue")

// errNoDatabase is what every database seam below reports when the process has
// no database (model.Queries is only set by model.InitDb). It is an error, not
// a panic: a poller running without a database must log and keep going, and a
// unit test must be able to exercise the queue's own logic with no MySQL.
var errNoDatabase = errors.New("jobqueue: database is not initialised")

// stage renders: the Java's remitt.control.initialStep default
// (ControlThread.java:596-607 falls back to ThreadType.RENDER) and the stage
// the journaled tProcessor row carries.
const (
	processorStageRender = "render"

	// journalTimeout bounds every journaling statement. The worker holds the
	// job's own lock while the terminal journal runs, so a database that never
	// answers must not pin a worker (or the caller of Finish/Fail) forever.
	journalTimeout = 10 * time.Second

	// payloadStateValid / payloadStateCompleted / payloadStateFailed are
	// tPayload.payloadState's three enum values (migrations/001_legacy.up.sql:122).
	payloadStateValid     = "valid"
	payloadStateCompleted = "completed"
	payloadStateFailed    = "failed"
)

// The database seams the queue uses. They are package variables so a unit test
// can replace them with a stub: there is no MySQL in a unit test, and the
// alternative - a queue that can only be tested against a live database - is
// what left the feeder unwritten. Every one of them checks model.Queries first,
// so an uninitialised process reports errNoDatabase instead of a nil panic.
var (
	// dbGetPayload loads the tPayload row a job will run.
	dbGetPayload = func(ctx context.Context, id int64) (dbgen.Tpayload, error) {
		if model.Queries == nil {
			return dbgen.Tpayload{}, errNoDatabase
		}
		return model.Queries.GetPayloadById(ctx, id)
	}

	// dbUnassignedPayloadIDs is the poll query: the payloads the database says
	// are valid and not yet journaled in tProcessor.
	dbUnassignedPayloadIDs = func(ctx context.Context) ([]int64, error) {
		if model.Queries == nil {
			return nil, errNoDatabase
		}
		return model.Queries.GetUnassignedPayloadIds(ctx)
	}

	// dbInsertProcessor journals the job into tProcessor, which is what makes
	// the same payload unpollable twice, and returns the new row's id.
	dbInsertProcessor = func(ctx context.Context, arg dbgen.InsertProcessorParams) (int64, error) {
		if model.Queries == nil {
			return 0, errNoDatabase
		}
		res, err := model.Queries.InsertProcessor(ctx, arg)
		if err != nil {
			return 0, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return 0, err
		}
		return id, nil
	}

	// dbSetProcessorThreadID records which worker took the job.
	dbSetProcessorThreadID = func(ctx context.Context, arg dbgen.SetProcessorThreadIdParams) error {
		if model.Queries == nil {
			return errNoDatabase
		}
		return model.Queries.SetProcessorThreadId(ctx, arg)
	}

	// dbFinishProcessor stamps the job's tsEnd.
	dbFinishProcessor = func(ctx context.Context, id int64) error {
		if model.Queries == nil {
			return errNoDatabase
		}
		return model.Queries.FinishProcessor(ctx, id)
	}

	// dbSetPayloadState moves tPayload.payloadState to its terminal value.
	dbSetPayloadState = func(ctx context.Context, arg dbgen.SetPayloadStateParams) error {
		if model.Queries == nil {
			return errNoDatabase
		}
		return model.Queries.SetPayloadState(ctx, arg)
	}
)

// EnqueuePayload is the ONE way a payload enters the worker pool, and the two
// triggers the pipeline has both go through it:
//
//  1. api.PayloadInsert enqueues the row it just stored, so a submission starts
//     immediately instead of waiting for the next poll.
//  2. the poller (poller.go) enqueues every payload the database still calls
//     valid which no tProcessor row has claimed, so a row inserted by any other
//     path - or left waiting by a restart - is still processed.
//
// It does all four things a job needs before a worker can run it, in this
// order, and undoes the registration if journaling fails:
//
//   - builds the item with lock: new(sync.RWMutex) (a nil lock panics in
//     AppendLog/Fail, see fail_lock_test.go);
//   - registers it in jobQueue under jobQueueLock, refusing a payload that is
//     already in flight;
//   - journals the tProcessor row (InsertProcessor) and keeps its id on the
//     item, so the same payload cannot be polled twice and the storefile
//     transports can satisfy tFileStore's foreign keys;
//   - sends a COPY on jobQueueChannel, which is what a worker receives. The
//     worker looks the item up by ID in jobQueue and takes its lock with no nil
//     check, so the copy and the map entry must agree - they carry the same
//     PayloadID, the same ProcessorID and the same lock pointer.
//
// The job identity is attached to the work context by attachJobIdentity, from
// the ids the item carries.
func EnqueuePayload(ctx context.Context, payloadID int64) (int64, error) {
	if payloadID <= 0 {
		return 0, fmt.Errorf("jobqueue: enqueue: invalid payload id %d", payloadID)
	}
	row, err := dbGetPayload(ctx, payloadID)
	if err != nil {
		return 0, fmt.Errorf("jobqueue: enqueue payload %d: load: %w", payloadID, err)
	}
	return enqueueRow(ctx, row)
}

// enqueueRow is the shared body of EnqueuePayload: the row is loaded, the job
// is journaled, the item is registered and dispatched.
func enqueueRow(ctx context.Context, row dbgen.Tpayload) (int64, error) {
	jobID := atomic.AddInt64(&jobQueueId, 1)
	item := newJobQueueItem(jobID, row)

	// Reserve the slot BEFORE talking to the database. Two triggers can race -
	// the insert path and the poller both seeing the same 'valid' row - and the
	// reservation is what keeps the loser out: it never journals a second
	// tProcessor row and never dispatches a second job for the payload.
	jobQueueLock.Lock()
	if payloadJobInFlightLocked(row.ID) {
		jobQueueLock.Unlock()
		return 0, fmt.Errorf("%w: payload %d", ErrPayloadInFlight, row.ID)
	}
	jobQueue[jobID] = item
	jobQueueLock.Unlock()

	processorID, err := dbInsertProcessor(ctx, dbgen.InsertProcessorParams{
		ThreadID:  0,
		PayloadID: uint64(row.ID),
		Stage:     sql.NullString{String: processorStageRender, Valid: true},
		Plugin:    row.Renderplugin,
		PInput:    row.Payload,
	})
	if err != nil {
		// With no journal row there is nothing to poll against, so drop the
		// reservation: the payload stays 'valid' and a later poll can retry it.
		jobQueueLock.Lock()
		delete(jobQueue, jobID)
		jobQueueLock.Unlock()
		return 0, fmt.Errorf("jobqueue: enqueue payload %d: journal tProcessor: %w", row.ID, err)
	}
	item.ProcessorID = processorID

	// Registered first, then sent: the worker dereferences what it looks up.
	// The send publishes the item (and its identity) to the dispatcher.
	jobQueueChannel <- *item

	log.Printf("jobqueue: enqueued payload %d as job %d (tProcessor id %d, plugin %s)",
		row.ID, jobID, processorID, row.Renderplugin)
	return jobID, nil
}

// newJobQueueItem builds the item a worker will run for one tPayload row.
//
// The per-item lock is a pointer that nothing else initialises, and it is the
// first thing AppendLog, Fail, Finish and Cancel touch - a job built without it
// panics with a nil dereference instead of failing.
func newJobQueueItem(jobID int64, row dbgen.Tpayload) *JobQueueItem {
	var payload []byte
	if row.Payload.Valid {
		payload = []byte(row.Payload.String)
	}
	return &JobQueueItem{
		ID:              jobID,
		Status:          jobStatusMap[JobStatusQueued],
		Enqueued:        time.Now(),
		Action:          "payload",
		User:            row.User,
		Payload:         payload,
		RenderPlugin:    row.Renderplugin,
		RenderOption:    row.Renderoption,
		TransportPlugin: row.Transportplugin,
		TransportOption: row.Transportoption.String,
		OriginalID:      row.Originalid.String,
		// The database identity of the work, carried so executeJob can attach
		// it to the context and tFileStore's two NOT NULL foreign keys can be
		// satisfied (common/jobcontext.go).
		PayloadID:   row.ID,
		ProcessorID: 0, // filled in once the tProcessor row exists
		lock:        new(sync.RWMutex),
	}
}

// payloadJobInFlight reports whether the queue already holds an unfinished job
// for payloadID. Callers that hold jobQueueLock (either mode) use the Locked
// form directly.
func payloadJobInFlight(payloadID int64) bool {
	jobQueueLock.RLock()
	defer jobQueueLock.RUnlock()
	return payloadJobInFlightLocked(payloadID)
}

// payloadJobInFlightLocked must be called with jobQueueLock held. Only an
// unfinished job counts: a QUEUED or RUNNING item is work someone is doing, so
// a second trigger for that payload is a duplicate. A terminal item is not in
// flight any more.
//
// The item's Status is read under the item's OWN lock: the worker writes it
// there, and reading it bare would be a data race. Lock order is always
// jobQueueLock -> JobQueueItem.lock, and no path takes them the other way
// round.
func payloadJobInFlightLocked(payloadID int64) bool {
	for _, item := range jobQueue {
		if item.PayloadID != payloadID {
			continue
		}
		item.ReadLock()
		status := item.Status
		item.ReadUnlock()
		if status == jobStatusMap[JobStatusQueued] || status == jobStatusMap[JobStatusRunning] {
			return true
		}
	}
	return false
}

// attachJobIdentity puts the job's database identity on the work context the
// pipeline's plugins are handed.
//
// Both tFileStore foreign keys - payloadId -> tPayload(id) and
// processorId -> tProcessor(id) - are NOT NULL and no parent row can have id 0,
// so the plugin that writes the row cannot invent either id: they have to come
// from the code that journaled the job, which is this package. That is what
// common.NewJobContext is for (common/jobcontext.go), and a plugin handed no
// identity fails loudly instead of writing an invalid row.
//
// A payload or processor id that is unknown or negative is reported as 0, which
// every consumer treats as "not known" rather than as a key.
func attachJobIdentity(ctx context.Context, w *JobQueueItem) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return common.NewJobContext(ctx, common.JobIdentity{
		PayloadID:   toUint64(w.PayloadID),
		ProcessorID: toUint64(w.ProcessorID),
		JobID:       w.ID,
	})
}

func toUint64(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// journalThread records which worker is running the job on the tProcessor row.
// It is the "// Journal update" the worker used to leave as a comment; with no
// processor row (a hand-built item in a test) there is nothing to update.
func (o *JobQueueItem) journalThread(threadID int) {
	if o.ProcessorID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), journalTimeout)
	defer cancel()
	if err := dbSetProcessorThreadID(ctx, dbgen.SetProcessorThreadIdParams{
		ThreadID: uint32(threadID),
		ID:       o.ProcessorID,
	}); err != nil {
		log.Printf("jobqueue: job %d: journal tProcessor.threadId (processor %d): %s",
			o.ID, o.ProcessorID, err.Error())
	}
}

// journalTerminal closes the job in the database: the tProcessor row gets its
// tsEnd, and the payload moves to state, its terminal payloadState. 'valid'
// plus a tProcessor row is what the poll query looks for, so this is also what
// stops a finished payload from being polled again.
//
// It is called with the job's lock already held (from Finish and Fail), so it
// must not take it again.
func (o *JobQueueItem) journalTerminal(state string) {
	if o.ProcessorID != 0 {
		ctx, cancel := context.WithTimeout(context.Background(), journalTimeout)
		if err := dbFinishProcessor(ctx, o.ProcessorID); err != nil {
			log.Printf("jobqueue: job %d: journal tProcessor.tsEnd (processor %d): %s",
				o.ID, o.ProcessorID, err.Error())
		}
		cancel()
	}
	if o.PayloadID == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), journalTimeout)
	defer cancel()
	if err := dbSetPayloadState(ctx, dbgen.SetPayloadStateParams{
		PayloadState: sql.NullString{String: state, Valid: true},
		ID:           o.PayloadID,
	}); err != nil {
		log.Printf("jobqueue: job %d: journal tPayload.payloadState=%s (payload %d): %s",
			o.ID, state, o.PayloadID, err.Error())
	}
}
