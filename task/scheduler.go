package task

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/freemed/remitt-server/model"
)

// TaskRunner is a function that runs a single execution of a task.
type TaskRunner func() error

// scheduledTask holds the state of a running scheduled job.
type scheduledTask struct {
	ID       int64
	Schedule string
	Class    string
	runner   TaskRunner
	stopCh   chan struct{}
}

// Scheduler manages background task execution from the tJobs table.
type Scheduler struct {
	mu     sync.Mutex
	tasks  map[int64]*scheduledTask
	ticker *time.Ticker
	stopCh chan struct{}
}

// NewScheduler creates a new task scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		tasks:  make(map[int64]*scheduledTask),
		stopCh: make(chan struct{}),
	}
}

// Start begins the scheduler loop. It reads tJobs and starts task goroutines.
func (s *Scheduler) Start() {
	log.Print("task.Scheduler: Starting scheduler")
	s.ticker = time.NewTicker(30 * time.Second) // Re-read jobs table every 30s
	s.refreshJobs()

	go func() {
		for {
			select {
			case <-s.ticker.C:
				s.refreshJobs()
			case <-s.stopCh:
				log.Print("task.Scheduler: Stopping scheduler")
				s.ticker.Stop()
				s.stopAll()
				return
			}
		}
	}()
}

// Stop gracefully shuts down the scheduler and all tasks.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// refreshJobs reads tJobs and starts/updates tasks accordingly.
func (s *Scheduler) refreshJobs() {
	var jobs []model.JobsModel
	rows, err := model.Queries.GetEnabledJobs(context.Background())
	if err != nil {
		log.Printf("task.Scheduler: refreshJobs error: %s", err.Error())
		return
	}

	// Map dbgen.Tjob → model.JobsModel
	for _, r := range rows {
		jobs = append(jobs, model.JobsModel{
			Id:          r.ID,
			JobSchedule: r.Jobschedule,
			JobClass:    r.Jobclass,
			JobEnabled:  r.Jobenabled,
		})
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	activeIDs := make(map[int64]bool)

	for _, job := range jobs {
		activeIDs[job.Id] = true

		if existing, ok := s.tasks[job.Id]; ok {
			// Already running — check if schedule changed
			if existing.Schedule != job.JobSchedule {
				log.Printf("task.Scheduler: Schedule changed for job %d (%s), restarting", job.Id, job.JobClass)
				s.stopTask(existing)
				delete(s.tasks, job.Id)
			} else {
				continue // No change
			}
		}

		// Start new task
		runner := s.resolveRunner(job.JobClass)
		if runner == nil {
			log.Printf("task.Scheduler: Unknown job class %s for job %d", job.JobClass, job.Id)
			continue
		}

		dur, err := time.ParseDuration(job.JobSchedule)
		if err != nil {
			log.Printf("task.Scheduler: Invalid schedule %s for job %d: %s", job.JobSchedule, job.Id, err.Error())
			continue
		}

		st := &scheduledTask{
			ID:       job.Id,
			Schedule: job.JobSchedule,
			Class:    job.JobClass,
			runner:   runner,
			stopCh:   make(chan struct{}),
		}
		s.tasks[job.Id] = st
		go s.runTask(st, dur)
		log.Printf("task.Scheduler: Started task %d (%s) every %s", job.Id, job.JobClass, dur)
	}

	// Stop tasks that are no longer in the jobs table
	for id, st := range s.tasks {
		if !activeIDs[id] {
			log.Printf("task.Scheduler: Removing task %d (%s)", id, st.Class)
			s.stopTask(st)
			delete(s.tasks, id)
		}
	}
}

// runTask executes a task runner on a repeating interval.
func (s *Scheduler) runTask(st *scheduledTask, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately
	if err := st.runner(); err != nil {
		log.Printf("task.Scheduler: Task %d (%s) error: %s", st.ID, st.Class, err.Error())
	}

	for {
		select {
		case <-ticker.C:
			if err := st.runner(); err != nil {
				log.Printf("task.Scheduler: Task %d (%s) error: %s", st.ID, st.Class, err.Error())
			}
		case <-st.stopCh:
			log.Printf("task.Scheduler: Task %d (%s) stopped", st.ID, st.Class)
			return
		}
	}
}

// stopTask signals a task to stop.
func (s *Scheduler) stopTask(st *scheduledTask) {
	select {
	case st.stopCh <- struct{}{}:
	default:
		// Already stopped
	}
}

// stopAll stops all running tasks.
func (s *Scheduler) stopAll() {
	for _, st := range s.tasks {
		s.stopTask(st)
	}
}

// resolveRunner maps a job class name to a TaskRunner function.
func (s *Scheduler) resolveRunner(jobClass string) TaskRunner {
	switch jobClass {
	case EligibilityJobClass:
		return RunEligibilityTask
	case ScooperJobClass:
		return RunScooperTask
	default:
		return nil
	}
}
