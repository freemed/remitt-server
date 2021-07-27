package jobqueue

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/freemed/remitt-server/common"
	"github.com/freemed/remitt-server/config"
	"github.com/freemed/remitt-server/model"
	"github.com/freemed/remitt-server/model/user"
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
	lock            *sync.RWMutex
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
	log.Printf("JobQueue status %d | %s", o.ID, item)
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

func (o *JobQueueItem) Render() (out []byte, err error) {
	// Create temporary
	inxml, err := ioutil.TempFile(config.Config.Paths.TemporaryPath, "render-in")
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

	outxml, err := ioutil.TempFile(config.Config.Paths.TemporaryPath, "render-out")
	if err != nil {
		log.Printf("Render(): %s", err.Error())
		return
	}
	//defer os.Remove(outxml.Name())

	xslfile := config.Config.Paths.BasePath + string(os.PathSeparator) + "resources" + string(os.PathSeparator) + "xsl" + string(os.PathSeparator) + o.RenderOption + ".xsl"
	//if config.Config.InternalXslt {
	//	err = common.XslTransform(inxml.Name(), xslfile, outxml.Name(), map[string]string{})
	//} else {
	err = common.XslTransformExternal(inxml.Name(), xslfile, outxml.Name(), map[string]string{})
	//}

	// Bring data back in by reading again
	outxml.Close()
	out, err = ioutil.ReadFile(outxml.Name())
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

// StartDispatcher initializes the jobqueue dispatcher with nworker workers
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

	err := executeJob(w)
	return err
}

// executeJob performs the actual worker task
func executeJob(w *JobQueueItem) error {
	tag := fmt.Sprintf("executeJob(%d): ", w.ID)

	u, err := model.GetUserByName(w.User)
	if err != nil {
		log.Printf("executeJob(): %s", err.Error())
		return err
	}
	ctx := user.NewContext(context.Background(), &u)

	// Render
	inxml, err := ioutil.TempFile(config.Config.Paths.TemporaryPath, "render-in")
	if err != nil {
		log.Printf(tag+"%s", err.Error())
		return err
	}
	defer os.Remove(inxml.Name())
	outxml, err := ioutil.TempFile(config.Config.Paths.TemporaryPath, "render-out")
	if err != nil {
		log.Printf("executeJob(): %s", err.Error())
		return err
	}
	defer os.Remove(outxml.Name())
	xslfile := config.Config.Paths.BasePath + string(os.PathSeparator) + "resources" + string(os.PathSeparator) + "xsl" + string(os.PathSeparator) + w.RenderOption + ".xsl"
	//if config.Config.InternalXslt {
	//	err = common.XslTransform(inxml.Name(), xslfile, outxml.Name(), map[string]string{})
	//} else {
	err = common.XslTransformExternal(inxml.Name(), xslfile, outxml.Name(), map[string]string{})
	//}
	err = outxml.Close()
	if err != nil {
		w.Fail(err)
		return err
	}
	renderedData, err := ioutil.ReadFile(outxml.Name())
	if err != nil {
		w.Fail(err)
		return err
	}

	// Instantiate transport plugin
	transportPlugin, err := transport.InstantiateTransporter(w.TransportPlugin)
	if err != nil {
		w.Fail(err)
		return err
	}
	// Pass context to transport plugin
	transportPlugin.SetContext(ctx)

	// Resolve translation plugin
	translationPluginName, err := translation.ResolveTranslator(w.RenderOption, transportPlugin.InputFormat())
	if err != nil {
		w.Fail(err)
		return err
	}
	log.Printf(tag+"Resolved plugin %s for %s -> %s", translationPluginName, w.RenderOption, transportPlugin.InputFormat())

	// Instantiate translation plugin
	translationPlugin, err := translation.InstantiateTranslator(translationPluginName)
	if err != nil {
		w.Fail(err)
		return err
	}
	// Pass context to translation plugin
	translationPlugin.SetContext(ctx)

	// Translation
	translatedData, err := translationPlugin.Translate(renderedData)
	if err != nil {
		w.Fail(err)
		return err
	}

	var ext string
	switch transportPlugin.InputFormat() {
	case "txt":
		ext = "txt"
		break
	case "text":
		ext = "txt"
		break
	case "x12":
		ext = "x12"
		break
	case "pdf":
		ext = "pdf"
		break
	default:
		ext = "txt"
		break
	}
	fn := fmt.Sprintf("%d.%s", time.Now().UnixNano(), ext)
	log.Printf(tag+"Using filename %s", fn)

	// Transmission
	err = transportPlugin.Transport(fn, translatedData)
	if err != nil {
		w.Fail(err)
		return err
	}

	w.Finish()
	return nil
}
