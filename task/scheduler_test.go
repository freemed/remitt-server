package task

// DB-free test suite for the task (job scheduler) package.
//
// Environment: this repository has no test database, and model.Queries is a nil
// *dbgen.Queries without a live MySQL connection, so every code path that reads
// tJobs panics on a nil dereference. The strategy is therefore:
//
//  1. Exercise the pure logic directly: the job-class dispatch table
//     (resolveRunner, scheduler.go:179), the jobSchedule string contract
//     (time.ParseDuration, scheduler.go:111), NewScheduler and runTask's tick
//     loop (scheduler.go:140) with an injected runner.
//  2. Exercise the real dispatch code (refreshJobs, scheduler.go:67) against an
//     in-memory database/sql driver installed on model.Queries. The scheduler
//     has NO injection seam - refreshJobs dereferences the package-level
//     model.Queries handle directly (scheduler.go:69) - so swapping that global
//     is the only way to reach the code without MySQL; this is reported as a
//     testability finding.
//  3. Skip, naming the missing dependency, every DB-bound path that has no fake
//     (the convention already used by eligibility/gatewayedi_test.go and
//     api/api_test.go).
//
// Defects found while writing this suite are pinned by characterization tests
// (marked BUG in the test comment) and reported; production code was NOT
// modified. Characterization assertions fail if a defect is fixed; update the
// test when that happens.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/freemed/remitt-server/internal/dbgen"
	"github.com/freemed/remitt-server/model"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// waitFor polls cond until it returns true or the timeout elapses.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func doneClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// startRunTask runs the scheduler's tick loop in its own goroutine and returns a
// channel closed when runTask has returned.
func startRunTask(s *Scheduler, st *scheduledTask, interval time.Duration) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runTask(st, interval)
	}()
	return done
}

// loopGuard retires tasks deterministically so no test can hang or leak, and
// remembers which stop channels it already closed.
type loopGuard struct {
	mu     sync.Mutex
	closed map[*scheduledTask]bool
}

func newLoopGuard() *loopGuard {
	return &loopGuard{closed: make(map[*scheduledTask]bool)}
}

func (g *loopGuard) isClosed(st *scheduledTask) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed[st]
}

// hardStop closes the task's stop channel, which the runTask select also
// honours, so the goroutine always returns.
func (g *loopGuard) hardStop(st *scheduledTask) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed[st] {
		return
	}
	g.closed[st] = true
	close(st.stopCh)
}

// retire stops a task through the production stop path. That path
// ((*Scheduler).stopTask, scheduler.go:163) is best effort - it discards the
// signal whenever the runTask goroutine is not parked in its select - so it is
// polled; if the goroutine still has not returned by the deadline the stop
// channel is closed directly. Safe to call repeatedly, and always bounded.
func (g *loopGuard) retire(t *testing.T, s *Scheduler, st *scheduledTask, done <-chan struct{}, timeout time.Duration) bool {
	if t != nil {
		t.Helper()
	}

	if done != nil && doneClosed(done) {
		return true
	}

	if !g.isClosed(st) {
		deadline := time.Now().Add(timeout)
		for time.Now().Before(deadline) {
			s.stopTask(st)
			if done != nil && doneClosed(done) {
				return true
			}
			time.Sleep(2 * time.Millisecond)
		}
		if t != nil {
			t.Logf("finding: task %d did not retire through the production stop path within %s "+
				"(scheduler.go:163 drops the signal while the runner is busy); hard-stopping it", st.ID, timeout)
		}
		g.hardStop(st)
	}

	if done == nil {
		return true
	}
	return waitFor(timeout, func() bool { return doneClosed(done) })
}

// ---------------------------------------------------------------------------
// in-memory job source (fake database/sql driver)
// ---------------------------------------------------------------------------

// fakeJobsSource is an in-memory stand-in for the tJobs table. It deliberately
// ignores the WHERE jobEnabled = TRUE clause of GetEnabledJobs: rows handed to
// setJobs are returned verbatim, so tests only ever feed enabled rows.
type fakeJobsSource struct {
	mu   sync.Mutex
	rows [][]driver.Value
}

func (f *fakeJobsSource) setJobs(rows ...[]driver.Value) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = rows
}

func (f *fakeJobsSource) snapshot() [][]driver.Value {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]driver.Value, len(f.rows))
	copy(out, f.rows)
	return out
}

// jobRow builds a tJobs row in the column order GetEnabledJobs scans:
// id, jobschedule, jobclass, jobenabled.
func jobRow(id int64, schedule, class string, enabled bool) []driver.Value {
	return []driver.Value{id, schedule, class, enabled}
}

var fakeJobsDriverSeq atomic.Int64

type fakeJobsDriver struct{ src *fakeJobsSource }

func (d *fakeJobsDriver) Open(string) (driver.Conn, error) {
	return &fakeJobsConn{src: d.src}, nil
}

type fakeJobsConn struct{ src *fakeJobsSource }

func (c *fakeJobsConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (c *fakeJobsConn) Close() error                        { return nil }
func (c *fakeJobsConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (c *fakeJobsConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &fakeJobsRows{
		cols: []string{"id", "jobschedule", "jobclass", "jobenabled"},
		rows: c.src.snapshot(),
	}, nil
}

type fakeJobsRows struct {
	cols []string
	rows [][]driver.Value
	idx  int
}

func (r *fakeJobsRows) Columns() []string { return r.cols }
func (r *fakeJobsRows) Close() error      { return nil }

func (r *fakeJobsRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.idx])
	r.idx++
	return nil
}

// installFakeJobs points model.Queries at a hermetic in-memory database and
// restores the previous handle when the test ends. It exists only because the
// scheduler offers no way to inject a job source (scheduler.go:69).
func installFakeJobs(t *testing.T) *fakeJobsSource {
	t.Helper()

	src := &fakeJobsSource{}
	name := fmt.Sprintf("remitt-task-fake-%d", fakeJobsDriverSeq.Add(1))
	sql.Register(name, &fakeJobsDriver{src: src})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open(%q): %v", name, err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })

	saved := model.Queries
	model.Queries = dbgen.New(db)
	t.Cleanup(func() { model.Queries = saved })

	return src
}

// schedulerFixture couples a fake job source with a scheduler and retires every
// task it starts, so no test leaves a goroutine behind.
type schedulerFixture struct {
	s        *Scheduler
	src      *fakeJobsSource
	g        *loopGuard
	baseline int

	mu      sync.Mutex
	tracked []*scheduledTask
}

func newSchedulerFixture(t *testing.T) *schedulerFixture {
	t.Helper()

	f := &schedulerFixture{
		s:        NewScheduler(),
		g:        newLoopGuard(),
		src:      installFakeJobs(t),
		baseline: runtime.NumGoroutine(),
	}
	t.Cleanup(f.stop)
	return f
}

// track remembers a task the scheduler started so the fixture can retire it.
func (f *schedulerFixture) track(st *scheduledTask) *scheduledTask {
	if st == nil {
		return nil
	}
	f.mu.Lock()
	f.tracked = append(f.tracked, st)
	f.mu.Unlock()
	return st
}

// task returns the scheduler's current task for id (same package access).
func (f *schedulerFixture) task(id int64) *scheduledTask {
	return f.track(f.s.tasks[id])
}

// stop retires every task the fixture saw; it runs as a t.Cleanup so a failing
// assertion cannot leave a goroutine behind.
func (f *schedulerFixture) stop() {
	f.mu.Lock()
	tracked := append([]*scheduledTask(nil), f.tracked...)
	f.mu.Unlock()

	seen := make(map[*scheduledTask]bool)
	for _, st := range tracked {
		if st == nil || seen[st] {
			continue
		}
		seen[st] = true
		f.g.retire(nil, f.s, st, nil, 100*time.Millisecond)
	}

	if !waitFor(500*time.Millisecond, func() bool { return runtime.NumGoroutine() <= f.baseline }) {
		// Diagnostics only: report, never fail, so the suite stays deterministic.
		fmt.Printf("task tests: %d goroutine(s) still running after cleanup (baseline %d)\n",
			runtime.NumGoroutine(), f.baseline)
	}
}

// ---------------------------------------------------------------------------
// seed data (migrations/001_legacy.up.sql)
// ---------------------------------------------------------------------------

type seedJobRow struct {
	id       int64
	schedule string
	class    string
	enabled  bool
}

var seedJobRowRe = regexp.MustCompile(`\(\s*(\d+)\s*,\s*'([^']*)'\s*,\s*'([^']*)'\s*,\s*(TRUE|FALSE)\s*\)`)

// loadSeedJobs reads the tJobs seed rows shipped in the initial migration.
func loadSeedJobs(t *testing.T) []seedJobRow {
	t.Helper()

	const rel = "../migrations/001_legacy.up.sql"
	data, err := os.ReadFile(filepath.Join("..", "migrations", "001_legacy.up.sql"))
	if err != nil {
		t.Skipf("cannot read the tJobs seed migration %s: %v", rel, err)
	}

	text := string(data)
	start := strings.Index(text, "INSERT INTO tJobs VALUES")
	if start < 0 {
		t.Skipf("no 'INSERT INTO tJobs VALUES' block in %s", rel)
	}
	end := strings.Index(text[start:], ";")
	if end < 0 {
		end = len(text) - start
	}

	matches := seedJobRowRe.FindAllStringSubmatch(text[start:start+end], -1)
	if len(matches) == 0 {
		t.Skipf("no tJobs rows matched in %s", rel)
	}

	rows := make([]seedJobRow, 0, len(matches))
	for _, m := range matches {
		id, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatalf("parsing tJobs id %q from %s: %v", m[1], rel, err)
		}
		rows = append(rows, seedJobRow{
			id:       id,
			schedule: m[2],
			class:    m[3],
			enabled:  m[4] == "TRUE",
		})
	}
	return rows
}

// ---------------------------------------------------------------------------
// pure logic: construction, dispatch table, schedule contract
// ---------------------------------------------------------------------------

func TestNewSchedulerInitialState(t *testing.T) {
	s := NewScheduler()

	if s == nil {
		t.Fatal("NewScheduler() returned nil")
	}
	if s.tasks == nil {
		t.Error("NewScheduler(): tasks map is nil; refreshJobs would panic on assignment")
	}
	if len(s.tasks) != 0 {
		t.Errorf("NewScheduler(): len(tasks) = %d; want 0", len(s.tasks))
	}
	if s.ticker != nil {
		t.Error("NewScheduler(): ticker is non-nil before Start()")
	}
	if s.stopCh == nil {
		t.Error("NewScheduler(): stopCh is nil; Stop() would panic")
	}

	// stopCh must be open and unbuffered (Stop closes it; only the select in
	// Start's goroutine may receive from it).
	select {
	case <-s.stopCh:
		t.Error("NewScheduler(): stopCh is already closed")
	default:
	}
}

func TestJobClassConstants(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		// Names come from the Java RemITT task classes stored in tJobs.jobClass.
		{"EligibilityJobClass", EligibilityJobClass, "org.remitt.server.tasks.EligibilityTask"},
		{"ScooperJobClass", ScooperJobClass, "org.remitt.server.tasks.ScooperTask"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s = %q; want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestResolveRunnerDispatchTable pins the job-class to task-function mapping the
// scheduler dispatches on (scheduler.go:179). Function identity is compared by
// code pointer because function values are not comparable.
func TestResolveRunnerDispatchTable(t *testing.T) {
	s := NewScheduler()

	eligibilityPtr := reflect.ValueOf(RunEligibilityTask).Pointer()
	scooperPtr := reflect.ValueOf(RunScooperTask).Pointer()

	cases := []struct {
		name    string
		class   string
		wantPtr uintptr // 0 means "no runner expected"
	}{
		{"eligibility_class", EligibilityJobClass, eligibilityPtr},
		{"scooper_class", ScooperJobClass, scooperPtr},

		{"empty_class", "", 0},
		{"short_class_name", "EligibilityTask", 0},
		{"lowercase_class", strings.ToLower(EligibilityJobClass), 0},
		{"uppercase_class", strings.ToUpper(EligibilityJobClass), 0},
		{"suffix_whitespace", EligibilityJobClass + " ", 0},
		{"prefix_whitespace", " " + ScooperJobClass, 0},
		{"plugin_class_not_a_task", "org.remitt.plugin.eligibility.GatewayEDIEligibility", 0},
		{"unknown_class", "com.example.NoSuchTask", 0},

		// The class name seeded in migrations/001_legacy.up.sql:353 is missing
		// its second "i" ("EligibiltyTask"), so it does not match the constant
		// above and the seeded job is skipped as an unknown class.
		{"seed_typo_class", "org.remitt.server.tasks.EligibiltyTask", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := s.resolveRunner(tc.class)

			if tc.wantPtr == 0 {
				if got != nil {
					t.Errorf("resolveRunner(%q) resolved a runner; want nil (unknown class)", tc.class)
				}
				return
			}
			if got == nil {
				t.Fatalf("resolveRunner(%q) = nil; want the registered task function", tc.class)
			}
			if ptr := reflect.ValueOf(got).Pointer(); ptr != tc.wantPtr {
				t.Errorf("resolveRunner(%q) resolved the wrong function (ptr %#x; want %#x)", tc.class, ptr, tc.wantPtr)
			}
		})
	}
}

// TestScheduleStringContract pins the jobSchedule values the scheduler accepts:
// scheduler.go:111 parses the column with time.ParseDuration, so ONLY Go
// duration strings schedule a job. Cron/Quartz expressions are rejected, and
// zero or negative durations parse successfully but are handled badly later.
func TestScheduleStringContract(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
		wantOK   bool
		want     time.Duration
	}{
		{"seconds", "30s", true, 30 * time.Second},
		{"minutes", "90m", true, 90 * time.Minute},
		{"hours", "1h", true, time.Hour},
		{"compound", "1h30m", true, 90 * time.Minute},
		{"milliseconds", "500ms", true, 500 * time.Millisecond},
		{"day_as_24h", "24h", true, 24 * time.Hour},

		{"cron_every_minute", "* * * * *", false, 0},
		{"cron_every_30_minutes", "*/30 * * * *", false, 0},
		{"quartz", "0 0/30 * * * ?", false, 0},
		{"cron_nickname", "@every 1h", false, 0},
		{"bare_number", "60", false, 0},
		{"prose", "5 minutes", false, 0},
		{"days", "1d", false, 0},
		{"empty", "", false, 0},
		{"whitespace", " ", false, 0},
		{"trailing_newline", "1h\n", false, 0},

		// ParseDuration accepts these, so they pass the scheduler's validation
		// and reach time.NewTicker, which panics on a non-positive interval
		// (see TestRunTaskPanicsOnNonPositiveInterval).
		{"zero", "0s", true, 0},
		{"zero_bare", "0", true, 0},
		{"negative", "-5m", true, -5 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := time.ParseDuration(tc.schedule)

			if tc.wantOK {
				if err != nil {
					t.Fatalf("time.ParseDuration(%q) error = %v; want it accepted at scheduler.go:111", tc.schedule, err)
				}
				if got != tc.want {
					t.Errorf("time.ParseDuration(%q) = %v; want %v", tc.schedule, got, tc.want)
				}
				return
			}
			if err == nil {
				t.Errorf("time.ParseDuration(%q) = %v, nil; want a parse error so refreshJobs skips the job (scheduler.go:111-115)", tc.schedule, got)
			}
		})
	}
}

// TestSeedJobsAreRejectedByTheScheduler documents that neither row shipped in
// migrations/001_legacy.up.sql can ever be scheduled: both jobSchedule values are
// 5-field cron expressions and scheduler.go:111 parses the column as a Go
// duration.
//
// BUG (seed data vs scheduler, migrations/001_legacy.up.sql:352-353 ->
// scheduler.go:111): every seeded job is skipped with "Invalid schedule", so an
// unmodified deployment runs no tasks at all. Characterization test: if the seed
// data or the parser is fixed, update this test.
func TestSeedJobsAreRejectedByTheScheduler(t *testing.T) {
	rows := loadSeedJobs(t)
	if len(rows) != 2 {
		t.Fatalf("len(seed tJobs rows) = %d; want 2", len(rows))
	}

	for _, row := range rows {
		t.Run(fmt.Sprintf("job_%d", row.id), func(t *testing.T) {
			if !row.enabled {
				t.Skipf("seed job %d is disabled; the scheduler would not read it", row.id)
			}
			dur, err := time.ParseDuration(row.schedule)
			if err == nil {
				t.Errorf("seed schedule %q for job %d parses as %v; this test pins the documented mismatch "+
					"between cron seed data and the duration parser (scheduler.go:111) - update it if the "+
					"seed data or the parser changed", row.schedule, row.id, dur)
				return
			}
			t.Logf("BUG: seed job %d (%s) uses cron schedule %q, which time.ParseDuration rejects (%v); "+
				"refreshJobs skips it at scheduler.go:111-115", row.id, row.class, row.schedule, err)
		})
	}
}

// TestSeedEligibilityJobClassDoesNotResolve pins the typo in the seeded class
// name: tJobs row 2 says "org.remitt.server.tasks.EligibiltyTask", but the
// dispatch table only knows "org.remitt.server.tasks.EligibilityTask"
// (task/eligibility.go:17, scheduler.go:180-182).
//
// BUG (seed data, migrations/001_legacy.up.sql:353): even with a cron-capable
// schedule parser the seeded eligibility job would log "Unknown job class".
// Characterization test: update it when the seed data is fixed.
func TestSeedEligibilityJobClassDoesNotResolve(t *testing.T) {
	s := NewScheduler()

	var candidate *seedJobRow
	for _, row := range loadSeedJobs(t) {
		if row.class != ScooperJobClass && strings.HasSuffix(row.class, "Task") {
			r := row
			candidate = &r
			break
		}
	}
	if candidate == nil {
		t.Skip("no non-scooper task row found in the tJobs seed data")
	}

	if runner := s.resolveRunner(candidate.class); runner != nil {
		t.Errorf("the seeded class %q now resolves to a runner; the seed typo was fixed - update this test",
			candidate.class)
		return
	}
	t.Logf("BUG: seeded class %q for job %d does not match EligibilityJobClass (%q); "+
		"refreshJobs logs \"Unknown job class\" for it (scheduler.go:105-109)",
		candidate.class, candidate.id, EligibilityJobClass)

	if runner := s.resolveRunner(EligibilityJobClass); runner == nil {
		t.Errorf("resolveRunner(%q) = nil; the dispatch table lost the eligibility task", EligibilityJobClass)
	}
}

// ---------------------------------------------------------------------------
// pure logic: the runTask tick loop and the stop path
// ---------------------------------------------------------------------------

// TestRunTaskRunsImmediatelyAndOnEveryTick exercises the tick loop with an
// injected runner, so no database is involved.
func TestRunTaskRunsImmediatelyAndOnEveryTick(t *testing.T) {
	s := NewScheduler()
	g := newLoopGuard()

	var calls atomic.Int64
	st := &scheduledTask{
		ID:       1,
		Schedule: "5ms",
		Class:    ScooperJobClass,
		runner:   func() error { calls.Add(1); return nil },
		stopCh:   make(chan struct{}),
	}
	done := startRunTask(s, st, 5*time.Millisecond)
	defer func() {
		if !g.retire(t, s, st, done, 2*time.Second) {
			t.Errorf("task 1 did not return after stop; the tick loop leaked")
		}
	}()

	if !waitFor(2*time.Second, func() bool { return calls.Load() >= 1 }) {
		t.Fatal("the runner was never invoked; runTask does not run once immediately (scheduler.go:145)")
	}
	if !waitFor(2*time.Second, func() bool { return calls.Load() >= 3 }) {
		t.Fatalf("runner calls after ~2s at a 5ms interval = %d; want at least 3", calls.Load())
	}
}

// TestRunTaskKeepsTickingAfterRunnerError checks that a failing runner does not
// abort the tick loop: the error is logged (scheduler.go:146/153) and the loop
// keeps ticking.
func TestRunTaskKeepsTickingAfterRunnerError(t *testing.T) {
	s := NewScheduler()
	g := newLoopGuard()

	var calls atomic.Int64
	st := &scheduledTask{
		ID:       7,
		Schedule: "5ms",
		Class:    EligibilityJobClass,
		runner:   func() error { calls.Add(1); return fmt.Errorf("injected failure") },
		stopCh:   make(chan struct{}),
	}
	done := startRunTask(s, st, 5*time.Millisecond)
	defer func() {
		if !g.retire(t, s, st, done, 2*time.Second) {
			t.Errorf("task 7 did not return after stop; the tick loop leaked")
		}
	}()

	if !waitFor(2*time.Second, func() bool { return calls.Load() >= 3 }) {
		t.Fatalf("a failing runner stopped the tick loop after %d call(s); want it to keep ticking", calls.Load())
	}
}

// TestStopTaskDropsSignalWhileRunnerIsBusy is the minimal reproduction for the
// stop-path defect.
//
// BUG (task/scheduler.go:163-169): stopTask uses a non-blocking send on an
// unbuffered channel; when the runTask goroutine is not parked in its select -
// i.e. whenever the runner is executing, which for a real task is the whole
// point - the send takes the default branch and the stop request is silently
// discarded. The task then runs forever.
//
// Reproduction (this test): start runTask with a runner that blocks, call
// stopTask while the goroutine is inside the runner, release the runner, and
// observe that the tick loop keeps running instead of returning.
// Characterization test: if stopTask is made reliable (buffered channel, close,
// or context), update this test.
func TestStopTaskDropsSignalWhileRunnerIsBusy(t *testing.T) {
	s := NewScheduler()
	g := newLoopGuard()

	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once

	st := &scheduledTask{
		ID:       99,
		Schedule: "1h",
		Class:    ScooperJobClass,
		runner: func() error {
			enteredOnce.Do(func() { close(entered) })
			<-release // hold the goroutine inside the runner
			return nil
		},
		stopCh: make(chan struct{}),
	}
	done := startRunTask(s, st, time.Hour)
	defer func() {
		if !g.retire(t, s, st, done, 2*time.Second) {
			t.Errorf("task 99 did not return at all; the stop path is unrecoverable")
		}
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the runner was never invoked; runTask did not start")
	}

	// The goroutine is inside the runner, so no receiver is waiting and the send
	// cannot be delivered.
	begin := time.Now()
	s.stopTask(st)
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Errorf("stopTask blocked for %s; it is a non-blocking send", elapsed)
	}

	close(release)

	// The discarded signal means the loop blocks in its select even though the
	// runner has returned.
	if waitFor(300*time.Millisecond, func() bool { return doneClosed(done) }) {
		t.Errorf("the stop request was honoured while the runner was busy; the drop at scheduler.go:163 no "+
			"longer reproduces - update this test (task %d, class %s)", st.ID, st.Class)
		return
	}

	t.Logf("BUG: task 99 is still ticking 300ms after a stop request made while its runner was running " +
		"(unbuffered stopCh + non-blocking send, scheduler.go:163-169); the request is only honoured if a " +
		"later call happens to find the goroutine parked in the select")

	// Deterministic cleanup: the goroutine is parked in the select now, so this
	// send is received.
	if !g.retire(t, s, st, done, 2*time.Second) {
		t.Fatalf("task 99 could not be stopped even while parked in its select")
	}
}

// TestStopIsNotIdempotent pins the lifecycle defect in Stop.
//
// BUG (task/scheduler.go:62-64): Stop closes stopCh unconditionally, so a second
// call panics with "close of closed channel"; a scheduler stopped twice crashes
// its caller. In addition, because the channel is never recreated, a Start after
// a Stop keeps using the closed channel and its ticker goroutine exits on the
// first iteration (scheduler.go:43-58).
// Characterization test: if Stop becomes idempotent, update this test.
func TestStopIsNotIdempotent(t *testing.T) {
	s := NewScheduler()
	s.Stop()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		s.Stop()
	}()

	if recovered == nil {
		t.Errorf("second Stop() did not panic; Stop is idempotent now - update this test")
		return
	}
	t.Logf("BUG: a second Stop() panics with %v (scheduler.go:63 closes stopCh unconditionally)", recovered)
}

// TestRunTaskPanicsOnNonPositiveInterval is the minimal reproduction for the
// interval-validation defect.
//
// BUG (task/scheduler.go:111 + scheduler.go:141 + scheduler.go:125):
// refreshJobs validates jobSchedule with time.ParseDuration only, which accepts
// "0", "0s" and negative durations; the accepted value is handed to
// time.NewTicker inside runTask, which panics on a non-positive interval. In
// production runTask is started with `go` from refreshJobs, and a panic in a
// goroutine cannot be recovered by the caller - the whole server process dies. A
// single tJobs row with jobSchedule '0s' (or '-5m') is enough.
// Characterization test: if a positive-duration check is added, update this test.
func TestRunTaskPanicsOnNonPositiveInterval(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
	}{
		{"zero_seconds", "0s"},
		{"zero_bare", "0"},
		{"negative", "-5m"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The scheduler accepts the schedule string as a valid duration...
			dur, err := time.ParseDuration(tc.schedule)
			if err != nil {
				t.Fatalf("time.ParseDuration(%q) error = %v; the premise of this reproduction failed", tc.schedule, err)
			}
			if dur > 0 {
				t.Fatalf("time.ParseDuration(%q) = %v (positive); this reproduction needs a non-positive interval", tc.schedule, dur)
			}

			// ...and runTask then panics inside time.NewTicker.
			s := NewScheduler()
			st := &scheduledTask{
				ID:       3,
				Schedule: tc.schedule,
				Class:    ScooperJobClass,
				runner:   func() error { return nil },
				stopCh:   make(chan struct{}),
			}

			var recovered any
			func() {
				defer func() { recovered = recover() }()
				s.runTask(st, dur)
			}()

			if recovered == nil {
				t.Errorf("runTask with interval %v did not panic; the interval check was added - update this test", dur)
				return
			}
			// time.NewTicker panics with a plain string, not a runtime.Error.
			if msg := fmt.Sprint(recovered); !strings.Contains(msg, "non-positive interval") {
				t.Errorf("runTask panicked with %T (%v); want time.NewTicker's non-positive-interval panic", recovered, recovered)
			}
			t.Logf("BUG: runTask(%q -> %v) panics with %v at time.NewTicker (scheduler.go:141); launched from "+
				"scheduler.go:125 `go s.runTask(...)` it is an unrecoverable panic that kills the process",
				tc.schedule, dur, recovered)
		})
	}
}

// ---------------------------------------------------------------------------
// integration-shaped: the real dispatch code against a fake job source
// ---------------------------------------------------------------------------

// TestRefreshJobsDispatchesFromFakeJobSource drives the real refreshJobs against
// an in-memory tJobs source (see installFakeJobs) and asserts the dispatch
// rules: known class + valid duration starts a task; unknown class is skipped;
// a schedule the parser rejects is skipped before any runner starts.
func TestRefreshJobsDispatchesFromFakeJobSource(t *testing.T) {
	f := newSchedulerFixture(t)

	f.src.setJobs(
		jobRow(1, "1h", ScooperJobClass, true),                          // startable
		jobRow(2, "1h", "com.example.NoSuchTask", true),                 // unknown class
		jobRow(3, "*/30 * * * *", EligibilityJobClass, true),            // cron, not a duration
		jobRow(4, "", EligibilityJobClass, true),                        // empty schedule
		jobRow(5, "1h", "org.remitt.server.tasks.EligibiltyTask", true), // seeded typo
	)
	f.s.refreshJobs()

	if got := len(f.s.tasks); got != 1 {
		t.Fatalf("len(s.tasks) = %d; want 1: only job 1 has a known class and a duration schedule "+
			"(scheduler.go:105-115)", got)
	}

	st := f.task(1)
	if st == nil {
		t.Fatal("job 1 was not started")
	}
	if st.Schedule != "1h" {
		t.Errorf("task 1 Schedule = %q; want %q", st.Schedule, "1h")
	}
	if st.Class != ScooperJobClass {
		t.Errorf("task 1 Class = %q; want %q", st.Class, ScooperJobClass)
	}
	if st.runner == nil {
		t.Error("task 1 runner is nil; refreshJobs stored an unresolvable runner")
	}
	if st.stopCh == nil {
		t.Error("task 1 stopCh is nil; the task could never be stopped")
	}

	for _, id := range []int64{2, 3, 4, 5} {
		if _, ok := f.s.tasks[id]; ok {
			t.Errorf("job %d was started; want it skipped (unknown class or unparseable schedule)", id)
		}
	}
}

// TestRefreshJobsIsIdempotentForUnchangedSchedule covers the "No change" branch
// (scheduler.go:99-101): re-reading the same row must not restart the task.
func TestRefreshJobsIsIdempotentForUnchangedSchedule(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	f.s.refreshJobs()
	first := f.task(1)
	if first == nil {
		t.Fatal("job 1 was not started")
	}

	f.s.refreshJobs()
	if got := len(f.s.tasks); got != 1 {
		t.Fatalf("len(s.tasks) = %d after a second refresh; want 1", got)
	}
	if second := f.s.tasks[1]; second != first {
		t.Error("refreshJobs restarted an unchanged job (task pointer changed); scheduler.go:99-101 should have " +
			"taken the no-change branch")
	}
}

// TestRefreshJobsRestartsTaskWhenScheduleChanges covers scheduler.go:93-101: a
// changed jobSchedule retires the old task and starts a new one.
func TestRefreshJobsRestartsTaskWhenScheduleChanges(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	f.s.refreshJobs()
	first := f.task(1)
	if first == nil {
		t.Fatal("job 1 was not started")
	}

	f.src.setJobs(jobRow(1, "2h", ScooperJobClass, true))
	f.s.refreshJobs()

	if got := len(f.s.tasks); got != 1 {
		t.Fatalf("len(s.tasks) = %d after a schedule change; want 1", got)
	}
	second := f.task(1)
	if second == nil {
		t.Fatal("job 1 is missing after the schedule change")
	}
	if second == first {
		t.Error("the schedule change did not restart the task; scheduler.go:95-98 should have replaced it")
	}
	if second.Schedule != "2h" {
		t.Errorf("task 1 Schedule = %q; want %q", second.Schedule, "2h")
	}
	if first.Schedule != "1h" {
		t.Errorf("the retired task's Schedule = %q; want %q", first.Schedule, "1h")
	}
}

// TestRefreshJobsRemovesTasksNoLongerEnabled covers scheduler.go:129-136: a job
// that disappears from the enabled job set is dropped from the scheduler map.
func TestRefreshJobsRemovesTasksNoLongerEnabled(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	f.s.refreshJobs()
	if f.task(1) == nil {
		t.Fatal("job 1 was not started")
	}

	f.src.setJobs() // job disabled or deleted
	f.s.refreshJobs()

	if got := len(f.s.tasks); got != 0 {
		t.Errorf("len(s.tasks) = %d after the job was removed from tJobs; want 0", got)
	}
}

// ---------------------------------------------------------------------------
// the package's own runners
// ---------------------------------------------------------------------------

// TestRunScooperTaskIsANoOp is the only runner that completes without a
// database, and it documents what it actually does.
//
// FINDING (task/eligibility.go:97-101): the scheduled ScooperTask logs "scooper
// run complete (no plugins configured)" and returns nil, while the scooper
// package (scooper/map.go, scooper/sftp.go, scooper/gatewayedi.go) does have
// pluggable implementations. Scheduled scoops therefore never run, and the task
// reports success.
func TestRunScooperTaskIsANoOp(t *testing.T) {
	if err := RunScooperTask(); err != nil {
		t.Errorf("RunScooperTask() error = %v; want nil (the stub logs and returns nil)", err)
	}
}

// TestRefreshJobsWithoutDatabaseSkips follows the package convention
// (recover-and-skip, see eligibility/gatewayedi_test.go:192-196): refreshJobs
// reads tJobs through the package-level model.Queries handle, and with no
// database that handle is nil.
func TestRefreshJobsWithoutDatabaseSkips(t *testing.T) {
	if model.Queries != nil {
		t.Skipf("a live database is configured (model.Queries is non-nil); this test covers the no-database case")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Skipf("skipping: no MySQL database is available for tJobs (model.Queries is nil); refreshJobs "+
				"dereferences it at task/scheduler.go:69 and panics with %v instead of returning an error", r)
		}
	}()

	s := NewScheduler()
	s.refreshJobs()
	t.Log("refreshJobs completed without a database connection")
}

// TestSchedulerStartWithoutDatabaseSkips covers Start(), which calls refreshJobs
// synchronously (scheduler.go:44) before launching its ticker goroutine.
func TestSchedulerStartWithoutDatabaseSkips(t *testing.T) {
	if model.Queries != nil {
		t.Skipf("a live database is configured (model.Queries is non-nil); this test covers the no-database case")
	}

	s := NewScheduler()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		s.Start()
	}()

	// Start creates the ticker before calling refreshJobs, so it must always be
	// handed back; otherwise the runtime timer leaks into the test binary.
	if s.ticker != nil {
		s.ticker.Stop()
	}

	if recovered != nil {
		t.Skipf("skipping: no MySQL database is available for tJobs (model.Queries is nil); Start() panics at "+
			"task/scheduler.go:44 -> scheduler.go:69 with %v instead of returning an error", recovered)
	}
	s.Stop()
	t.Log("Start() succeeded against a live database and the scheduler was stopped again")
}

// TestRunEligibilityTaskWithoutDatabasePanics records, rather than skips, the
// behaviour of the DB-bound eligibility runner: it should return an error or be
// skipped, but it panics.
//
// BUG (task/eligibility.go:24): RunEligibilityTask reaches model.Queries through
// GetPendingEligibilityJobs with no nil guard, so with no database it panics on
// the nil sqlc handle instead of returning an error. The scheduler runs it from
// a goroutine (scheduler.go:125), where that panic is unrecoverable and takes
// the process down.
// This test pins the observed behaviour; it is not an endorsement of it.
func TestRunEligibilityTaskWithoutDatabasePanics(t *testing.T) {
	if model.Queries != nil {
		t.Skipf("a live database is configured (model.Queries is non-nil); this test covers the no-database case")
	}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		err := RunEligibilityTask()
		if err != nil {
			t.Logf("RunEligibilityTask() returned an error (%v) without a database; the nil-guard convention is "+
				"satisfied for this path", err)
		}
	}()

	if recovered == nil {
		return // an error, or a clean no-op: nothing to report
	}
	if _, ok := recovered.(runtime.Error); !ok {
		t.Errorf("RunEligibilityTask() panicked with %T (%v); want either an error return or a runtime nil-dereference", recovered, recovered)
	}
	t.Logf("BUG: RunEligibilityTask() panics with %v (task/eligibility.go:24, model.Queries is nil) instead of "+
		"returning an error; called from scheduler.go:125 in a goroutine that panic cannot be recovered", recovered)
}

// ---------------------------------------------------------------------------
// concurrency
// ---------------------------------------------------------------------------

// TestRunTaskConcurrentTicksNoRace runs two independent tick loops concurrently
// under -race and retires both deterministically. TaskRunner takes no context
// (scheduler.go:13) and the scheduler has no cancellation plumbing, so these
// tests drive the package's own stop channel instead of a context.
func TestRunTaskConcurrentTicksNoRace(t *testing.T) {
	s := NewScheduler()
	g := newLoopGuard()

	var calls1, calls2 atomic.Int64
	st1 := &scheduledTask{
		ID: 11, Schedule: "1ms", Class: ScooperJobClass,
		runner: func() error { calls1.Add(1); return nil }, stopCh: make(chan struct{}),
	}
	st2 := &scheduledTask{
		ID: 12, Schedule: "1ms", Class: EligibilityJobClass,
		runner: func() error { calls2.Add(1); return nil }, stopCh: make(chan struct{}),
	}

	done1 := startRunTask(s, st1, time.Millisecond)
	done2 := startRunTask(s, st2, time.Millisecond)

	defer func() {
		if !g.retire(t, s, st1, done1, 2*time.Second) {
			t.Errorf("task 11 did not return after being stopped")
		}
		if !g.retire(t, s, st2, done2, 2*time.Second) {
			t.Errorf("task 12 did not return after being stopped")
		}
	}()

	if !waitFor(2*time.Second, func() bool { return calls1.Load() >= 3 }) {
		t.Fatalf("task 11 ran %d time(s) in 2s at a 1ms interval; want at least 3", calls1.Load())
	}
	if !waitFor(2*time.Second, func() bool { return calls2.Load() >= 3 }) {
		t.Fatalf("task 12 ran %d time(s) in 2s at a 1ms interval; want at least 3", calls2.Load())
	}

	// Stop both loops through the production stop path, bounded; the deferred
	// guard hard-stops anything the best-effort send misses.
	if !g.retire(t, s, st1, done1, 2*time.Second) {
		t.Errorf("task 11 did not stop through the production stop path (scheduler.go:163)")
	}
	if !g.retire(t, s, st2, done2, 2*time.Second) {
		t.Errorf("task 12 did not stop through the production stop path (scheduler.go:163)")
	}
}

// TestRefreshJobsConcurrentTicksNoRace runs the scheduler's tick path
// (refreshJobs) from several goroutines while the tasks it started are ticking:
// the state it touches (s.tasks, s.mu) must stay race free and a job must not be
// started twice.
func TestRefreshJobsConcurrentTicksNoRace(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(
		jobRow(1, "1h", ScooperJobClass, true),
		jobRow(2, "1h", ScooperJobClass, true),
		jobRow(3, "*/30 * * * *", ScooperJobClass, true),
	)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.s.refreshJobs()
		}()
	}

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent refreshJobs calls did not finish within 5s")
	}

	// All writers have finished, so reading the map here is race free.
	if got := len(f.s.tasks); got != 2 {
		t.Errorf("len(s.tasks) = %d after three concurrent refreshes; want 2 (one task per startable job; no "+
			"duplicates from the concurrent refresh)", got)
	}
	f.task(1)
	f.task(2)
}

// TestSchedulerDoubleStartRaceRepro is the minimal reproduction for a data race
// in the scheduler lifecycle. It is opt-in because a `go test -race` run that
// trips the race detector aborts the whole test binary.
//
// BUG (task/scheduler.go:43 vs scheduler.go:49): Start writes s.ticker without
// synchronisation while a previously started ticker goroutine re-reads s.ticker
// in its select on every iteration, and the lifecycle is unguarded. Calling
// Start twice (double initialisation, or a restart after Stop) is therefore a
// data race on the ticker field.
//
// Reproduce with:
//
//	REMITT_TASK_RACE_REPRO=1 go test -race -count=1 -run TestSchedulerDoubleStartRaceRepro -v ./task/
func TestSchedulerDoubleStartRaceRepro(t *testing.T) {
	if os.Getenv("REMITT_TASK_RACE_REPRO") == "" {
		t.Skip("opt-in reproduction: set REMITT_TASK_RACE_REPRO=1 and run with -race to observe the " +
			"unsynchronised write/read of s.ticker (scheduler.go:43 vs scheduler.go:49)")
	}

	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	f.s.Start() // starts a ticker goroutine that reads s.ticker every iteration
	time.Sleep(20 * time.Millisecond)
	f.s.Start() // rewrites s.ticker while that goroutine is reading it
	time.Sleep(20 * time.Millisecond)

	f.s.Stop()
	time.Sleep(50 * time.Millisecond)
}
