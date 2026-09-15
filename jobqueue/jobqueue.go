package jobqueue

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/freemed/remitt-server/callback"
	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
	"github.com/freemed/remitt-server/render"
	"github.com/freemed/remitt-server/translation"
	"github.com/freemed/remitt-server/transport"
)

const (
	JobStatusQueued  = 1
	JobStatusRunning = 2
	JobStatusFailed  = 3
	JobStatusSuccess = 4
	JobStatusCancel  = 5
)

var (
	jobQueueLock *sync.RWMutex
	jobQueueId   int64
	jobQueue     map[int64]*JobQueueItem
	jobStatusMap = map[int64]string{
		JobStatusQueued:  "QUEUED",
		JobStatusRunning: "RUNNING",
		JobStatusFailed:  "FAILED",
		JobStatusSuccess: "SUCCESS",
		JobStatusCancel:  "CANCEL",
	}

	jobQueueChannel = make(chan JobQueueItem, 100)
	WorkerQueue     chan chan JobQueueItem
)

func init() {
	jobQueueLock = new(sync.RWMutex)
	jobQueue = map[int64]*JobQueueItem{}
}

// JobQueueItem represents an individual job status
type JobQueueItem struct {
	ID              int64          `json:"job_id"`
	Status          string         `json:"status"`
	Enqueued        time.Time      `json:"enqueued"`
	Started         model.NullTime `json:"started"`
	Completed       model.NullTime `json:"completed"`
	Message         string         `json:"message"`
	Log             []string       `json:"log"`
	Action          string         `json:"action"`
	User            string         `json:"user"`
	IP              string         `json:"ip"`
	Payload         []byte
	RenderPlugin    string
	RenderOption    string
	TransportPlugin string
	TransportOption string
	OriginalID      string

	// PayloadID and ProcessorID are the job's DATABASE identity: tPayload.id
	// and the tProcessor.id the job was journaled under (the Java plugin
	// interface called that the jobId). They are set by the enqueue path
	// (enqueue.go) and travel with the item so executeJob can attach them to
	// the work context - tFileStore has a NOT NULL foreign key to each, so a
	// transport that persists its output cannot write a row without them
	// (common/jobcontext.go). JobID is w.ID.
	PayloadID   int64
	ProcessorID int64

	lock *sync.RWMutex
}

func (o *JobQueueItem) ReadLock() {
	o.lock.RLock()
}

func (o *JobQueueItem) Lock() {
	o.lock.Lock()
}

func (o *JobQueueItem) ReadUnlock() {
	o.lock.RUnlock()
}

func (o *JobQueueItem) Unlock() {
	o.lock.Unlock()
}

// AppendLog records a log line and updates the message. It takes the job's lock.
func (o *JobQueueItem) AppendLog(item string) {
	o.lock.Lock()
	defer o.lock.Unlock()
	o.appendLogLocked(item)
}

// appendLogLocked does AppendLog's work for callers that ALREADY hold the lock.
// It exists because Fail holds the write lock and used to call the exported
// AppendLog, which takes the same non-reentrant sync.RWMutex: marking a job
// FAILED deadlocked the caller while holding the job's own lock. Every failure
// path in executeJob returns through Fail (including the worker's own i.Fail),
// so a failing job HUNG instead of failing - which is precisely the behaviour a
// fix that must "fail the job and name the option" depends on.
func (o *JobQueueItem) appendLogLocked(item string) {
	log.Printf("JobQueue status %d | %s", o.ID, item)
	if o.Log == nil {
		o.Log = make([]string, 0)
	}
	o.Log = append(o.Log, strconv.FormatInt(time.Now().Unix(), 10)+"|"+item)

	// Additionally set message to "last log item" automatically without timestamp
	o.Message = item

	// No database write here: this is called for every line, from inside the
	// job's own lock, and the previous "Journal update (sqlc migration pending)"
	// comment left the impression that something was persisted. The journal is
	// written at the two points that mean something in the database: the worker
	// stamping tProcessor.threadId when it takes the job (journalThread) and
	// the terminal state in Finish/Fail (journalTerminal).
}

func (o *JobQueueItem) IsCancelled() bool {
	return o.Status == jobStatusMap[JobStatusCancel]
}

func (o *JobQueueItem) Cancel() {
	o.lock.Lock()
	o.Status = jobStatusMap[JobStatusCancel]
	o.Completed = model.NullTimeNow()
	o.lock.Unlock()
}

func (o *JobQueueItem) Finish() {
	o.lock.Lock()
	o.Status = jobStatusMap[JobStatusSuccess]
	o.Completed = model.NullTimeNow()
	o.journalTerminal(payloadStateCompleted)
	o.lock.Unlock()
}

func (o *JobQueueItem) Fail(err error) {
	o.lock.Lock()
	defer o.lock.Unlock()
	o.Status = jobStatusMap[JobStatusFailed]
	o.Completed = model.NullTimeNow()
	o.appendLogLocked(err.Error())
	o.Message = err.Error()
	o.journalTerminal(payloadStateFailed)
}

func (o *JobQueueItem) Render() (out []byte, err error) {
	// Create temporary
	inxml, err := os.CreateTemp(config.Config.Paths.TemporaryPath, "render-in")
	if err != nil {
		log.Printf("Render(): %s", err.Error())
		return
	}
	defer os.Remove(inxml.Name())
	_, err = inxml.Write(o.Payload)
	if err != nil {
		log.Printf("Render(): %s", err.Error())
		return
	}

	outxml, err := os.CreateTemp(config.Config.Paths.TemporaryPath, "render-out")
	if err != nil {
		log.Printf("Render(): %s", err.Error())
		return
	}
	//defer os.Remove(outxml.Name())

	xslfile := config.Config.Paths.BasePath + string(os.PathSeparator) + "resources" + string(os.PathSeparator) + "xsl" + string(os.PathSeparator) + o.RenderOption + ".xsl"

	// Honor the configured engine: common.XslTransform dispatches to the
	// in-process engine by default and falls back to xsltproc if that errors.
	//
	// NOTE: this method is NOT on the live pipeline path. executeJob renders
	// through the render plugin (render/xslt.go), which calls the same
	// dispatcher. This method is kept correct anyway so it is not a trap: it
	// previously called the external binary unconditionally (the internal branch
	// was commented out, so the internal-xslt setting did nothing here) and then
	// overwrote its transform error with the following ReadFile error, returning
	// an empty payload with a NIL error.
	if err = common.XslTransform(inxml.Name(), xslfile, outxml.Name(), map[string]string{}); err != nil {
		log.Printf("Render(): %s", err.Error())
		outxml.Close()
		os.Remove(outxml.Name())
		return
	}

	// Bring data back in by reading again
	outxml.Close()
	out, err = os.ReadFile(outxml.Name())
	os.Remove(outxml.Name())
	return
}

// NewWorker creates, and returns a new Worker object. Its only argument
// is a channel that the worker can add itself to whenever it is done its
// work.
func NewWorker(id int, workerQueue chan chan JobQueueItem) Worker {
	// Create, and return the worker.
	worker := Worker{
		ID:          id,
		Work:        make(chan JobQueueItem),
		WorkerQueue: workerQueue,
		QuitChan:    make(chan bool)}

	return worker
}

// Worker represents the individual job worker status
type Worker struct {
	ID          int
	Work        chan JobQueueItem
	WorkerQueue chan chan JobQueueItem
	QuitChan    chan bool
}

// Start "starts" the worker by starting a goroutine, that is
// an infinite "for-select" loop.
func (w Worker) Start() {
	go func() {
		for {
			// Add ourselves into the worker queue.
			w.WorkerQueue <- w.Work

			select {
			case work := <-w.Work:
				// Receive a work request.
				log.Printf("worker[%d]: Received work request id == %d, action == %s", w.ID, work.ID, work.Action)

				// Pull from jobStatusMap
				jobQueueLock.RLock()
				i := jobQueue[work.ID]
				jobQueueLock.RUnlock()

				// Mark as PROCESSING
				i.Lock()
				i.Status = jobStatusMap[JobStatusRunning]
				i.Started = model.NullTimeNow()
				// Journal which worker took the job, on the tProcessor row the
				// enqueue path already wrote. The worker's ID is what the Java
				// stored in tProcessor.threadId (ControlThread.java:230-272).
				i.journalThread(w.ID)
				i.Unlock()

				// Actually process queue item
				err := processJobQueueItem(i)
				if err != nil {
					i.Fail(err)
				} else {
					i.Finish()
				}

			case <-w.QuitChan:
				// We have been asked to stop.
				fmt.Printf("worker[%d] stopping\n", w.ID)
				return
			}
		}
	}()
}

// Stop tells the worker to stop listening for work requests.
//
// Note that the worker will only stop *after* it has finished its work.
func (w Worker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}

// usableWorkerCount clamps a configured worker count to something the dispatcher
// can actually run. A non-positive count used to start NO workers at all: the
// dispatcher still read jobQueueChannel, then blocked forever on
// `worker := <-WorkerQueue` waiting for a worker that could never exist, so every
// payload was accepted, journaled into tProcessor, and never processed, with
// nothing logged as an error (observed 2026-09-15: a payload sat at 'valid' with
// threadId 0 for as long as the process lived).
func usableWorkerCount(n int) int {
	if n < 1 {
		log.Printf("StartDispatcher(): worker count %d is not usable; starting 1 worker "+
			"(set timing-iterations.worker-threads to choose the pool size)", n)
		return 1
	}
	return n
}

// StartDispatcher initializes the jobqueue dispatcher with nworker workers
func StartDispatcher(nworkers int) {
	nworkers = usableWorkerCount(nworkers)

	// First, initialize the channel we are going to but the workers' work channels into.
	WorkerQueue = make(chan chan JobQueueItem, nworkers)

	// Now, create all of our workers.
	for i := 0; i < nworkers; i++ {
		log.Printf("StartDispatcher(): Starting worker %d", i+1)
		worker := NewWorker(i+1, WorkerQueue)
		worker.Start()
	}

	// Start the poller alongside the workers: it is the safety net for payloads
	// the on-insert trigger never saw (another process wrote the row, the
	// insert path failed to enqueue, or the row was waiting across a restart).
	// Enqueue-on-insert and the poller are the two triggers this pipeline is
	// supposed to have, and both converge on EnqueuePayload.
	if PollEnabled() {
		log.Printf("StartDispatcher(): starting the payload poller (queue.poll-interval-ms=%d, %s)",
			pollIntervalMillis(), PollInterval())
		StartPoller(context.Background())
	} else {
		log.Print("StartDispatcher(): the payload poller is DISABLED (queue.poll-enabled=false): " +
			"only payloads inserted through the API will be processed")
	}

	go func() {
		for {
			select {
			case work := <-jobQueueChannel:
				log.Print("Dispatcher: Received work request")
				go func() {
					worker := <-WorkerQueue

					log.Print("Dispatcher: Dispatching work request")
					worker <- work
				}()
			}
		}
	}()
}

func processJobQueueItem(w *JobQueueItem) error {
	log.Printf("processJobQueueItem : %v", w)

	// TODO: Validate

	err := executeJob(w)
	return err
}

// executeJob performs the actual worker task
func executeJob(w *JobQueueItem) (err error) {
	tag := fmt.Sprintf("executeJob(%d): ", w.ID)

	u, err := model.GetUserByName(w.User)
	if err != nil {
		log.Printf("executeJob(): %s", err.Error())
		return fmt.Errorf("executejob: getuserbyname: %w", err)
	}
	ctx := user.NewContext(context.Background(), &u)

	// Attach the job's database identity (tPayload.id and the tProcessor.id it
	// was journaled under). Everything downstream - the transports that
	// persist their output, the translators, the callbacks - is handed THIS
	// context, and tFileStore's two NOT NULL foreign keys can only be
	// satisfied from it. A job enqueued with no identity (a hand-built item)
	// carries zeroes, which the consumers report as a miss rather than writing
	// an invalid row.
	ctx = attachJobIdentity(ctx, w)

	// Fire callback asynchronously on completion (success or failure).
	// Non-blocking goroutine so job processing is never delayed.
	defer func() {
		go fireCallback(&u, w, err)
	}()

	// Render
	renderPlugin, err := render.InstantiateRenderer(w.RenderPlugin)
	if err != nil {
		w.Fail(err)
		return fmt.Errorf("executejob: renderplugin: %w", err)
	}
	renderPlugin.SetContext(ctx)
	renderedData, err := renderPlugin.Render(w.Payload, w.RenderOption)
	if err != nil {
		w.Fail(err)
		return fmt.Errorf("executejob: render: %w", err)
	}

	// Instantiate transport plugin
	transportPlugin, err := transport.InstantiateTransporter(w.TransportPlugin)
	if err != nil {
		w.Fail(err)
		return fmt.Errorf("executejob: instantiatetransporter: %w", err)
	}
	// Pass context to transport plugin
	transportPlugin.SetContext(ctx)

	// Resolve translation plugin
	//
	// The database owns this decision (p_ResolveTranslationPlugin,
	// migrations/001_legacy.up.sql:327-338): the render plugin and option give
	// the format the render produces, the transport plugin and option give the
	// formats it accepts, and tTranslation names the translator between them.
	// ResolveTranslatorForJob does exactly that against the live tables and
	// falls back to the Go registries' own format strings only when the
	// database has no row for the pair. Resolving with the render OPTION and
	// the Go transport's InputFormat() - which is what this line used to do -
	// compares two vocabularies the database never stored, so no shipped
	// stylesheet ever resolved ('4010_837p' vs 'x12').
	translationPluginName, translationSource, err := translation.ResolveTranslatorForJob(
		w.RenderPlugin, w.RenderOption, w.TransportPlugin, w.TransportOption, transportPlugin.InputFormat())
	if err != nil {
		w.Fail(err)
		return fmt.Errorf("executejob: resolve translator: %w", err)
	}
	log.Printf(tag+"Resolved plugin %s (%s) for render %s/%s -> transport %s/%s",
		translationPluginName, translationSource, w.RenderPlugin, w.RenderOption, w.TransportPlugin, w.TransportOption)

	// Instantiate translation plugin
	translationPlugin, err := translation.InstantiateTranslator(translationPluginName)
	if err != nil {
		w.Fail(err)
		return fmt.Errorf("executejob: instantiatetranslator: %w", err)
	}
	// Pass context to translation plugin
	translationPlugin.SetContext(ctx)

	// Translation
	translatedData, err := translationPlugin.Translate(renderedData)
	if err != nil {
		w.Fail(err)
		return fmt.Errorf("executejob: translate: %w", err)
	}

	var ext string
	switch transportPlugin.InputFormat() {
	case "txt":
		ext = "txt"
	case "text":
		ext = "txt"
	case "x12":
		ext = "x12"
	case "pdf":
		ext = "pdf"
	default:
		ext = "txt"
	}
	fn := fmt.Sprintf("%d.%s", time.Now().UnixNano(), ext)
	log.Printf(tag+"Using filename %s", fn)

	// Transmission
	err = transportPlugin.Transport(fn, translatedData)
	if err != nil {
		w.Fail(err)
		return fmt.Errorf("executejob: transport: %w", err)
	}

	w.Finish()
	return nil
}

// fireCallback sends a job completion notification via the configured callback sender.
// This is always called in its own goroutine so it never blocks job processing.
func fireCallback(u *model.UserModel, w *JobQueueItem, jobErr error) {
	status := "SUCCESS"
	message := w.Message
	if jobErr != nil {
		status = "FAILED"
		message = jobErr.Error()
	}

	// The payload the job ran for. An enqueued job carries it directly
	// (JobQueueItem.PayloadID, set from the tPayload row); OriginalID is only a
	// fallback, for the callers that put the payload id there as a string
	// because nothing else carried it. Parsing OriginalID alone reported
	// PayloadID 0 for every queued job whose originalId is not a number (the
	// live runs' "e2e-orphan-..." rows, for instance), even though the queue
	// knew the payload id.
	payloadID := w.PayloadID
	if payloadID == 0 {
		if id, err := strconv.ParseInt(w.OriginalID, 10, 64); err == nil {
			payloadID = id
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result := callback.JobResult{
		JobID:      w.ID,
		PayloadID:  payloadID,
		Status:     status,
		Message:    message,
		OriginalID: w.OriginalID,
	}

	sender := callback.DefaultCallbackSender()
	if err := sender.SendResult(ctx, u, result); err != nil {
		log.Printf("fireCallback: callback failed for job %d: %v", w.ID, err)
	}
}
