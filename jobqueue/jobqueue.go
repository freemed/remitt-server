package jobqueue

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/freemed/remitt-server/model"
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

type JobQueueItem struct {
	Id        int64          `json:"job_id"`
	Status    string         `json:"status"`
	Enqueued  time.Time      `json:"enqueued"`
	Started   model.NullTime `json:"started"`
	Completed model.NullTime `json:"completed"`
	Message   string         `json:"message"`
	Log       []string       `json:"log"`
	Action    string         `json:"action"`
	User      string         `json:"user"`
	Ip        string         `json:"ip"`
	lock      *sync.RWMutex
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

func (o *JobQueueItem) AppendLog(item string) {
	log.Printf("JobQueue status %d | %s", o.Id, item)
	o.lock.Lock()
	if o.Log == nil {
		o.Log = make([]string, 0)
	}
	o.Log = append(o.Log, strconv.FormatInt(time.Now().Unix(), 10)+"|"+item)

	// Additionally set message to "last log item" automatically without timestamp
	o.Message = item

	// Journal updates to database
	//go model.DbMap.Update(JobLogObjFromJobQueueItem(o))

	o.lock.Unlock()
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
	o.lock.Unlock()
}

func (o *JobQueueItem) Fail(err error) {
	o.lock.Lock()
	o.Status = jobStatusMap[JobStatusFailed]
	o.Completed = model.NullTimeNow()
	o.AppendLog(err.Error())
	o.Message = err.Error()
	o.lock.Unlock()
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

type Worker struct {
	ID          int
	Work        chan JobQueueItem
	WorkerQueue chan chan JobQueueItem
	QuitChan    chan bool
}

// This function "starts" the worker by starting a goroutine, that is
// an infinite "for-select" loop.
func (w Worker) Start() {
	go func() {
		for {
			// Add ourselves into the worker queue.
			w.WorkerQueue <- w.Work

			select {
			case work := <-w.Work:
				// Receive a work request.
				log.Printf("worker[%d]: Received work request id == %d, action == %s", w.ID, work.Id, work.Action)

				// Pull from jobStatusMap
				jobQueueLock.RLock()
				i := jobQueue[work.Id]
				jobQueueLock.RUnlock()

				// Mark as PROCESSING
				i.Lock()
				i.Status = jobStatusMap[JobStatusRunning]
				i.Started = model.NullTimeNow()
				//go model.DbMap.Update(JobLogObjFromJobQueueItem(i))
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

func StartDispatcher(nworkers int) {
	// First, initialize the channel we are going to but the workers' work channels into.
	WorkerQueue = make(chan chan JobQueueItem, nworkers)

	// Now, create all of our workers.
	for i := 0; i < nworkers; i++ {
		log.Printf("StartDispatcher(): Starting worker %d", i+1)
		worker := NewWorker(i+1, WorkerQueue)
		worker.Start()
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

	/*
		model.DbMap.Insert(&model.LogObj{
			LogTime: time.Now(),
			User:    w.User,
			Server:  w.System,
			Domain:  w.Domain,
			Product: w.Product,
			Message: "Requested restart",
		})
	*/

	// TODO: Execute job
	// return executeJob(w, &systemObj, appUser)

	return nil
}
