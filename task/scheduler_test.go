package task

// DB-free test suite for the task (job scheduler) package.
//
// Environment: this repository has no test database and model.Queries is a nil
// *dbgen.Queries without a live MySQL connection, so every code path that reads
// tJobs has to nil-guard (which refreshJobs and RunEligibilityTask now do). The
// strategy is therefore:
//
//  1. Exercise the pure logic directly: the job-class dispatch table
//     (resolveRunner), schedule parsing, and the next-fire-time computation
//     (parseSchedule, nextFire), which is what the scheduler loop is built from.
//  2. Exercise the real dispatch code (refreshJobs) against an in-memory
//     database/sql driver installed on model.Queries. The scheduler has no
//     injection seam for the job source, so swapping that global is the only way
//     to reach the code without MySQL.
//  3. Skip, naming the missing dependency, the DB-bound paths that have no fake
//     (the convention already used by eligibility/gatewayedi_test.go and
//     api/api_test.go).
//
// Cron expectations in TestNextFireMatchesCron4j are not hand-written: they were
// produced by running cron4j itself (it.sauronsoftware.cron4j:cron4j:2.2.5, the
// dependency declared in ../remitt/pom.xml and used by
// MasterControl.java:213) - `new Predictor(new SchedulingPattern(p), base)`
// followed by three nextMatchingTime() calls, with base =
// 2026-09-15T12:28:22Z in UTC. cron4j is the contract authority for the
// tJobs.jobSchedule column, so where its behaviour is surprising (a step indexes
// the value list rather than the value itself; day-of-month AND day-of-week
// both have to match; "0-7" in the day-of-week field means Sunday only) the
// expectations below encode cron4j's answer, not a generic cron's.

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

// cronReferenceTime is the reference instant every cron expectation below is
// relative to: the instant the cron4j oracle was run from.
var cronReferenceTime = time.Date(2026, 9, 15, 12, 28, 22, 0, time.UTC)

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

// startRunTask runs the scheduler's task loop in its own goroutine and returns a
// channel closed when the loop has returned.
func startRunTask(s *Scheduler, st *scheduledTask) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runTask(st)
	}()
	return done
}

// stopAndWait signals a task through the production stop path (stopTask) and
// waits for the task loop to return. Bounded, so a broken stop path fails the
// test instead of hanging the suite.
func stopAndWait(t *testing.T, s *Scheduler, st *scheduledTask, done <-chan struct{}, timeout time.Duration) bool {
	t.Helper()
	if done != nil && doneClosed(done) {
		return true
	}
	s.stopTask(st)
	if done == nil {
		return true
	}
	if !waitFor(timeout, func() bool { return doneClosed(done) }) {
		t.Errorf("task %d did not return within %s of a stop request through stopTask", st.ID, timeout)
		return false
	}
	return true
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
// restores the previous handle when the test ends.
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

// schedulerFixture couples a fake job source with a scheduler and signals every
// task it started to stop, so no test leaves a task loop running.
type schedulerFixture struct {
	t        *testing.T
	s        *Scheduler
	src      *fakeJobsSource
	baseline int

	mu      sync.Mutex
	tracked []*scheduledTask
}

func newSchedulerFixture(t *testing.T) *schedulerFixture {
	t.Helper()

	f := &schedulerFixture{
		t:        t,
		s:        NewScheduler(),
		src:      installFakeJobs(t),
		baseline: runtime.NumGoroutine(),
	}
	t.Cleanup(f.stop)
	return f
}

// track remembers a task the scheduler started so the fixture can stop it.
func (f *schedulerFixture) track(st *scheduledTask) *scheduledTask {
	if st == nil {
		return nil
	}
	f.mu.Lock()
	f.tracked = append(f.tracked, st)
	f.mu.Unlock()
	return st
}

// task returns the scheduler's current task for id (same package access, under
// the scheduler's lock so it is race free while the scheduler loop runs).
func (f *schedulerFixture) task(id int64) *scheduledTask {
	f.s.mu.Lock()
	st := f.s.tasks[id]
	f.s.mu.Unlock()
	return f.track(st)
}

// taskCount returns the number of tasks the scheduler currently holds.
func (f *schedulerFixture) taskCount() int {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return len(f.s.tasks)
}

// ticker returns the scheduler's refresh ticker, under the lock, so reading it
// while the scheduler loop runs is race free.
func (f *schedulerFixture) ticker() *time.Ticker {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.ticker
}

// running reports the scheduler's lifecycle state.
func (f *schedulerFixture) running() bool {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.running
}

// stop signals every task the fixture saw. It runs as a t.Cleanup so a failing
// assertion cannot leave a task loop behind: delivery is reliable now, so a
// single signalStop per task is enough.
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
		st.signalStop()
	}

	if !waitFor(500*time.Millisecond, func() bool { return runtime.NumGoroutine() <= f.baseline }) {
		// Diagnostics only: report, never fail, so the suite stays deterministic.
		fmt.Printf("task tests: %d goroutine(s) still running after cleanup (baseline %d)\n",
			runtime.NumGoroutine(), f.baseline)
	}
}

// ---------------------------------------------------------------------------
// seed data (migrations/001_legacy.up.sql) and the data fix in 002
// ---------------------------------------------------------------------------

type seedJobRow struct {
	id       int64
	schedule string
	class    string
	enabled  bool
}

// seededTypoClass is the misspelled job class 001_legacy.up.sql seeds for the
// eligibility job.
const seededTypoClass = "org.remitt.server.tasks.EligibiltyTask"

var seedJobRowRe = regexp.MustCompile(`\(\s*(\d+)\s*,\s*'([^']*)'\s*,\s*'([^']*)'\s*,\s*(TRUE|FALSE)\s*\)`)

// loadSeedJobs reads the tJobs seed rows shipped in the initial migration.
func loadSeedJobs(t *testing.T) []seedJobRow {
	t.Helper()

	const rel = "../migrations/001_legacy.up.sql"
	data, err := os.ReadFile(filepath.Join("..", "migrations", "001_legacy.up.sql"))
	if err != nil {
		t.Fatalf("cannot read the tJobs seed migration %s: %v", rel, err)
	}

	text := string(data)
	start := strings.Index(text, "INSERT INTO tJobs VALUES")
	if start < 0 {
		t.Fatalf("no 'INSERT INTO tJobs VALUES' block in %s", rel)
	}
	end := strings.Index(text[start:], ";")
	if end < 0 {
		end = len(text) - start
	}

	matches := seedJobRowRe.FindAllStringSubmatch(text[start:start+end], -1)
	if len(matches) == 0 {
		t.Fatalf("no tJobs rows matched in %s", rel)
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

// jobClassFix is the class-name rewrite a data-fix migration performs, read out
// of the migration file itself so the test exercises the statement that will run
// against a real database rather than a copy of it.
type jobClassFix struct {
	from string
	to   string
}

var jobClassFixRe = regexp.MustCompile(
	`(?i)UPDATE\s+` + "`?" + `tJobs` + "`?" +
		`\s+SET\s+` + "`?" + `jobClass` + "`?" + `\s*=\s*'([^']*)'` +
		`\s+WHERE\s+` + "`?" + `jobClass` + "`?" + `\s*=\s*'([^']*)'`)

// loadJobClassFix extracts the jobClass rewrite from a migration file.
func loadJobClassFix(t *testing.T, file string) jobClassFix {
	t.Helper()

	path := filepath.Join("..", "migrations", file)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read the migration %s: %v", path, err)
	}

	matches := jobClassFixRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatalf("%s contains no 'UPDATE tJobs SET jobClass = ... WHERE jobClass = ...' statement", path)
	}
	// Last match wins if a migration rewrites the same column more than once.
	last := matches[len(matches)-1]
	return jobClassFix{from: last[2], to: last[1]}
}

// applyJobClassFix applies a migration's rewrite to in-memory tJobs rows.
func applyJobClassFix(rows []seedJobRow, fix jobClassFix) []seedJobRow {
	out := make([]seedJobRow, len(rows))
	copy(out, rows)
	for i := range out {
		if out[i].class == fix.from {
			out[i].class = fix.to
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// pure logic: construction, dispatch table
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
		t.Error("NewScheduler(): stopCh is nil; Stop() could not close it")
	}
	if s.running {
		t.Error("NewScheduler(): running is true before Start()")
	}

	// stopCh must be open (Stop closes the channel of a running scheduler).
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
// scheduler dispatches on. Function identity is compared by code pointer because
// function values are not comparable.
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

		// The class name 001_legacy.up.sql seeded ("EligibiltyTask", missing the
		// second 'i') does not match the constant above. It is fixed in DATA by
		// migrations/002_fix_seed_job_class.up.sql - there is deliberately no
		// fuzzy fallback, so a mistyped class name still resolves to nothing and
		// refreshJobs reports it instead of running the wrong task.
		{"seed_typo_class_still_unresolvable", seededTypoClass, 0},
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

// ---------------------------------------------------------------------------
// schedule parsing: durations and cron4j patterns
// ---------------------------------------------------------------------------

// TestScheduleAcceptsDurationsAndCron pins which jobSchedule values are
// schedulable. Both forms occur in the schema: tJobs.jobClass holds the Java
// task class, and the Java implementation parsed the same column with cron4j
// (MasterControl.java:213), so a cron pattern must be accepted; rows written for
// the Go server use Go duration strings, which must keep working.
func TestScheduleAcceptsDurationsAndCron(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
		wantCron bool
		wantDur  time.Duration
	}{
		{"seconds", "30s", false, 30 * time.Second},
		{"minutes", "90m", false, 90 * time.Minute},
		{"hours", "1h", false, time.Hour},
		{"compound", "1h30m", false, 90 * time.Minute},
		{"milliseconds", "500ms", false, 500 * time.Millisecond},
		{"day_as_24h", "24h", false, 24 * time.Hour},

		{"seed_scooper_every_minute", "* * * * *", true, 0},
		{"seed_eligibility_every_30_minutes", "*/30 * * * *", true, 0},
		{"cron_daily_3am", "0 3 * * *", true, 0},
		{"cron_with_names", "30 8 1 jan mon", true, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseSchedule(tc.schedule)
			if err != nil {
				t.Fatalf("parseSchedule(%q) error = %v; want it accepted", tc.schedule, err)
			}
			if parsed.isCron() != tc.wantCron {
				t.Errorf("parseSchedule(%q).isCron() = %v; want %v", tc.schedule, parsed.isCron(), tc.wantCron)
			}
			if tc.wantCron {
				return
			}
			if parsed.interval != tc.wantDur {
				t.Errorf("parseSchedule(%q).interval = %s; want %s", tc.schedule, parsed.interval, tc.wantDur)
			}
		})
	}
}

// TestScheduleRejections pins the values that must not schedule a job, and that
// the rejection carries an explanation: refreshJobs logs the reason it rejected
// a row, so a bad schedule is visible instead of silently skipped.
func TestScheduleRejections(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
		wantErr  string
	}{
		{"empty", "", "empty schedule"},
		{"whitespace", " ", "empty schedule"},
		{"garbage", "garbage", "neither a cron4j cron pattern"},
		{"prose", "5 minutes", "requires 5"},
		{"four_fields", "* * * *", "requires 5"},
		{"six_fields", "* * * * * *", "requires 5"},
		{"cron_nickname", "@every 1h", "requires 5"},
		{"quartz_question_mark", "0 0 1 * ?", "invalid value"},
		{"quartz_last_day_of_week", "0 0 * * 1L", "invalid value"},
		{"quartz_nth_weekday", "0 0 * * 1#2", "invalid value"},
		{"zero_step", "*/0 * * * *", "step must be >= 1"},
		{"negative_step", "*/-3 * * * *", "step must be >= 1"},
		{"minute_out_of_range", "60 * * * *", "out of the minute range"},
		{"hour_out_of_range", "0 24 * * *", "out of the hour range"},
		{"day_of_month_out_of_range", "0 0 0 * *", "out of the day of month range"},
		{"month_out_of_range", "0 0 * 13 *", "invalid value"},
		{"day_of_week_out_of_range", "0 0 * * 8", "invalid value"},
		{"unknown_month_name", "0 0 * january *", "invalid value"},
		{"unknown_day_name", "0 0 * * funday", "invalid value"},
		{"empty_list_element", "0 0 1,,2 * *", "empty list element"},
		{"zero_duration", "0s", "non-positive duration"},
		{"zero_bare", "0", "non-positive duration"},
		{"negative_duration", "-5m", "non-positive duration"},
		{"days_duration", "1d", "neither a cron4j cron pattern"},
		{"trailing_newline", "1h\n", "neither a cron4j cron pattern"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseSchedule(tc.schedule)
			if err == nil {
				t.Fatalf("parseSchedule(%q) = %s, nil; want a rejection", tc.schedule, parsed.describe())
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("parseSchedule(%q) error = %q; want it to mention %q", tc.schedule, err.Error(), tc.wantErr)
			}
		})
	}
}

// TestNextFireMatchesCron4j is the schedule engine's oracle test. Every row was
// produced by cron4j 2.2.5 itself (see the file header): three successive
// Predicator.nextMatchingTime() values starting from 2026-09-15T12:28:22Z, i.e.
// the next three instants strictly after the running fire time.
//
// The awkward rows are the point: cron4j steps over the enumerated value list
// ("0/6" in the hour field is the hour 0 alone, not 0,6,12,18), ANDs
// day-of-month with day-of-week ("0 0 13 * 5" is Friday the 13th), treats "0-7"
// in the day-of-week field as Sunday only (both endpoints normalise through
// % 7), wraps descending ranges ("fri-mon"), and accepts "L" for the last day of
// the month.
func TestNextFireMatchesCron4j(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
		want     []string
	}{
		{"every_minute", "* * * * *", []string{
			"2026-09-15T12:29:00Z", "2026-09-15T12:30:00Z", "2026-09-15T12:31:00Z"}},
		{"every_thirty_minutes", "*/30 * * * *", []string{
			"2026-09-15T12:30:00Z", "2026-09-15T13:00:00Z", "2026-09-15T13:30:00Z"}},
		{"every_five_minutes", "*/5 * * * *", []string{
			"2026-09-15T12:30:00Z", "2026-09-15T12:35:00Z", "2026-09-15T12:40:00Z"}},
		{"daily_three_am", "0 3 * * *", []string{
			"2026-09-16T03:00:00Z", "2026-09-17T03:00:00Z", "2026-09-18T03:00:00Z"}},
		{"daily_one_am", "0 1 * * *", []string{
			"2026-09-16T01:00:00Z", "2026-09-17T01:00:00Z", "2026-09-18T01:00:00Z"}},
		{"first_of_month", "0 0 1 * *", []string{
			"2026-10-01T00:00:00Z", "2026-11-01T00:00:00Z", "2026-12-01T00:00:00Z"}},
		{"month_end_day", "0 0 31 * *", []string{
			"2026-10-31T00:00:00Z", "2026-12-31T00:00:00Z", "2027-01-31T00:00:00Z"}},
		{"last_day_of_month", "0 0 L * *", []string{
			"2026-09-30T00:00:00Z", "2026-10-31T00:00:00Z", "2026-11-30T00:00:00Z"}},
		{"day_list_including_last_day", "0 0 1,L * *", []string{
			"2026-09-30T00:00:00Z", "2026-10-01T00:00:00Z", "2026-10-31T00:00:00Z"}},
		{"day_range", "0 0 28-31 * *", []string{
			"2026-09-28T00:00:00Z", "2026-09-29T00:00:00Z", "2026-09-30T00:00:00Z"}},
		{"day_step_over_list", "0 0 */2 * *", []string{
			"2026-09-17T00:00:00Z", "2026-09-19T00:00:00Z", "2026-09-21T00:00:00Z"}},

		{"sunday_as_zero", "0 0 * * 0", []string{
			"2026-09-20T00:00:00Z", "2026-09-27T00:00:00Z", "2026-10-04T00:00:00Z"}},
		{"sunday_as_seven", "0 0 * * 7", []string{
			"2026-09-20T00:00:00Z", "2026-09-27T00:00:00Z", "2026-10-04T00:00:00Z"}},
		{"sunday_as_name", "0 0 * * sun", []string{
			"2026-09-20T00:00:00Z", "2026-09-27T00:00:00Z", "2026-10-04T00:00:00Z"}},
		{"day_name_uppercase", "0 0 * * MON", []string{
			"2026-09-21T00:00:00Z", "2026-09-28T00:00:00Z", "2026-10-05T00:00:00Z"}},
		{"weekday_list", "0 0 * * 1,3,5", []string{
			"2026-09-16T00:00:00Z", "2026-09-18T00:00:00Z", "2026-09-21T00:00:00Z"}},
		{"weekday_list_with_names", "0 0 * * 5,6,0", []string{
			"2026-09-18T00:00:00Z", "2026-09-19T00:00:00Z", "2026-09-20T00:00:00Z"}},
		{"weekday_range", "0 0 * * 1-5", []string{
			"2026-09-16T00:00:00Z", "2026-09-17T00:00:00Z", "2026-09-18T00:00:00Z"}},
		{"weekday_range_with_names", "0 0 * * mon-fri", []string{
			"2026-09-16T00:00:00Z", "2026-09-17T00:00:00Z", "2026-09-18T00:00:00Z"}},
		{"weekday_wrapping_range", "0 0 * * fri-mon", []string{
			"2026-09-18T00:00:00Z", "2026-09-19T00:00:00Z", "2026-09-20T00:00:00Z"}},
		// cron4j normalises both range endpoints through % 7, so "0-7" collapses
		// to the single value 0 (Sunday) rather than meaning "every day".
		{"weekday_zero_to_seven_is_sunday", "0 0 * * 0-7", []string{
			"2026-09-20T00:00:00Z", "2026-09-27T00:00:00Z", "2026-10-04T00:00:00Z"}},
		{"saturday_late", "45 23 * * 6", []string{
			"2026-09-19T23:45:00Z", "2026-09-26T23:45:00Z", "2026-10-03T23:45:00Z"}},
		{"weekend_noon", "0 12 * * 6,0", []string{
			"2026-09-19T12:00:00Z", "2026-09-20T12:00:00Z", "2026-09-26T12:00:00Z"}},

		{"month_name_with_day", "30 8 1 jan *", []string{
			"2027-01-01T08:30:00Z", "2028-01-01T08:30:00Z", "2029-01-01T08:30:00Z"}},
		{"month_name_range", "0 0 * jan-mar *", []string{
			"2027-01-01T00:00:00Z", "2027-01-02T00:00:00Z", "2027-01-03T00:00:00Z"}},
		{"month_uppercase_name", "0 0 * Jan *", []string{
			"2027-01-01T00:00:00Z", "2027-01-02T00:00:00Z", "2027-01-03T00:00:00Z"}},
		{"month_number_with_day", "5 0 * 8 *", []string{
			"2027-08-01T00:05:00Z", "2027-08-02T00:05:00Z", "2027-08-03T00:05:00Z"}},
		{"leap_day_only", "0 0 29 feb *", []string{
			"2028-02-29T00:00:00Z", "2032-02-29T00:00:00Z", "2036-02-29T00:00:00Z"}},

		// Both day fields restricted: cron4j requires BOTH to match.
		{"friday_the_thirteenth", "0 0 13 * 5", []string{
			"2026-11-13T00:00:00Z", "2027-08-13T00:00:00Z", "2028-10-13T00:00:00Z"}},
		{"first_monday_of_month", "0 0 1-7 * 1", []string{
			"2026-10-05T00:00:00Z", "2026-11-02T00:00:00Z", "2026-12-07T00:00:00Z"}},
		{"fifteenth_falling_on_wednesday", "0 0 15 * 3", []string{
			"2027-09-15T00:00:00Z", "2027-12-15T00:00:00Z", "2028-03-15T00:00:00Z"}},

		// Steps index the enumerated value list, so "0/6" in the hour field
		// selects hour 0 alone while "6,12,18" selects three hours.
		{"hour_index_step_is_single_hour", "0 0/6 * * *", []string{
			"2026-09-16T00:00:00Z", "2026-09-17T00:00:00Z", "2026-09-18T00:00:00Z"}},
		{"hour_list", "0 6,12,18 * * *", []string{
			"2026-09-15T18:00:00Z", "2026-09-16T06:00:00Z", "2026-09-16T12:00:00Z"}},
		{"minute_index_step_inside_hour", "*/15 2 * * *", []string{
			"2026-09-16T02:00:00Z", "2026-09-16T02:15:00Z", "2026-09-16T02:30:00Z"}},
		{"minute_range_step", "1-30/7 * * * *", []string{
			"2026-09-15T12:29:00Z", "2026-09-15T13:01:00Z", "2026-09-15T13:08:00Z"}},
		{"hour_wrapping_range", "0 22-2 * * *", []string{
			"2026-09-15T22:00:00Z", "2026-09-15T23:00:00Z", "2026-09-16T00:00:00Z"}},
		{"hour_wrapping_range_with_step", "0 22-2/2 * * *", []string{
			"2026-09-15T22:00:00Z", "2026-09-16T00:00:00Z", "2026-09-16T02:00:00Z"}},
		{"minute_wrapping_range", "50-10 * * * *", []string{
			"2026-09-15T12:50:00Z", "2026-09-15T12:51:00Z", "2026-09-15T12:52:00Z"}},
		{"hour_step_over_weekdays", "0 9-17/4 * * 1-5", []string{
			"2026-09-15T13:00:00Z", "2026-09-15T17:00:00Z", "2026-09-16T09:00:00Z"}},

		// The value 32 is a plain value in every field except day of month,
		// where it is cron4j's internal representation of "L": treating it as
		// the sentinel everywhere silently drops minute 32.
		{"minute_thirty_two", "32 * * * *", []string{
			"2026-09-15T12:32:00Z", "2026-09-15T13:32:00Z", "2026-09-15T14:32:00Z"}},
		{"minute_step_landing_on_thirty_two", "*/32 * * * *", []string{
			"2026-09-15T12:32:00Z", "2026-09-15T13:00:00Z", "2026-09-15T13:32:00Z"}},
		{"minute_and_hour_lists", "2,32 4,16 * * *", []string{
			"2026-09-15T16:02:00Z", "2026-09-15T16:32:00Z", "2026-09-16T04:02:00Z"}},

		// Day-of-month ranges, including a descending one and one built from "L".
		{"day_wrapping_range", "0 0 31-1 * *", []string{
			"2026-10-01T00:00:00Z", "2026-10-31T00:00:00Z", "2026-11-01T00:00:00Z"}},
		{"day_range_from_last_day_token", "0 0 L-31 * *", []string{
			"2026-09-16T00:00:00Z", "2026-09-17T00:00:00Z", "2026-09-18T00:00:00Z"}},
		{"last_day_of_february", "0 0 L 2 *", []string{
			"2027-02-28T00:00:00Z"}},
		{"last_day_of_month_late", "31 23 L * *", []string{
			"2026-09-30T23:31:00Z", "2026-10-31T23:31:00Z", "2026-11-30T23:31:00Z"}},
		{"month_step", "0 0 1 */3 *", []string{
			"2026-10-01T00:00:00Z", "2027-01-01T00:00:00Z", "2027-04-01T00:00:00Z"}},
		{"month_and_day_list", "0 12 1 1,7 *", []string{
			"2027-01-01T12:00:00Z", "2027-07-01T12:00:00Z"}},
		{"day_name_list", "15 3 * * sun,wed,fri", []string{
			"2026-09-16T03:15:00Z", "2026-09-18T03:15:00Z", "2026-09-20T03:15:00Z"}},
		{"weekday_wrapping_range_saturday_to_sunday", "0 0 * * 6-0", []string{
			"2026-09-19T00:00:00Z", "2026-09-20T00:00:00Z", "2026-09-26T00:00:00Z"}},

		// "|" joins alternatives: the task fires at the earliest matching time.
		{"alternatives", "0 0 * * * | 30 1 * * *", []string{
			"2026-09-16T00:00:00Z", "2026-09-16T01:30:00Z", "2026-09-17T00:00:00Z"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			after := cronReferenceTime
			for step, want := range tc.want {
				got, err := nextFire(tc.schedule, after)
				if err != nil {
					t.Fatalf("nextFire(%q, %s) step %d: %v", tc.schedule, after.Format(time.RFC3339), step, err)
				}
				if formatted := got.UTC().Format(time.RFC3339); formatted != want {
					t.Errorf("nextFire(%q, %s) step %d = %s; want %s (cron4j 2.2.5)",
						tc.schedule, after.Format(time.RFC3339), step, formatted, want)
				}
				after = got
			}
		})
	}
}

// TestNextFireForDurations covers the duration form of the same computation.
func TestNextFireForDurations(t *testing.T) {
	cases := []struct {
		schedule string
		interval time.Duration
	}{
		{"30s", 30 * time.Second},
		{"1h", time.Hour},
		{"90m", 90 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.schedule, func(t *testing.T) {
			got, err := nextFire(tc.schedule, cronReferenceTime)
			if err != nil {
				t.Fatalf("nextFire(%q, ...) error = %v", tc.schedule, err)
			}
			want := cronReferenceTime.Add(tc.interval)
			if !got.Equal(want) {
				t.Errorf("nextFire(%q, %s) = %s; want %s", tc.schedule, cronReferenceTime, got, want)
			}
		})
	}
}

// TestNextFireForNeverMatchingPatternIsBounded pins the horizon: cron4j's own
// Predictor spins forever on a pattern that cannot match, the scheduler must
// report it instead.
func TestNextFireForNeverMatchingPatternIsBounded(t *testing.T) {
	// The schedule parses (day 30 and February are both valid values) but never
	// matches, so it has no next fire time.
	parsed, err := parseSchedule("0 0 30 feb *")
	if err != nil {
		t.Fatalf("parseSchedule(\"0 0 30 feb *\") error = %v; want it to parse", err)
	}
	if next, ok := parsed.next(cronReferenceTime); ok {
		t.Errorf("parsed.next(%s) = %s, true; want no matching time", cronReferenceTime, next)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = nextFire("0 0 30 feb *", cronReferenceTime)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("nextFire on a never-matching pattern did not return within 5s; the search is unbounded")
	}
}

// ---------------------------------------------------------------------------
// the seeded jobs
// ---------------------------------------------------------------------------

// TestSeedJobSchedulesAreHonored replaces the characterization test that pinned
// the old mismatch: both rows shipped in migrations/001_legacy.up.sql carry cron
// patterns (`* * * * *` and `*/30 * * * *`), which the scheduler now has to
// parse - and they must compute the same fire times cron4j did.
func TestSeedJobSchedulesAreHonored(t *testing.T) {
	rows := loadSeedJobs(t)
	if len(rows) != 2 {
		t.Fatalf("len(seed tJobs rows) = %d; want 2", len(rows))
	}

	// The exact fire times cron4j 2.2.5 computes for the two seeded schedules,
	// from the shared reference instant.
	expectedNext := map[int64]string{
		1: "2026-09-15T12:29:00Z", // '* * * * *'  - every minute on the minute
		2: "2026-09-15T12:30:00Z", // '*/30 * * * *' - minutes 0 and 30
	}

	for _, row := range rows {
		row := row
		t.Run(fmt.Sprintf("job_%d", row.id), func(t *testing.T) {
			if !row.enabled {
				t.Fatalf("seed job %d is disabled; the scheduler would not read it", row.id)
			}

			parsed, err := parseSchedule(row.schedule)
			if err != nil {
				t.Fatalf("the scheduler rejects the seeded jobSchedule %q of job %d: %v", row.schedule, row.id, err)
			}
			if !parsed.isCron() {
				t.Fatalf("seeded jobSchedule %q of job %d parsed as %s; want a cron pattern",
					row.schedule, row.id, parsed.describe())
			}

			next, err := nextFire(row.schedule, cronReferenceTime)
			if err != nil {
				t.Fatalf("nextFire(%q, %s) error = %v", row.schedule, cronReferenceTime, err)
			}
			want, ok := expectedNext[row.id]
			if !ok {
				t.Fatalf("no expected fire time recorded for seed job %d", row.id)
			}
			if got := next.UTC().Format(time.RFC3339); got != want {
				t.Errorf("nextFire(%q) = %s; want %s (cron4j 2.2.5)", row.schedule, got, want)
			}
			if next.Second() != 0 {
				t.Errorf("nextFire(%q) = %s; want a fire time aligned to the minute", row.schedule, next)
			}
		})
	}
}

// TestSeedJobClassMigrationFixesTheTypo covers the data fix: the seeded class
// name is misspelled, the Go dispatch table (and the Java class) spell it
// correctly, and migrations/002_fix_seed_job_class.up.sql has to rewrite the
// stored value to the correct spelling - with a down migration that restores it.
func TestSeedJobClassMigrationFixesTheTypo(t *testing.T) {
	seeds := loadSeedJobs(t)
	up := loadJobClassFix(t, "002_fix_seed_job_class.up.sql")
	down := loadJobClassFix(t, "002_fix_seed_job_class.down.sql")

	if up.from != seededTypoClass {
		t.Errorf("the up migration rewrites %q; want the seeded misspelling %q", up.from, seededTypoClass)
	}
	if up.to != EligibilityJobClass {
		t.Errorf("the up migration rewrites to %q; want EligibilityJobClass (%q)", up.to, EligibilityJobClass)
	}
	if down.from != up.to || down.to != up.from {
		t.Errorf("the down migration (%q -> %q) does not reverse the up migration (%q -> %q)",
			down.from, down.to, up.from, up.to)
	}

	// The seeds really do carry the misspelling; otherwise this test proves
	// nothing about the migration.
	typoRows := 0
	for _, row := range seeds {
		if row.class == seededTypoClass {
			typoRows++
		}
	}
	if typoRows == 0 {
		t.Fatalf("no seeded tJobs row carries the misspelled class %q; the migration under test fixes nothing", seededTypoClass)
	}

	s := NewScheduler()
	for _, row := range seeds {
		if !strings.HasSuffix(row.class, "Task") {
			continue
		}
		if s.resolveRunner(row.class) == nil && row.class != seededTypoClass {
			t.Errorf("seeded job %d already has an unresolvable class %q; the seed data drifted", row.id, row.class)
		}
	}

	fixed := applyJobClassFix(seeds, up)
	for _, row := range fixed {
		if !strings.HasSuffix(row.class, "Task") {
			continue
		}
		if s.resolveRunner(row.class) == nil {
			t.Errorf("after the migration seeded job %d still has no runner for class %q", row.id, row.class)
		}
	}

	if restored := applyJobClassFix(fixed, down); !reflect.DeepEqual(restored, seeds) {
		t.Errorf("applying the down migration to the fixed rows gives %v; want the seeded rows %v", restored, seeds)
	}

	// No fuzzy matching was added: the misspelling still resolves to nothing.
	if s.resolveRunner(seededTypoClass) != nil {
		t.Errorf("resolveRunner(%q) resolved a runner; the typo must be fixed in data, not by loosening the lookup", seededTypoClass)
	}
}

// TestSeededJobsStartAfterTheMigrationFix is the end-to-end form: the seeded
// rows, with the class fix applied, must all start as tasks, with no rejection
// reported by refreshJobs.
func TestSeededJobsStartAfterTheMigrationFix(t *testing.T) {
	seeds := loadSeedJobs(t)
	fixed := applyJobClassFix(seeds, loadJobClassFix(t, "002_fix_seed_job_class.up.sql"))

	f := newSchedulerFixture(t)
	rows := make([][]driver.Value, 0, len(fixed))
	for _, row := range fixed {
		rows = append(rows, jobRow(row.id, row.schedule, row.class, row.enabled))
	}
	f.src.setJobs(rows...)

	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("refreshJobs() with the fixed seed rows = %v; want no rejected rows", err)
	}
	if got := f.taskCount(); got != 2 {
		t.Fatalf("len(s.tasks) = %d after loading the fixed seed rows; want 2 (both seeded jobs scheduled)", got)
	}

	for _, row := range fixed {
		st := f.task(row.id)
		if st == nil {
			t.Fatalf("seeded job %d was not started", row.id)
		}
		if st.runner == nil {
			t.Errorf("seeded job %d started with a nil runner", row.id)
		}
		if st.schedule == nil || !st.schedule.isCron() {
			t.Errorf("seeded job %d started with schedule %q, which did not parse as a cron pattern", row.id, st.Schedule)
		}
		if st.stopCh == nil {
			t.Errorf("seeded job %d started without a stop channel", row.id)
		}
	}
}

// TestSeededJobsBeforeTheFixAreReportedNotRun is the other half: the unfixed
// seed data (the misspelled class) must be reported loudly, not scheduled, and
// the row that is fine must still start.
func TestSeededJobsBeforeTheFixAreReportedNotRun(t *testing.T) {
	seeds := loadSeedJobs(t)

	f := newSchedulerFixture(t)
	rows := make([][]driver.Value, 0, len(seeds))
	for _, row := range seeds {
		rows = append(rows, jobRow(row.id, row.schedule, row.class, row.enabled))
	}
	f.src.setJobs(rows...)

	err := f.s.refreshJobs()
	if err == nil {
		t.Fatal("refreshJobs() on the unfixed seed rows = nil; want the misspelled class reported as a rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown job class") {
		t.Errorf("refreshJobs() error = %q; want it to report an unknown job class", msg)
	}
	if !strings.Contains(msg, seededTypoClass) {
		t.Errorf("refreshJobs() error = %q; want it to name the misspelled class %q", msg, seededTypoClass)
	}
	if !strings.Contains(msg, EligibilityJobClass) {
		t.Errorf("refreshJobs() error = %q; want it to list the registered classes", msg)
	}

	// The correctly spelled ScooperTask row is unaffected.
	if f.task(1) == nil {
		t.Error("the correctly spelled seeded job 1 was not started")
	}
	if _, ok := func() (*scheduledTask, bool) {
		f.s.mu.Lock()
		defer f.s.mu.Unlock()
		st, ok := f.s.tasks[2]
		return st, ok
	}(); ok {
		t.Error("the misspelled seeded job 2 was started; an unresolvable class must not schedule anything")
	}
}

// ---------------------------------------------------------------------------
// the run loops
// ---------------------------------------------------------------------------

// TestRunTaskDurationRunsImmediatelyAndOnEveryTick exercises the duration loop
// with an injected runner, so no database is involved.
func TestRunTaskDurationRunsImmediatelyAndOnEveryTick(t *testing.T) {
	s := NewScheduler()

	var calls atomic.Int64
	st, err := newScheduledTask(1, "5ms", ScooperJobClass, func() error { calls.Add(1); return nil })
	if err != nil {
		t.Fatalf("newScheduledTask: %v", err)
	}
	done := startRunTask(s, st)
	defer stopAndWait(t, s, st, done, 2*time.Second)

	if !waitFor(2*time.Second, func() bool { return calls.Load() >= 1 }) {
		t.Fatal("the runner was never invoked; a duration schedule runs once immediately")
	}
	if !waitFor(2*time.Second, func() bool { return calls.Load() >= 3 }) {
		t.Fatalf("runner calls after ~2s at a 5ms interval = %d; want at least 3", calls.Load())
	}
}

// TestRunTaskKeepsTickingAfterRunnerError checks that a failing runner does not
// abort the duration loop: the error is logged and the loop keeps ticking.
func TestRunTaskKeepsTickingAfterRunnerError(t *testing.T) {
	s := NewScheduler()

	var calls atomic.Int64
	st, err := newScheduledTask(7, "5ms", EligibilityJobClass, func() error {
		calls.Add(1)
		return fmt.Errorf("injected failure")
	})
	if err != nil {
		t.Fatalf("newScheduledTask: %v", err)
	}
	done := startRunTask(s, st)
	defer stopAndWait(t, s, st, done, 2*time.Second)

	if !waitFor(2*time.Second, func() bool { return calls.Load() >= 3 }) {
		t.Fatalf("a failing runner stopped the loop after %d call(s); want it to keep ticking", calls.Load())
	}
}

// TestRunTaskCronFiresOnThePatternsWallClockMinute is the end-to-end cron tick
// test. The task's clock is injected so the test does not sleep on real minutes:
// the fake clock reports the reference wall-clock time plus however long the
// test has actually been running, so the pattern's next fire time (12:01:00) is
// ~800ms of real time away.
//
// The assertion that matters is the timestamp the runner observes: it has to be
// the pattern's minute boundary, which is what "fires on wall-clock boundaries"
// means and what running immediately (as a duration schedule does) would not
// produce.
func TestRunTaskCronFiresOnThePatternsWallClockMinute(t *testing.T) {
	s := NewScheduler()

	// 12:00:59.2 -> the next matching minute of "* * * * *" is 12:01:00.
	base := time.Date(2026, 9, 15, 12, 0, 59, 200_000_000, time.UTC)
	started := time.Now()

	var (
		st    *scheduledTask
		err   error
		fires = make(chan time.Time, 4)
	)
	st, err = newScheduledTask(21, "* * * * *", ScooperJobClass, func() error {
		fires <- st.now()
		return nil
	})
	if err != nil {
		t.Fatalf("newScheduledTask: %v", err)
	}
	// The fake clock advances with real time, so the loop's wait is real but the
	// fire time it computes is the pattern's.
	st.clock = func() time.Time { return base.Add(time.Since(started)) }

	done := startRunTask(s, st)
	defer stopAndWait(t, s, st, done, 2*time.Second)

	// cron4j waits for the next matching minute: nothing may run before it.
	select {
	case <-time.After(250 * time.Millisecond):
	case got := <-fires:
		t.Fatalf("the cron task ran at %s before its first matching minute (12:01:00); a cron schedule must not run at start-up", got)
	}
	if len(fires) != 0 {
		t.Fatalf("the cron task ran %d time(s) before its first matching minute", len(fires))
	}

	var first time.Time
	select {
	case first = <-fires:
	case <-time.After(3 * time.Second):
		t.Fatal("the cron task did not run within 3s (its first fire time was ~800ms away)")
	}

	// The runner's clock is the advancing fake clock, so it observes the fire
	// time plus however long the timer overshot; the assertion is on the minute
	// the pattern selected. Firing before that minute (e.g. at start-up) would
	// truncate to an earlier minute and fail.
	want := time.Date(2026, 9, 15, 12, 1, 0, 0, time.UTC)
	if got := first.Truncate(time.Minute); !got.Equal(want) {
		t.Errorf("the cron task ran at %s (minute %s); want the pattern's fire time %s",
			first.Format(time.RFC3339Nano), got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

// TestRunTaskRejectsNonPositiveInterval pins the interval guard. A zero or
// negative interval used to reach time.NewTicker, which panics - and because
// runTask is started with `go`, that panic killed the process. It must log and
// return instead.
func TestRunTaskRejectsNonPositiveInterval(t *testing.T) {
	cases := []struct {
		name     string
		schedule string
		interval time.Duration
	}{
		{"zero", "0s", 0},
		{"negative", "-5m", -5 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The schedule parser rejects these values outright...
			if _, err := parseSchedule(tc.schedule); err == nil {
				t.Errorf("parseSchedule(%q) = nil error; want a rejection at the boundary", tc.schedule)
			}

			// ...and a task built by hand with such an interval must not panic
			// inside runTask either (defence in depth: a panic in this goroutine
			// is unrecoverable for the process).
			s := NewScheduler()
			ran := false
			st := &scheduledTask{
				ID:       3,
				Schedule: tc.schedule,
				Class:    ScooperJobClass,
				runner:   func() error { ran = true; return nil },
				schedule: &parsedSchedule{raw: tc.schedule, interval: tc.interval},
				stopCh:   make(chan struct{}),
			}

			done := startRunTask(s, st)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatalf("runTask with interval %s did not return; it is blocking instead of rejecting", tc.interval)
			}
			if ran {
				t.Errorf("runTask ran the task with a non-positive interval %s", tc.interval)
			}
		})
	}
}

// TestStopTaskIsDeliveredWhileRunnerIsBusy is the stop-path regression test: the
// stop request used to be a non-blocking send on an unbuffered channel, so it
// was dropped whenever the runner was executing - which for a real task is most
// of the time - and the task ran forever.
func TestStopTaskIsDeliveredWhileRunnerIsBusy(t *testing.T) {
	s := NewScheduler()

	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once

	st, err := newScheduledTask(99, "1h", ScooperJobClass, func() error {
		enteredOnce.Do(func() { close(entered) })
		<-release // hold the goroutine inside the runner
		return nil
	})
	if err != nil {
		t.Fatalf("newScheduledTask: %v", err)
	}
	done := startRunTask(s, st)

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the runner was never invoked; runTask did not start")
	}

	// The goroutine is inside the runner, so no receiver is waiting; the request
	// still has to be recorded.
	begin := time.Now()
	s.stopTask(st)
	if elapsed := time.Since(begin); elapsed > 100*time.Millisecond {
		t.Errorf("stopTask blocked for %s; it must not block on the runner", elapsed)
	}
	if !st.stopped() {
		t.Error("the stop request made while the runner was busy was dropped")
	}

	close(release)

	// Once the runner returns, the loop has to notice and exit: it may not wait
	// for a tick or for a second stop request.
	if !waitFor(2*time.Second, func() bool { return doneClosed(done) }) {
		t.Fatal("the task kept running after a stop request made while its runner was executing")
	}
}

// TestStopIsIdempotentAndRestartable covers the scheduler lifecycle: Stop used
// to close its channel unconditionally, so a second call panicked with "close of
// closed channel", and a Start after a Stop kept using the closed channel.
func TestStopIsIdempotentAndRestartable(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	// Stop before Start does nothing and must not panic.
	f.s.Stop()
	if f.running() {
		t.Error("Stop() before Start() left the scheduler marked running")
	}

	f.s.Start()
	if !f.running() {
		t.Error("Start() did not mark the scheduler running")
	}
	if f.ticker() == nil {
		t.Error("Start() did not install the refresh ticker")
	}

	f.s.Stop()
	if f.running() {
		t.Error("Stop() left the scheduler marked running")
	}
	if f.ticker() != nil {
		t.Error("Stop() left the refresh ticker installed")
	}

	// A second Stop is a no-op (this used to panic).
	f.s.Stop()
	f.s.Stop()

	// The scheduler is restartable: Start must build a fresh lifecycle rather
	// than reuse the closed stop channel, which would kill the loop instantly.
	f.s.Start()
	if !f.running() {
		t.Fatal("Start() after Stop() did not restart the scheduler")
	}
	firstTicker := f.ticker()
	if firstTicker == nil {
		t.Fatal("Start() after Stop() did not install a refresh ticker")
	}

	// The restarted loop must still be alive: a scheduler whose stop channel was
	// reused would have exited on its first iteration.
	time.Sleep(50 * time.Millisecond)
	if !f.running() {
		t.Error("the restarted scheduler loop exited on its own")
	}
	if f.ticker() == nil {
		t.Error("the restarted scheduler dropped its ticker")
	}

	f.s.Stop()
}

// TestRefreshJobsAfterStopDoesNotResurrectTasks covers the other half of the
// lifecycle: a stopped scheduler must not start new tasks, because nothing would
// ever stop them, and a restart must schedule again.
func TestRefreshJobsAfterStopDoesNotResurrectTasks(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	f.s.Start()
	f.s.Stop()

	f.src.setJobs(
		jobRow(1, "1h", ScooperJobClass, true),
		jobRow(2, "1h", ScooperJobClass, true),
	)
	err := f.s.refreshJobs()
	if err == nil {
		t.Fatal("refreshJobs() on a stopped scheduler = nil; want it to refuse to start tasks")
	}
	if !strings.Contains(err.Error(), "scheduler is stopped") {
		t.Errorf("refreshJobs() on a stopped scheduler = %q; want it to say the scheduler is stopped", err.Error())
	}
	if f.task(2) != nil {
		t.Error("refreshJobs started task 2 on a stopped scheduler; nothing would stop it")
	}

	// A restarted scheduler schedules again.
	f.s.Start()
	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("refreshJobs() after a restart = %v; want nil", err)
	}
	if f.task(2) == nil {
		t.Error("job 2 was not started after the scheduler was restarted")
	}
	f.s.Stop()
}

// TestStartIsIdempotentWhileRunning pins that a second Start does not replace
// the running loop's ticker (which used to be an unsynchronised field write
// racing the loop's read) and does not start a second loop.
func TestStartIsIdempotentWhileRunning(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	f.s.Start()
	first := f.ticker()
	if first == nil {
		t.Fatal("Start() did not install a refresh ticker")
	}

	f.s.Start() // must be ignored
	if second := f.ticker(); second != first {
		t.Errorf("a second Start() while running replaced the ticker (%p -> %p); it must be a no-op", first, second)
	}
	if !f.running() {
		t.Error("the scheduler is no longer running after a duplicate Start()")
	}

	f.s.Stop()
}

// TestSchedulerStartStopNoRace cycles the lifecycle concurrently with the task
// it starts, so `go test -race` covers the ticker field and the task map. The
// double-Start form of this used to be a data race on s.ticker.
func TestSchedulerStartStopNoRace(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(
		jobRow(1, "* * * * *", ScooperJobClass, true),
		jobRow(2, "1h", ScooperJobClass, true),
	)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.s.Start()
			f.s.Stop()
		}()
	}

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("concurrent Start/Stop cycles did not finish within 10s")
	}

	// Leave the scheduler stopped, and prove a fourth cycle still works.
	f.s.Start()
	f.s.Stop()
	if f.running() {
		t.Error("the scheduler is still marked running after the final Stop()")
	}
}

// ---------------------------------------------------------------------------
// integration-shaped: the real dispatch code against a fake job source
// ---------------------------------------------------------------------------

// TestRefreshJobsDispatchesFromFakeJobSource drives the real refreshJobs against
// an in-memory tJobs source and asserts the dispatch rules: a known class with a
// usable schedule (duration or cron) starts a task; an unknown class, an empty
// schedule and an unparseable one are rejected with a named reason and scheduled
// nothing.
func TestRefreshJobsDispatchesFromFakeJobSource(t *testing.T) {
	f := newSchedulerFixture(t)

	f.src.setJobs(
		jobRow(1, "1h", ScooperJobClass, true),                                 // duration schedule
		jobRow(2, "1h", "com.example.NoSuchTask", true),                        // unknown class
		jobRow(3, "*/30 * * * *", EligibilityJobClass, true),                   // cron, the seeded form
		jobRow(4, "", EligibilityJobClass, true),                               // empty schedule
		jobRow(5, "5 minutes", "org.remitt.server.tasks.EligibiltyTask", true), // seeded typo class
	)

	err := f.s.refreshJobs()
	if err == nil {
		t.Fatal("refreshJobs() = nil; want the three rejected rows reported")
	}
	msg := err.Error()
	for _, want := range []string{"3 job(s) rejected", "NoSuchTask", "jobSchedule", "unknown job class"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refreshJobs() error = %q; want it to mention %q", msg, want)
		}
	}

	if got := f.taskCount(); got != 2 {
		t.Fatalf("len(s.tasks) = %d; want 2: jobs 1 (duration) and 3 (cron) are schedulable", got)
	}

	for _, id := range []int64{2, 4, 5} {
		f.s.mu.Lock()
		_, ok := f.s.tasks[id]
		f.s.mu.Unlock()
		if ok {
			t.Errorf("job %d was started; want it rejected (unknown class or unusable schedule)", id)
		}
	}

	durationTask := f.task(1)
	if durationTask == nil {
		t.Fatal("job 1 (duration schedule) was not started")
	}
	if durationTask.Schedule != "1h" || durationTask.Class != ScooperJobClass {
		t.Errorf("task 1 = {Schedule: %q, Class: %q}; want {Schedule: \"1h\", Class: %q}",
			durationTask.Schedule, durationTask.Class, ScooperJobClass)
	}
	if durationTask.runner == nil {
		t.Error("task 1 runner is nil; refreshJobs stored an unresolvable runner")
	}
	if durationTask.stopCh == nil {
		t.Error("task 1 stopCh is nil; the task could never be stopped")
	}

	cronTask := f.task(3)
	if cronTask == nil {
		t.Fatal("job 3 (cron schedule) was not started")
	}
	if cronTask.schedule == nil || !cronTask.schedule.isCron() {
		t.Fatalf("task 3 schedule %q was not parsed as a cron pattern", cronTask.Schedule)
	}
	if cronTask.runner == nil {
		t.Error("task 3 runner is nil; refreshJobs stored an unresolvable runner")
	}
	if next, ok := cronTask.schedule.pattern.next(time.Now()); !ok || next.Before(time.Now()) {
		t.Errorf("task 3 has no future fire time (%s, ok=%v)", next, ok)
	}
}

// TestRefreshJobsReportsRejectedScheduleReasons checks the log-worthy detail in
// the returned error: a rejected row must say which value was rejected and why.
func TestRefreshJobsReportsRejectedScheduleReasons(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(
		jobRow(1, "*/0 * * * *", ScooperJobClass, true),
		jobRow(2, "0 0 30 feb *", ScooperJobClass, true),
		jobRow(3, "0s", ScooperJobClass, true),
	)

	err := f.s.refreshJobs()
	if err == nil {
		t.Fatal("refreshJobs() = nil; want all three unusable schedules reported")
	}
	msg := err.Error()
	for _, want := range []string{"step must be >= 1", "no matching time within", "non-positive duration", "rejecting unusable jobSchedule"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refreshJobs() error = %q; want it to mention %q", msg, want)
		}
	}
	if got := f.taskCount(); got != 0 {
		t.Errorf("len(s.tasks) = %d; want 0 (every row was rejected)", got)
	}
}

// TestRefreshJobsIsIdempotentForUnchangedSchedule covers the "No change" branch:
// re-reading the same row must not restart the task.
func TestRefreshJobsIsIdempotentForUnchangedSchedule(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "*/30 * * * *", ScooperJobClass, true))

	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("refreshJobs() = %v; want nil", err)
	}
	first := f.task(1)
	if first == nil {
		t.Fatal("job 1 was not started")
	}

	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("second refreshJobs() = %v; want nil", err)
	}
	if got := f.taskCount(); got != 1 {
		t.Fatalf("len(s.tasks) = %d after a second refresh; want 1", got)
	}
	if second := f.task(1); second != first {
		t.Error("refreshJobs restarted an unchanged job (task pointer changed)")
	}
}

// TestRefreshJobsRestartsTaskWhenScheduleChanges covers the change branch: a
// changed jobSchedule retires the old task and starts a new one.
func TestRefreshJobsRestartsTaskWhenScheduleChanges(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("refreshJobs() = %v; want nil", err)
	}
	first := f.task(1)
	if first == nil {
		t.Fatal("job 1 was not started")
	}

	f.src.setJobs(jobRow(1, "*/30 * * * *", ScooperJobClass, true))
	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("refreshJobs() after the schedule change = %v; want nil", err)
	}

	if got := f.taskCount(); got != 1 {
		t.Fatalf("len(s.tasks) = %d after a schedule change; want 1", got)
	}
	second := f.task(1)
	if second == nil {
		t.Fatal("job 1 is missing after the schedule change")
	}
	if second == first {
		t.Error("the schedule change did not restart the task")
	}
	if second.Schedule != "*/30 * * * *" {
		t.Errorf("task 1 Schedule = %q; want %q", second.Schedule, "*/30 * * * *")
	}
	if first.Schedule != "1h" {
		t.Errorf("the retired task's Schedule = %q; want %q", first.Schedule, "1h")
	}
	if !first.stopped() {
		t.Error("the retired task was not signalled to stop; its run loop would keep ticking")
	}
}

// TestRefreshJobsRemovesTasksNoLongerEnabled covers the removal branch: a job
// that disappears from the enabled job set is dropped from the scheduler map and
// signalled to stop.
func TestRefreshJobsRemovesTasksNoLongerEnabled(t *testing.T) {
	f := newSchedulerFixture(t)
	f.src.setJobs(jobRow(1, "1h", ScooperJobClass, true))

	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("refreshJobs() = %v; want nil", err)
	}
	first := f.task(1)
	if first == nil {
		t.Fatal("job 1 was not started")
	}

	f.src.setJobs() // job disabled or deleted
	if err := f.s.refreshJobs(); err != nil {
		t.Fatalf("refreshJobs() after the job was removed = %v; want nil", err)
	}

	if got := f.taskCount(); got != 0 {
		t.Errorf("len(s.tasks) = %d after the job was removed from tJobs; want 0", got)
	}
	if !first.stopped() {
		t.Error("the removed task was not signalled to stop")
	}
}

// TestRefreshJobsConcurrentTicksNoRace runs refreshJobs from several goroutines
// while the tasks it starts are running: the state it touches (s.tasks, s.mu)
// must stay race free and a job must not be started twice.
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
			if err := f.s.refreshJobs(); err != nil {
				t.Errorf("concurrent refreshJobs() = %v; want nil", err)
			}
		}()
	}

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent refreshJobs calls did not finish within 5s")
	}

	if got := f.taskCount(); got != 3 {
		t.Errorf("len(s.tasks) = %d after three concurrent refreshes; want 3 (one task per job, no duplicates)", got)
	}
	f.task(1)
	f.task(2)
	f.task(3)
}

// ---------------------------------------------------------------------------
// the package's own runners
// ---------------------------------------------------------------------------

// TestRunScooperTaskIsANoOp documents what the scheduled ScooperTask actually
// does.
//
// FINDING (task/eligibility.go): the scheduled ScooperTask logs "scooper run
// complete (no plugins configured)" and returns nil, while the scooper package
// (scooper/map.go, scooper/sftp.go, scooper/gatewayedi.go) does have pluggable
// implementations. Scheduled scoops therefore never run, and the task reports
// success. This is outside the scope of the scheduler fixes and is pinned here
// so a change in behaviour is noticed.
func TestRunScooperTaskIsANoOp(t *testing.T) {
	if err := RunScooperTask(); err != nil {
		t.Errorf("RunScooperTask() error = %v; want nil (the stub logs and returns nil)", err)
	}
}

// TestRefreshJobsWithoutDatabaseReportsError pins the nil-guard convention this
// repo uses (see eligibility/gatewayedi.go): with no database, refreshJobs
// returns an error naming the missing dependency instead of dereferencing the
// nil sqlc handle and panicking.
func TestRefreshJobsWithoutDatabaseReportsError(t *testing.T) {
	if model.Queries != nil {
		t.Skip("a live database is configured (model.Queries is non-nil); this test covers the no-database case")
	}

	s := NewScheduler()
	err := s.refreshJobs()
	if err == nil {
		t.Fatal("refreshJobs() = nil with model.Queries nil; want an error, not a nil dereference")
	}
	if !strings.Contains(err.Error(), "database not initialized") {
		t.Errorf("refreshJobs() error = %q; want it to report the uninitialized database", err.Error())
	}
}

// TestSchedulerStartStopWithoutDatabase covers the lifecycle with no database:
// Start's refreshJobs fails with a logged error, and the scheduler must still
// start, stop cleanly and be safe to stop twice.
func TestSchedulerStartStopWithoutDatabase(t *testing.T) {
	if model.Queries != nil {
		t.Skip("a live database is configured (model.Queries is non-nil); this test covers the no-database case")
	}

	s := NewScheduler()
	s.Start()
	if !s.running {
		t.Error("Start() did not mark the scheduler running")
	}
	if s.ticker == nil {
		t.Error("Start() did not install a refresh ticker; the runtime timer would leak")
	}
	s.Stop()
	s.Stop()
	if s.running {
		t.Error("Stop() left the scheduler marked running")
	}
}

// TestRunEligibilityTaskWithoutDatabaseReportsError pins the other DB-bound
// runner: it must return the error rather than panicking on the nil handle. The
// scheduler runs it from a goroutine, where that panic was unrecoverable and took
// the whole process down.
func TestRunEligibilityTaskWithoutDatabaseReportsError(t *testing.T) {
	if model.Queries != nil {
		t.Skip("a live database is configured (model.Queries is non-nil); this test covers the no-database case")
	}

	err := RunEligibilityTask()
	if err == nil {
		t.Fatal("RunEligibilityTask() = nil with model.Queries nil; want an error, not a nil dereference")
	}
	if !strings.Contains(err.Error(), "database not initialized") {
		t.Errorf("RunEligibilityTask() error = %q; want it to report the uninitialized database", err.Error())
	}
}

// TestRunTaskWithoutRunnerOrSchedule covers the two defensive guards in runTask:
// neither a missing runner nor a missing parsed schedule may panic, because
// runTask runs in a goroutine where a panic is unrecoverable.
func TestRunTaskWithoutRunnerOrSchedule(t *testing.T) {
	cases := []struct {
		name string
		task *scheduledTask
	}{
		{"nil_runner", &scheduledTask{
			ID: 31, Schedule: "1h", Class: ScooperJobClass,
			schedule: &parsedSchedule{raw: "1h", interval: time.Hour},
			stopCh:   make(chan struct{}),
		}},
		{"nil_schedule", &scheduledTask{
			ID: 32, Schedule: "1h", Class: ScooperJobClass,
			runner: func() error { return nil },
			stopCh: make(chan struct{}),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScheduler()
			done := startRunTask(s, tc.task)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("runTask did not return for an unusable task")
			}
		})
	}
}
