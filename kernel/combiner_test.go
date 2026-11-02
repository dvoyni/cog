package kernel

import (
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type counter struct{ Value int64 }

type label struct{ Value int64 }

// counterTask registers a task adding its arg to counter, read-modify-write
// with no synchronization of its own: two of its jobs running at once is a
// data race the race detector reports.
func counterTask(c *Configurator) *Task[int64, NoResult] {
	return c.RegisterTask(func(access *Access) TaskBody[int64, NoResult] {
		value := access.Write[counter]()
		return func(k *Kernel, arg int64) NoResult {
			v := value.Get()
			v.Value += arg
			value.Set(v)
			return NoResult{}
		}
	})
}

// readerTask registers a task reading counter.
func readerTask(c *Configurator) *Task[NoArg, int64] {
	return c.RegisterTask(func(access *Access) TaskBody[NoArg, int64] {
		value := access.Read[counter]()
		return func(k *Kernel, arg NoArg) int64 { return value.Get().Value }
	})
}

// scheduleBatch publishes once, scheduling what schedule does, and answers
// the batch's out dependency.
func scheduleBatch(k *Kernel, schedule func(ex *Executor) JobHandle) JobHandle {
	ex := k.acquireExecutor()
	out := schedule(ex)
	ex.submit()
	return out
}

// grantLog is every job a lock combiner granted, in order, read from its
// kernel's ready queue.
type grantLog struct {
	kernel *Kernel
	slots  []uint32
}

func (g *grantLog) is(want ...uint32) bool {
	g.slots = g.kernel.runner.ready.Drain(g.slots)
	return slices.Equal(g.slots, want)
}

// Lock tables for one resource: task 0 locks nothing, 1 writes the resource
// and 2 reads it.
const (
	noLocks = iota
	writer
	reader
)

func oneResourceLockTable() []*taskLocks {
	return []*taskLocks{
		noLocks: {},
		writer:  {reads: []uint64{0}, writes: []uint64{1}},
		reader:  {reads: []uint64{1}, writes: []uint64{0}},
	}
}

// newTestCombiner answers the lock combiner of a partial kernel over one
// resource with the lock table oneResourceLockTable answers, and the log of
// what it grants.
func newTestCombiner() (*lockCombiner, *grantLog) {
	k := newPartialKernel(nil, oneResourceLockTable(), 1)
	return &k.combiner, &grantLog{kernel: k}
}

func TestConflictingWritersAreGrantedOneAtATime(t *testing.T) {
	c, log := newTestCombiner()
	c.arrive(1, writer)
	c.arrive(2, writer)
	c.arrive(3, writer)
	if !log.is(1) {
		t.Fatalf("granted %v, want only the first writer", log.slots)
	}
	c.release(writer)
	if !log.is(1, 2) {
		t.Fatalf("granted %v after one release, want the second writer next", log.slots)
	}
	c.release(writer)
	if !log.is(1, 2, 3) {
		t.Fatalf("granted %v after two releases, want every writer in order", log.slots)
	}
}

func TestReadersAreGrantedTogether(t *testing.T) {
	c, log := newTestCombiner()
	c.arrive(1, reader)
	c.arrive(2, reader)
	if !log.is(1, 2) {
		t.Fatalf("granted %v, want both readers at once", log.slots)
	}
}

func TestWaitingWriterIsNotOvertakenByReaders(t *testing.T) {
	c, log := newTestCombiner()
	c.arrive(1, reader)
	c.arrive(2, writer)
	c.arrive(3, reader)
	if !log.is(1) {
		t.Fatalf("granted %v, want the late reader to wait behind the writer", log.slots)
	}
	c.release(reader)
	if !log.is(1, 2) {
		t.Fatalf("granted %v once the first reader released, want the writer", log.slots)
	}
	c.release(writer)
	if !log.is(1, 2, 3) {
		t.Fatalf("granted %v once the writer released, want the late reader", log.slots)
	}
}

func TestJobLockingNothingIsGrantedAtOnce(t *testing.T) {
	c, log := newTestCombiner()
	c.arrive(1, writer)
	c.arrive(2, noLocks)
	c.release(noLocks)
	c.arrive(3, writer)
	if !log.is(1, 2) {
		t.Fatalf("granted %v, want the lock-free job beside the writer, and its release to free nothing", log.slots)
	}
}

// Goroutines arrive and release at once, as workers do: every arrival is
// granted exactly once, none is left waiting, and no writer ever runs
// alongside another job of the resource.
func TestEveryArrivalIsGrantedOnce(t *testing.T) {
	const arrivers, perArriver = 8, 500
	const jobs = arrivers * perArriver
	var (
		taskOf       [jobs + 1]uint32
		grantedCount [jobs + 1]atomic.Int32
		held         atomic.Int32 // readers holding, or -1 for a writer
		conflict     atomic.Bool
		finished     atomic.Int32
	)
	for slot := 1; slot <= jobs; slot++ {
		taskOf[slot] = reader
		if slot%3 == 0 {
			taskOf[slot] = writer
		}
	}
	c, log := newTestCombiner()
	stop := make(chan struct{})
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			for {
				slot, ok := log.kernel.runner.ready.Pop()
				if !ok {
					select {
					case <-stop:
						return
					default:
						runtime.Gosched()
						continue
					}
				}
				grantedCount[slot].Add(1)
				task := taskOf[slot]
				if task == writer {
					if !held.CompareAndSwap(0, -1) {
						conflict.Store(true)
					}
					runtime.Gosched()
					held.CompareAndSwap(-1, 0)
				} else {
					if held.Add(1) <= 0 {
						conflict.Store(true)
					}
					runtime.Gosched()
					held.Add(-1)
				}
				c.release(task)
				finished.Add(1)
			}
		})
	}
	var arriving sync.WaitGroup
	for arriver := range arrivers {
		arriving.Go(func() {
			for i := range perArriver {
				slot := uint32(arriver*perArriver + i + 1)
				c.arrive(slot, taskOf[slot])
			}
		})
	}
	arriving.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for finished.Load() < jobs && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(stop)
	workers.Wait()
	if got := finished.Load(); got != jobs {
		t.Fatalf("%d of %d arrivals were left waiting for the combiner", jobs-got, jobs)
	}
	if conflict.Load() {
		t.Fatal("a writer ran alongside another job of its resource")
	}
	for slot := 1; slot <= jobs; slot++ {
		if got := grantedCount[slot].Load(); got != 1 {
			t.Fatalf("slot %d was granted %d times, want once", slot, got)
		}
	}
}

func TestPinnedJobGoesToTheMainThread(t *testing.T) {
	var pinned *Task[int64, NoResult]
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(counter{})
		pinned = counterTask(c)
		c.PinToMainThread(pinned)
	})
	var job JobHandle
	scheduleBatch(k, func(ex *Executor) JobHandle {
		job = ex.Schedule(pinned, 1).Handle()
		return job
	})
	if k.runner.ready.Len() != 0 {
		t.Fatal("a pinned job went to the workers' ready queue")
	}
	if got := k.supervisor.pinned.Drain(nil); len(got) != 1 || got[0].kernel != k || got[0].job != job {
		t.Fatalf("the main thread got %v, want the pinned job", got)
	}
}

// The workers run jobs at once while writers of one resource and readers of
// it arrive and finish: the race detector reports any two writers, or a
// writer and a reader, that run together, and the total proves none was lost.
func TestCombinerUnderConcurrentWorkers(t *testing.T) {
	var add *Task[int64, NoResult]
	var read *Task[NoArg, int64]
	var other *Task[NoArg, NoResult]
	k, c := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(counter{})
		c.InitResource(label{})
		add = counterTask(c)
		read = readerTask(c)
		other = c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
			l := access.Write[label]()
			return func(k *Kernel, arg NoArg) NoResult {
				v := l.Get()
				v.Value++
				l.Set(v)
				return NoResult{}
			}
		})
	})

	const batches, perBatch = 200, 6
	var finished sync.WaitGroup
	for range batches {
		finished.Go(func() {
			out := scheduleBatch(k, func(ex *Executor) JobHandle {
				var jobs []JobHandle
				for i := range perBatch {
					switch i % 3 {
					case 0:
						jobs = append(jobs, ex.Schedule(add, 1).Handle())
					case 1:
						jobs = append(jobs, ex.Schedule(read, NoArg{}).Handle())
					default:
						jobs = append(jobs, ex.Schedule(other, NoArg{}).Handle())
					}
				}
				return ex.Join(jobs...)
			})
			sig := NewSignal()
			k.Notify(sig, out)
			<-sig.C()
		})
	}
	finished.Wait()
	if got, _ := c.Resource[counter](); got.Value != batches*perBatch/3 {
		t.Fatalf("counter is %d, want %d", got.Value, batches*perBatch/3)
	}
	if got, _ := c.Resource[label](); got.Value != batches*perBatch/3 {
		t.Fatalf("label is %d, want %d", got.Value, batches*perBatch/3)
	}
}
