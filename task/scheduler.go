package task

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/freemed/remitt-server/model"
)

// TaskRunner is a function that runs a single execution of a task.
type TaskRunner func() error

// refreshInterval is how often the scheduler re-reads the tJobs table.
const refreshInterval = 30 * time.Second

// cronHorizonDays bounds how far ahead the pattern search looks for a matching
// wall-clock minute. It has to cover the sparsest pattern cron4j can express -
// "0 0 29 feb *" fires at most every 8 years - without turning a schedule that
// can never match (e.g. "0 0 30 feb *") into an infinite loop: cron4j's own
// Predictor spins forever on such a pattern, the scheduler must not.
const cronHorizonDays = 3660 // ~10 years

// lastDayOfMonthSentinel is the value cron4j's DayOfMonthValueParser returns for
// the "L" (last day of the month) token in the day-of-month field. cron4j
// compares it against the real day of month in DayOfMonthValueMatcher.
const lastDayOfMonthSentinel = 32

// scheduledTask holds the state of a running scheduled job.
type scheduledTask struct {
	ID       int64
	Schedule string
	Class    string
	runner   TaskRunner
	schedule *parsedSchedule
	stopCh   chan struct{}
	stopOnce sync.Once

	// clock supplies the current wall-clock time. Tests replace it to exercise
	// the cron loop without waiting for real minutes; nil means time.Now.
	clock func() time.Time
}

// newScheduledTask builds a runnable task, parsing (and rejecting) its
// jobSchedule value up front so a bad schedule can never reach runTask.
func newScheduledTask(id int64, schedule, class string, runner TaskRunner) (*scheduledTask, error) {
	parsed, err := parseSchedule(schedule)
	if err != nil {
		return nil, err
	}
	return &scheduledTask{
		ID:       id,
		Schedule: schedule,
		Class:    class,
		runner:   runner,
		schedule: parsed,
		stopCh:   make(chan struct{}),
	}, nil
}

// now returns the task's reference wall-clock time.
func (st *scheduledTask) now() time.Time {
	if st.clock != nil {
		return st.clock()
	}
	return time.Now()
}

// stopped reports whether a stop request has been made for this task.
func (st *scheduledTask) stopped() bool {
	if st.stopCh == nil {
		return true
	}
	select {
	case <-st.stopCh:
		return true
	default:
		return false
	}
}

// signalStop requests the task's run loop to exit. It is idempotent and safe to
// call from any goroutine, including while the runner is executing: the stop
// channel is closed (not merely sent on), so delivery cannot be lost when the
// loop is not parked in its select.
func (st *scheduledTask) signalStop() {
	if st.stopCh == nil {
		return
	}
	st.stopOnce.Do(func() { close(st.stopCh) })
}

// Scheduler manages background task execution from the tJobs table.
type Scheduler struct {
	mu      sync.Mutex
	tasks   map[int64]*scheduledTask
	ticker  *time.Ticker
	stopCh  chan struct{}
	doneCh  chan struct{}
	running bool
	stopped bool
}

// NewScheduler creates a new task scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{
		tasks:  make(map[int64]*scheduledTask),
		stopCh: make(chan struct{}),
	}
}

// Start begins the scheduler loop. It reads tJobs and starts task goroutines.
// Calling Start on an already running scheduler is a no-op; calling it after
// Stop restarts the scheduler with a fresh stop channel.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Print("task.Scheduler: Start() called while the scheduler is already running; ignoring")
		return
	}
	// Fresh lifecycle state: a Stop()d scheduler can be started again, and the
	// ticker goroutine only ever touches the ticker captured here, so the field
	// below is never read unsynchronised.
	ticker := time.NewTicker(refreshInterval)
	stop := make(chan struct{})
	done := make(chan struct{})
	s.ticker = ticker
	s.stopCh = stop
	s.doneCh = done
	s.running = true
	s.stopped = false
	s.mu.Unlock()

	log.Print("task.Scheduler: Starting scheduler")
	if err := s.refreshJobs(); err != nil {
		log.Printf("task.Scheduler: %s", err.Error())
	}

	go func() {
		defer close(done)
		for {
			select {
			case <-ticker.C:
				if err := s.refreshJobs(); err != nil {
					log.Printf("task.Scheduler: %s", err.Error())
				}
			case <-stop:
				log.Print("task.Scheduler: Stopping scheduler")
				ticker.Stop()
				s.stopAll()
				return
			}
		}
	}()
}

// Stop gracefully shuts down the scheduler and signals every task to stop. It
// is idempotent - a second (or concurrent) call is a no-op, and calling it on a
// scheduler that was never started does nothing - so a lifecycle that stops
// twice cannot panic. It waits for the scheduler loop to exit, but does not
// block on a task runner that is mid-execution: that runner's goroutine sees
// the (now closed) stop channel as soon as it returns.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running || s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.stopCh)
	done := s.doneCh
	s.mu.Unlock()

	<-done

	s.mu.Lock()
	s.running = false
	s.ticker = nil
	s.mu.Unlock()
}

// refreshJobs reads tJobs and starts/updates tasks accordingly. Rows it cannot
// schedule are logged with the reason that rejected them and summarised in the
// returned error; the schedulable rows still start.
func (s *Scheduler) refreshJobs() error {
	if model.Queries == nil {
		return errors.New("refreshJobs: refusing to read tJobs: database not initialized")
	}

	rows, err := model.Queries.GetEnabledJobs(context.Background())
	if err != nil {
		return fmt.Errorf("refreshJobs: unable to read tJobs: %w", err)
	}

	// Map dbgen.Tjob → model.JobsModel
	jobs := make([]model.JobsModel, 0, len(rows))
	for _, r := range rows {
		jobs = append(jobs, model.JobsModel{
			Id:          r.ID,
			JobSchedule: r.Jobschedule,
			JobClass:    r.Jobclass,
			JobEnabled:  r.Jobenabled,
		})
	}

	var rejected []error

	s.mu.Lock()
	defer s.mu.Unlock()

	// A stopped scheduler must not start tasks nothing would ever stop again.
	if s.stopped && !s.running {
		return errors.New("refreshJobs: scheduler is stopped")
	}

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

		// Resolve the job class. An unknown class is reported loudly and
		// rejected; there is deliberately no fuzzy matching, because a class
		// name that does not match one of the registered task classes is a
		// data error the operator has to see (tJobs.jobClass stores the Java
		// task class names, e.g. org.remitt.server.tasks.ScooperTask).
		runner := s.resolveRunner(job.JobClass)
		if runner == nil {
			err := fmt.Errorf("job %d: rejecting unknown job class %q (registered classes: %s, %s)",
				job.Id, job.JobClass, EligibilityJobClass, ScooperJobClass)
			log.Printf("task.Scheduler: %s", err.Error())
			rejected = append(rejected, err)
			continue
		}

		// Parse the jobSchedule. This schema stores cron4j patterns (the Java
		// 0.5.x MasterControl did `new SchedulingPattern(schedule)` on the same
		// column), so both cron patterns and Go durations are accepted here;
		// anything else is rejected with the reason.
		st, err := newScheduledTask(job.Id, job.JobSchedule, job.JobClass, runner)
		if err != nil {
			err = fmt.Errorf("job %d (%s): rejecting unusable jobSchedule %q: %v",
				job.Id, job.JobClass, job.JobSchedule, err)
			log.Printf("task.Scheduler: %s", err.Error())
			rejected = append(rejected, err)
			continue
		}

		// A cron pattern that never matches inside the search horizon is not
		// schedulable either: reject it now instead of starting a goroutine
		// that would silently do nothing.
		if st.schedule.isCron() {
			if _, ok := st.schedule.pattern.next(time.Now()); !ok {
				err = fmt.Errorf("job %d (%s): rejecting schedule %q: no matching time within the next %d days",
					job.Id, job.JobClass, job.JobSchedule, cronHorizonDays)
				log.Printf("task.Scheduler: %s", err.Error())
				rejected = append(rejected, err)
				continue
			}
		}

		s.tasks[job.Id] = st
		go s.runTask(st)
		log.Printf("task.Scheduler: Started task %d (%s) on schedule %s", job.Id, job.JobClass, st.schedule.describe())
	}

	// Stop tasks that are no longer in the jobs table
	for id, st := range s.tasks {
		if !activeIDs[id] {
			log.Printf("task.Scheduler: Removing task %d (%s)", id, st.Class)
			s.stopTask(st)
			delete(s.tasks, id)
		}
	}

	if len(rejected) > 0 {
		return fmt.Errorf("refreshJobs: %d job(s) rejected: %w", len(rejected), errors.Join(rejected...))
	}
	return nil
}

// runTask executes a task runner until the task is signalled to stop. Duration
// schedules run once immediately and then on every interval (the historical Go
// behaviour); cron patterns fire on the wall-clock minutes the pattern selects,
// as the Java cron4j scheduler did. A non-positive interval is rejected with a
// logged error instead of panicking inside time.NewTicker - runTask is started
// with `go`, so a panic here would take the whole process down.
func (s *Scheduler) runTask(st *scheduledTask) {
	if st == nil {
		return
	}
	if st.runner == nil {
		log.Printf("task.Scheduler: Task %d (%s): no runner; not starting", st.ID, st.Class)
		return
	}
	if st.schedule == nil {
		log.Printf("task.Scheduler: Task %d (%s): no parsed schedule; not starting", st.ID, st.Class)
		return
	}

	if st.schedule.isCron() {
		s.runCronTask(st)
		return
	}

	interval := st.schedule.interval
	if interval <= 0 {
		log.Printf("task.Scheduler: Task %d (%s): rejecting schedule %q: interval %s is not positive",
			st.ID, st.Class, st.Schedule, interval)
		return
	}

	// Run once immediately
	if err := st.runner(); err != nil {
		log.Printf("task.Scheduler: Task %d (%s) error: %s", st.ID, st.Class, err.Error())
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if st.stopped() {
				log.Printf("task.Scheduler: Task %d (%s) stopped", st.ID, st.Class)
				return
			}
			if err := st.runner(); err != nil {
				log.Printf("task.Scheduler: Task %d (%s) error: %s", st.ID, st.Class, err.Error())
			}
		case <-st.stopCh:
			log.Printf("task.Scheduler: Task %d (%s) stopped", st.ID, st.Class)
			return
		}
	}
}

// runCronTask waits for each wall-clock fire time the pattern selects and runs
// the task then. It deliberately does not run at start-up: cron4j's scheduler
// waits for the next matching minute too. A run that overruns the next fire
// time skips the missed occurrences rather than queuing them.
func (s *Scheduler) runCronTask(st *scheduledTask) {
	for {
		now := st.now()
		next, ok := st.schedule.pattern.next(now)
		if !ok {
			log.Printf("task.Scheduler: Task %d (%s): schedule %q has no matching time within the next %d days; not rescheduling",
				st.ID, st.Class, st.Schedule, cronHorizonDays)
			return
		}

		delay := next.Sub(now)
		if delay < 0 {
			delay = 0
		}
		log.Printf("task.Scheduler: Task %d (%s) next run at %s (in %s)", st.ID, st.Class, next.Format(time.RFC3339), delay.Round(time.Millisecond))

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			if st.stopped() {
				log.Printf("task.Scheduler: Task %d (%s) stopped", st.ID, st.Class)
				return
			}
			if err := st.runner(); err != nil {
				log.Printf("task.Scheduler: Task %d (%s) error: %s", st.ID, st.Class, err.Error())
			}
		case <-st.stopCh:
			timer.Stop()
			log.Printf("task.Scheduler: Task %d (%s) stopped", st.ID, st.Class)
			return
		}
	}
}

// stopTask signals a task to stop. Delivery is reliable: the task's stop channel
// is closed through sync.Once, so the request is honoured even when the runner
// is executing and the loop only reaches its select later.
func (s *Scheduler) stopTask(st *scheduledTask) {
	if st == nil {
		return
	}
	st.signalStop()
}

// stopAll stops all running tasks.
func (s *Scheduler) stopAll() {
	s.mu.Lock()
	tasks := make([]*scheduledTask, 0, len(s.tasks))
	for _, st := range s.tasks {
		tasks = append(tasks, st)
	}
	s.mu.Unlock()

	for _, st := range tasks {
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

// ---------------------------------------------------------------------------
// Schedule parsing
//
// tJobs.jobSchedule holds the schedule in whatever form the Java 0.5.x code
// understood. That code parsed the column with cron4j 2.2.5
// (MasterControl.java:213, `new SchedulingPattern(schedule)`), so a cron
// pattern is the native form for this schema - including the two rows the
// legacy migration seeds. Go duration strings are also accepted, because rows
// written for the Go server use them.
// ---------------------------------------------------------------------------

// parsedSchedule is a jobSchedule value translated into something the scheduler
// can compute fire times from. Exactly one of pattern/interval is set.
type parsedSchedule struct {
	raw      string
	pattern  *cronPattern
	interval time.Duration
}

// isCron reports whether the schedule is a cron pattern rather than a duration.
func (p *parsedSchedule) isCron() bool { return p != nil && p.pattern != nil }

// next returns the next instant this schedule fires, strictly after `after`.
func (p *parsedSchedule) next(after time.Time) (time.Time, bool) {
	if p == nil {
		return time.Time{}, false
	}
	if p.pattern != nil {
		return p.pattern.next(after)
	}
	if p.interval <= 0 {
		return time.Time{}, false
	}
	return after.Add(p.interval), true
}

// describe renders the parsed schedule for log lines.
func (p *parsedSchedule) describe() string {
	if p == nil {
		return "none"
	}
	if p.pattern != nil {
		return fmt.Sprintf("cron %q", p.raw)
	}
	return fmt.Sprintf("every %s", p.interval)
}

// parseSchedule accepts either a cron4j scheduling pattern or a Go duration
// string. Durations must be positive: a zero or negative interval parses as a
// duration but cannot be scheduled (time.NewTicker panics on it), so it is
// rejected here, at the boundary, with an explanation.
func parseSchedule(schedule string) (*parsedSchedule, error) {
	if strings.TrimSpace(schedule) == "" {
		return nil, errors.New("empty schedule")
	}

	pattern, cronErr := parseCronPattern(schedule)
	if cronErr == nil {
		return &parsedSchedule{raw: schedule, pattern: pattern}, nil
	}

	interval, durErr := time.ParseDuration(schedule)
	if durErr != nil {
		return nil, fmt.Errorf("%q is neither a cron4j cron pattern (%v) nor a Go duration (%v)",
			schedule, cronErr, durErr)
	}
	if interval <= 0 {
		return nil, fmt.Errorf("%q is a non-positive duration; the scheduler needs an interval > 0", schedule)
	}

	return &parsedSchedule{raw: schedule, interval: interval}, nil
}

// nextFire is the pure next-fire-time computation behind the scheduler loop: the
// schedule value from tJobs plus a reference instant in, the next instant the
// job should run out. Tests drive this directly rather than waiting on
// wall-clock minutes.
func nextFire(schedule string, after time.Time) (time.Time, error) {
	parsed, err := parseSchedule(schedule)
	if err != nil {
		return time.Time{}, err
	}
	next, ok := parsed.next(after)
	if !ok {
		return time.Time{}, fmt.Errorf("schedule %q has no next fire time", schedule)
	}
	return next, nil
}

// cronPattern is a parsed cron4j scheduling pattern: one or more space-separated
// alternatives joined by "|". Each alternative has five fields -
// minute hour day-of-month month day-of-week - and a task fires at an instant
// that matches ANY alternative.
type cronPattern struct {
	raw  string
	alts []cronAlternative
}

// cronAlternative is one five-field group of a cron pattern.
type cronAlternative struct {
	minute     cronField
	hour       cronField
	dayOfMonth cronField
	month      cronField
	dayOfWeek  cronField
}

// cronField is a single parsed field: the set of values it allows.
type cronField struct {
	name    string
	min     int
	max     int
	values  []bool // membership by value; length is max+1
	ordered []int  // ascending allowed values
	lastDay bool   // day-of-month "L": matches the last day of the month
}

// matches reports whether v is allowed by this field.
func (f *cronField) matches(v int) bool {
	if v < 0 || v > f.max {
		return false
	}
	return f.values[v]
}

var (
	cronMonthAliases = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}
	cronDayAliases   = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
)

// parseCronPattern parses a cron4j 2.2.5 scheduling pattern. It returns an error
// describing why a value is not a pattern, so the caller can report it.
func parseCronPattern(schedule string) (*cronPattern, error) {
	if strings.TrimSpace(schedule) == "" {
		return nil, errors.New("empty pattern")
	}

	pattern := &cronPattern{raw: schedule}
	for _, alternative := range strings.Split(schedule, "|") {
		// cron4j tokenizes on "|" and drops empty alternatives.
		fields := strings.Fields(alternative)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 5 {
			return nil, fmt.Errorf("alternative %q has %d field(s); cron4j requires 5 (minute hour day-of-month month day-of-week)",
				strings.TrimSpace(alternative), len(fields))
		}
		parsed, err := parseCronAlternative(fields)
		if err != nil {
			return nil, err
		}
		pattern.alts = append(pattern.alts, parsed)
	}
	if len(pattern.alts) == 0 {
		return nil, fmt.Errorf("%q contains no cron fields", schedule)
	}

	return pattern, nil
}

// parseCronAlternative parses the five fields of one alternative.
func parseCronAlternative(fields []string) (cronAlternative, error) {
	var alt cronAlternative
	var err error

	if alt.minute, err = parseCronField("minute", fields[0], 0, 59, false, cronIntegerValue("minute", 0, 59)); err != nil {
		return alt, err
	}
	if alt.hour, err = parseCronField("hour", fields[1], 0, 23, false, cronIntegerValue("hour", 0, 23)); err != nil {
		return alt, err
	}
	if alt.dayOfMonth, err = parseCronField("day of month", fields[2], 1, 31, true, cronDayOfMonthValue); err != nil {
		return alt, err
	}
	if alt.month, err = parseCronField("month", fields[3], 1, 12, false, cronMonthValue); err != nil {
		return alt, err
	}
	// Day of week is 0-7 in cron4j with both 0 and 7 meaning Sunday, so the
	// value parser normalises modulo 7 and the enumerated range keeps the raw
	// values (a 7 in a range never matches a real weekday, which is harmless -
	// that is cron4j's own behaviour).
	if alt.dayOfWeek, err = parseCronField("day of week", fields[4], 0, 7, false, cronDayOfWeekValue); err != nil {
		return alt, err
	}

	return alt, nil
}

// parseCronField parses one field: a comma list of elements, each of which is
// "*", a value, a "a-b" range, optionally followed by "/step". Step semantics
// follow cron4j exactly: the divisor indexes the enumerated value list, so
// "0/6" in the hour field selects the hour 0 alone (the list [0] stepped by 6)
// while "*/6" selects 0, 6, 12 and 18.
func parseCronField(name, raw string, min, max int, allowLastDay bool, parseValue func(string) (int, error)) (cronField, error) {
	field := cronField{
		name:   name,
		min:    min,
		max:    max,
		values: make([]bool, max+1),
	}
	if raw == "" {
		return field, fmt.Errorf("%s field is empty", name)
	}

	seen := make(map[int]bool)

	for _, element := range strings.Split(raw, ",") {
		if element == "" {
			return field, fmt.Errorf("%s field %q contains an empty list element", name, raw)
		}

		parts := strings.Split(element, "/")
		if len(parts) > 2 {
			return field, fmt.Errorf("%s field element %q has more than one '/' step", name, element)
		}

		var divisor int
		if len(parts) == 2 {
			d, err := strconv.Atoi(parts[1])
			if err != nil {
				return field, fmt.Errorf("%s field element %q: invalid step %q", name, element, parts[1])
			}
			if d < 1 {
				return field, fmt.Errorf("%s field element %q: step must be >= 1, got %d", name, element, d)
			}
			divisor = d
		}

		list, err := parseCronRange(name, parts[0], min, max, parseValue)
		if err != nil {
			return field, err
		}
		if divisor > 1 {
			stepped := make([]int, 0, len(list))
			for i := 0; i < len(list); i += divisor {
				stepped = append(stepped, list[i])
			}
			list = stepped
		}

		for _, v := range list {
			if allowLastDay && v == lastDayOfMonthSentinel {
				// Only the day-of-month field has an "L" token, so the sentinel
				// is interpreted there and nowhere else: treating a plain 32 as
				// "last day of month" would silently drop minute 32.
				field.lastDay = true
				continue
			}
			if !seen[v] {
				seen[v] = true
				field.ordered = append(field.ordered, v)
			}
			field.values[v] = true
		}
	}

	if len(field.ordered) == 0 && !field.lastDay {
		return field, fmt.Errorf("%s field %q allows no values", name, raw)
	}
	sort.Ints(field.ordered)

	return field, nil
}

// parseCronRange parses one comma-free element of a field: "*", a single value,
// or "a-b". Wrapping ranges (a > b) are supported, as in cron4j: in the
// day-of-week field "fri-mon" means Friday, Saturday, Sunday and Monday.
func parseCronRange(name, element string, min, max int, parseValue func(string) (int, error)) ([]int, error) {
	if element == "*" {
		all := make([]int, 0, max-min+1)
		for v := min; v <= max; v++ {
			all = append(all, v)
		}
		return all, nil
	}

	parts := strings.Split(element, "-")
	if len(parts) > 2 {
		return nil, fmt.Errorf("%s field element %q has more than one '-' range", name, element)
	}

	first, err := parseValue(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%s field element %q: %v", name, element, err)
	}
	if len(parts) == 1 {
		return []int{first}, nil
	}

	second, err := parseValue(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%s field element %q: %v", name, element, err)
	}

	switch {
	case first < second:
		out := make([]int, 0, second-first+1)
		for v := first; v <= second; v++ {
			out = append(out, v)
		}
		return out, nil
	case first > second:
		out := make([]int, 0, max-first+second-min+2)
		for v := first; v <= max; v++ {
			out = append(out, v)
		}
		for v := min; v <= second; v++ {
			out = append(out, v)
		}
		return out, nil
	default:
		return []int{first}, nil
	}
}

// cronIntegerValue parses a plain numeric field value in [min, max].
func cronIntegerValue(name string, min, max int) func(string) (int, error) {
	return func(s string) (int, error) {
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0, fmt.Errorf("invalid integer value %q", s)
		}
		if v < min || v > max {
			return 0, fmt.Errorf("value %d out of the %s range %d-%d", v, name, min, max)
		}
		return v, nil
	}
}

// cronAliasedValue parses a numeric value or one of the three-letter English
// names cron4j accepts ("jan".."dec" for months, "sun".."sat" for days of the
// week), case-insensitively. When modulo is non-zero the numeric value is
// normalised with it, which is how cron4j maps both 0 and 7 to Sunday.
func cronAliasedValue(name string, aliases []string, min, max, offset, modulo int) func(string) (int, error) {
	integer := cronIntegerValue(name, min, max)
	return func(s string) (int, error) {
		if v, err := integer(s); err == nil {
			if modulo > 0 {
				return v % modulo, nil
			}
			return v, nil
		}
		lower := strings.ToLower(s)
		for i, alias := range aliases {
			if lower == alias {
				return i + offset, nil
			}
		}
		return 0, fmt.Errorf("invalid value %q: not an integer in %d-%d and not a known name", s, min, max)
	}
}

// cronMonthValue parses the month field: 1-12 or jan..dec.
func cronMonthValue(s string) (int, error) {
	return cronAliasedValue("month", cronMonthAliases, 1, 12, 1, 0)(s)
}

// cronDayOfWeekValue parses the day-of-week field: 0-7 (0 and 7 are both
// Sunday) or sun..sat.
func cronDayOfWeekValue(s string) (int, error) {
	return cronAliasedValue("day of week", cronDayAliases, 0, 7, 0, 7)(s)
}

// cronDayOfMonthValue parses the day-of-month field: 1-31 or "L" for the last
// day of the month, which cron4j represents internally as 32.
func cronDayOfMonthValue(s string) (int, error) {
	if strings.EqualFold(s, "L") {
		return lastDayOfMonthSentinel, nil
	}
	return cronIntegerValue("day of month", 1, 31)(s)
}

// matchesDay reports whether the alternative's month, day-of-month and
// day-of-week fields all match the given day. cron4j ANDs these fields: when
// both day-of-month and day-of-week are restricted, a day has to satisfy both
// ("0 0 13 * 5" fires on Friday the 13th, not on every Friday or the 13th).
func (a *cronAlternative) matchesDay(day time.Time) bool {
	if !a.month.matches(int(day.Month())) {
		return false
	}

	dayOfMonthOK := a.dayOfMonth.matches(day.Day())
	if !dayOfMonthOK && a.dayOfMonth.lastDay && day.Day() == daysInMonth(day.Year(), day.Month()) {
		dayOfMonthOK = true
	}

	return dayOfMonthOK && a.dayOfWeek.matches(int(day.Weekday()))
}

// matches reports whether the alternative matches the given instant. Seconds
// are not part of a cron4j pattern, so only minute-and-coarser fields are
// compared.
func (a *cronAlternative) matches(t time.Time) bool {
	return a.minute.matches(t.Minute()) &&
		a.hour.matches(t.Hour()) &&
		a.matchesDay(t)
}

// firstTimeOnDay returns the earliest instant on `day` that the alternative
// matches and that is not before `notBefore` (which is how a same-day search
// skips minutes that have already passed).
func (a *cronAlternative) firstTimeOnDay(day, notBefore time.Time) (time.Time, bool) {
	sameDay := day.Year() == notBefore.Year() && day.YearDay() == notBefore.YearDay()

	for _, hour := range a.hour.ordered {
		if sameDay && hour < notBefore.Hour() {
			continue
		}
		for _, minute := range a.minute.ordered {
			if sameDay && hour == notBefore.Hour() && minute < notBefore.Minute() {
				continue
			}
			t := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, day.Location())
			if !t.Before(notBefore) {
				return t, true
			}
		}
	}

	return time.Time{}, false
}

// next returns the first instant strictly after `after` that matches the
// pattern, using cron4j's Predictor semantics: the candidate times are minute
// aligned and the instant `after` falls in is itself excluded ("* * * * *" from
// 12:29:00 gives 12:30:00). The search is bounded by cronHorizonDays; false
// means the pattern does not match anything in that window.
func (p *cronPattern) next(after time.Time) (time.Time, bool) {
	if p == nil || len(p.alts) == 0 {
		return time.Time{}, false
	}

	location := after.Location()

	// The first candidate minute: the minute after the one `after` is in.
	year, month, day := after.Date()
	notBefore := time.Date(year, month, day, after.Hour(), after.Minute(), 0, 0, location).Add(time.Minute)

	// Walk forward one day at a time, testing each alternative's day fields
	// first (cheap) and only then its hour/minute sets.
	cursor := time.Date(notBefore.Year(), notBefore.Month(), notBefore.Day(), 0, 0, 0, 0, location)
	for i := 0; i < cronHorizonDays; i++ {
		var best time.Time
		for ai := range p.alts {
			alt := &p.alts[ai]
			if !alt.matchesDay(cursor) {
				continue
			}
			candidate, ok := alt.firstTimeOnDay(cursor, notBefore)
			if !ok {
				continue
			}
			if best.IsZero() || candidate.Before(best) {
				best = candidate
			}
		}
		if !best.IsZero() {
			return best, true
		}
		cursor = cursor.AddDate(0, 0, 1)
	}

	return time.Time{}, false
}

// daysInMonth returns the number of days in the given month.
func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
