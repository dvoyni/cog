package kernel

import (
	"runtime"
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

func TestConflictingWritersRunOneAtATime(t *testing.T) {
	var running atomic.Int32
	var overlapped atomic.Bool
	var write *Task[NoArg, NoResult]
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(counter{})
		write = c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
			access.Write[counter]()
			return func(k *Kernel, arg NoArg) NoResult {
				if running.Add(1) != 1 {
					overlapped.Store(true)
				}
				runtime.Gosched()
				running.Add(-1)
				return NoResult{}
			}
		})
	})
	out := scheduleBatch(k, func(ex *Executor) JobHandle {
		jobs := make([]JobHandle, 50)
		for i := range jobs {
			jobs[i] = ex.Schedule(write, NoArg{}).Handle()
		}
		return ex.Join(jobs...)
	})
	waitFor(t, k, out)
	if overlapped.Load() {
		t.Fatal("two writers of one resource ran at once")
	}
}

func TestReadersRunTogether(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs two workers")
	}
	var arrived atomic.Int32
	met := make(chan struct{})
	var apart atomic.Bool
	var read *Task[NoArg, NoResult]
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(counter{})
		read = c.RegisterTask(func(access *Access) TaskBody[NoArg, NoResult] {
			access.Read[counter]()
			return func(k *Kernel, arg NoArg) NoResult {
				// Each reader waits for the other, which only works if both
				// hold the read lock at once.
				if arrived.Add(1) == 2 {
					close(met)
				}
				select {
				case <-met:
				case <-time.After(time.Second):
					apart.Store(true)
				}
				return NoResult{}
			}
		})
	})
	out := scheduleBatch(k, func(ex *Executor) JobHandle {
		return ex.Join(ex.Schedule(read, NoArg{}).Handle(), ex.Schedule(read, NoArg{}).Handle())
	})
	waitFor(t, k, out)
	if apart.Load() {
		t.Fatal("two readers of one resource did not run together")
	}
}

func TestWaitingWriterIsNotOvertakenByReaders(t *testing.T) {
	var order recording
	var read, write *Task[int32, NoResult]
	k, _ := spawnTestKernel(t, func(c *Configurator) {
		c.InitResource(counter{})
		read = c.RegisterTask(func(access *Access) TaskBody[int32, NoResult] {
			access.Read[counter]()
			return func(k *Kernel, arg int32) NoResult {
				time.Sleep(time.Millisecond) // long enough for the late reader to try
				order.record(arg)
				return NoResult{}
			}
		})
		write = c.RegisterTask(func(access *Access) TaskBody[int32, NoResult] {
			access.Write[counter]()
			return func(k *Kernel, arg int32) NoResult {
				order.record(arg)
				return NoResult{}
			}
		})
	})
	out := scheduleBatch(k, func(ex *Executor) JobHandle {
		return ex.Join(
			ex.Schedule(read, 1).Handle(),
			ex.Schedule(write, 2).Handle(),
			ex.Schedule(read, 3).Handle(),
		)
	})
	waitFor(t, k, out)
	if !order.is(1, 2, 3) {
		t.Fatalf("ran %v, want the writer between the readers", &order)
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
	if k.ready.Len() != 0 {
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
