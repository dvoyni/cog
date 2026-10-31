package kernel

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// testTask is a configurable task used to observe scheduler behavior.
//   - started (if non-nil) is closed the moment run begins, so tests can detect
//     whether a task was allowed to enter its critical section.
//   - release (if non-nil) blocks run until the test closes it, so tests can hold
//     a task inside its critical section and observe locking.
//   - fn (if non-nil) runs synchronously inside run.
type testTask struct {
	read    []int32
	write   []int32
	started chan struct{}
	release chan struct{}
	fn      func() error
	err     error
}

func (tt *testTask) locks() *lockSet {
	return &lockSet{read: tt.read, write: tt.write, span: testResources}
}

func (tt *testTask) run(any) error {
	if tt.started != nil {
		close(tt.started)
	}
	if tt.fn != nil {
		if err := tt.fn(); err != nil {
			return err
		}
	}
	if tt.release != nil {
		<-tt.release
	}
	return tt.err
}

// blockingTask returns a task that signals when it starts and blocks until released.
func blockingTask(read, write []int32) *testTask {
	return &testTask{
		read:    read,
		write:   write,
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

// testResources names the tracks the scheduler tests lock, by the letter each
// test uses for it.
const testResources = 4

func testResourceIndex(name string) int32 {
	switch name {
	case "R":
		return 0
	case "A":
		return 1
	case "B":
		return 2
	case "W":
		return 3
	default:
		panic("unknown test resource " + name)
	}
}

func set(names ...string) []int32 {
	indices := make([]int32, 0, len(names))
	for _, n := range names {
		indices = append(indices, testResourceIndex(n))
	}
	return indices
}

// schedule runs one task through the scheduler, which is what the engine's
// runTask does for a task that asks for locks.
func schedule(s *scheduler, t task) error {
	return s.execute(t, nil, t.locks())
}

func startScheduler(t *testing.T) *scheduler {
	t.Helper()
	s := newScheduler()
	t.Cleanup(s.shutdown)
	return s
}

func mustStart(t *testing.T, started <-chan struct{}, within time.Duration) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(within):
		t.Fatalf("task did not start within %s (expected it to be allowed to run)", within)
	}
}

func mustNotStart(t *testing.T, started <-chan struct{}, within time.Duration) {
	t.Helper()
	select {
	case <-started:
		t.Fatal("task started but should have been blocked by a resource lock")
	case <-time.After(within):
	}
}

// Readers of the same resource must be allowed to run concurrently.
func TestScheduler_ReadersOverlap(t *testing.T) {
	s := startScheduler(t)

	r1 := blockingTask(set("R"), nil)
	r2 := blockingTask(set("R"), nil)
	go func() { _ = schedule(s, r1) }()
	go func() { _ = schedule(s, r2) }()

	// If reads were incorrectly exclusive, only one would start.
	mustStart(t, r1.started, time.Second)
	mustStart(t, r2.started, time.Second)

	close(r1.release)
	close(r2.release)
}

// A writer must exclude readers of the same resource until it releases.
func TestScheduler_WriterExcludesReader(t *testing.T) {
	s := startScheduler(t)

	w := blockingTask(nil, set("R"))
	go func() { _ = schedule(s, w) }()
	mustStart(t, w.started, time.Second)

	r := blockingTask(set("R"), nil)
	go func() { _ = schedule(s, r) }()
	mustNotStart(t, r.started, 50*time.Millisecond)

	close(w.release)
	mustStart(t, r.started, time.Second)
	close(r.release)
}

// Two writers of the same resource must be mutually exclusive.
func TestScheduler_WritersExclusiveSameResource(t *testing.T) {
	s := startScheduler(t)

	w1 := blockingTask(nil, set("R"))
	go func() { _ = schedule(s, w1) }()
	mustStart(t, w1.started, time.Second)

	w2 := blockingTask(nil, set("R"))
	go func() { _ = schedule(s, w2) }()
	mustNotStart(t, w2.started, 50*time.Millisecond)

	close(w1.release)
	mustStart(t, w2.started, time.Second)
	close(w2.release)
}

func TestScheduler_WaitingWriterBlocksLaterReader(t *testing.T) {
	s := startScheduler(t)

	activeReader := blockingTask(set("R"), nil)
	go func() { _ = schedule(s, activeReader) }()
	mustStart(t, activeReader.started, time.Second)

	writer := blockingTask(nil, set("R"))
	go func() { _ = schedule(s, writer) }()
	mustNotStart(t, writer.started, 50*time.Millisecond)
	laterReader := blockingTask(set("R"), nil)
	go func() { _ = schedule(s, laterReader) }()
	mustNotStart(t, laterReader.started, 50*time.Millisecond)

	close(activeReader.release)
	mustStart(t, writer.started, time.Second)
	mustNotStart(t, laterReader.started, 50*time.Millisecond)
	close(writer.release)
	mustStart(t, laterReader.started, time.Second)
	close(laterReader.release)
}

func TestScheduler_UnrelatedRequestPassesBlockedRequest(t *testing.T) {
	s := startScheduler(t)

	activeReader := blockingTask(set("R"), nil)
	go func() { _ = schedule(s, activeReader) }()
	mustStart(t, activeReader.started, time.Second)

	writer := blockingTask(nil, set("R"))
	go func() { _ = schedule(s, writer) }()
	mustNotStart(t, writer.started, 50*time.Millisecond)
	unrelated := blockingTask(nil, set("B"))
	go func() { _ = schedule(s, unrelated) }()
	mustStart(t, unrelated.started, time.Second)

	close(unrelated.release)
	close(activeReader.release)
	mustStart(t, writer.started, time.Second)
	close(writer.release)
}

// Writers on different resources must run in parallel.
func TestScheduler_DifferentResourcesParallel(t *testing.T) {
	s := startScheduler(t)

	a := blockingTask(nil, set("A"))
	b := blockingTask(nil, set("B"))
	go func() { _ = schedule(s, a) }()
	go func() { _ = schedule(s, b) }()

	mustStart(t, a.started, time.Second)
	mustStart(t, b.started, time.Second)

	close(a.release)
	close(b.release)
}

// Concurrent writers on the same resource must be serialized: an unguarded
// counter increment stays correct and race-free (validated under -race).
func TestScheduler_WritersSerialized(t *testing.T) {
	s := startScheduler(t)

	const n = 200
	counter := 0
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task := &testTask{
				write: set("R"),
				fn: func() error {
					counter++ // safe only if writers are exclusive
					return nil
				},
			}
			_ = schedule(s, task)
		}()
	}
	wg.Wait()

	if counter != n {
		t.Fatalf("counter = %d, want %d (writers were not serialized)", counter, n)
	}
}

// Once the coordinator stops, execute returns ErrSchedulerStopped instead of
// blocking forever.
func TestScheduler_StoppedReturnsError(t *testing.T) {
	s := newScheduler()
	s.shutdown() // coordinator has exited

	err := schedule(s, &testTask{write: set("R")})
	if _, ok := err.(ErrSchedulerStopped); !ok {
		t.Fatalf("err = %v, want ErrSchedulerStopped", err)
	}
}

// The coordination model must remain live under GOMAXPROCS=1: concurrency
// (not parallelism) is what the scheduler guarantees, and goroutines yield at
// channel operations.
func TestScheduler_SingleThreaded(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))

	s := startScheduler(t)

	// Readers still overlap.
	r1 := blockingTask(set("R"), nil)
	r2 := blockingTask(set("R"), nil)
	go func() { _ = schedule(s, r1) }()
	go func() { _ = schedule(s, r2) }()
	mustStart(t, r1.started, time.Second)
	mustStart(t, r2.started, time.Second)
	close(r1.release)
	close(r2.release)

	// A writer still excludes a reader.
	w := blockingTask(nil, set("W"))
	go func() { _ = schedule(s, w) }()
	mustStart(t, w.started, time.Second)
	rd := blockingTask(set("W"), nil)
	go func() { _ = schedule(s, rd) }()
	mustNotStart(t, rd.started, 50*time.Millisecond)
	close(w.release)
	mustStart(t, rd.started, time.Second)
	close(rd.release)
}

// testOwner records how each of its pooled jobs ended.
type testOwner struct{ outcomes chan error }

func (o testOwner) jobDone(err error) { o.outcomes <- err }

// A pooled job still waiting for its locks when the scheduler stops is ended
// with ErrSchedulerStopped, so whoever owns it - a publication being waited on -
// is never left hanging. The job holding the lock finishes normally.
func TestScheduler_ShutdownEndsWaitingPooledJobs(t *testing.T) {
	s := newScheduler()
	holder := blockingTask(nil, set("R"))
	waiter := blockingTask(nil, set("R"))
	owner := testOwner{outcomes: make(chan error, 2)}
	if err := s.submitBatch([]batchJob{
		{task: holder, locks: holder.locks()},
		{task: waiter, locks: waiter.locks()},
	}, nil, owner); err != nil {
		t.Fatalf("submitBatch = %v", err)
	}
	mustStart(t, holder.started, time.Second)

	s.shutdown()
	select {
	case err := <-owner.outcomes:
		if _, ok := err.(ErrSchedulerStopped); !ok {
			t.Fatalf("waiting job ended with %v, want ErrSchedulerStopped", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting job was never ended")
	}
	mustNotStart(t, waiter.started, 50*time.Millisecond)

	close(holder.release)
	select {
	case err := <-owner.outcomes:
		if err != nil {
			t.Fatalf("running job ended with %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("running job never finished")
	}
}
