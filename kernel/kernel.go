package kernel

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/dvoyni/cog/kernel/internal/concurrent"
)

// KernelID identifies a Kernel. Spawn assigns it from a counter the
// Supervisor increments atomically, so it is unique for the process's life
// and never reused: an ID kept past its kernel's destroy names no kernel,
// rather than whichever kernel was spawned next. The zero KernelID names no
// kernel.
type KernelID uint64

// Kernel is one isolated runtime inside a Supervisor: its resources, its
// registered tasks, its event handlers and its job graph. Handlers, task
// bodies and plugin Start and Stop receive it; code outside the pool, such as
// the main loop, gets one from Supervisor.GetKernel.
//
// Every method is safe from any goroutine. A Kernel that has been destroyed
// stays safe to call: Publish publishes nothing and Notify signals at once.
type Kernel struct {
	// supervisor spawned k, and runs its pinned jobs on the main thread.
	supervisor *Supervisor
	id         KernelID
	logger     *Logger
	// plugins is the order Init and Start ran in; Stop and Destroy run it in
	// reverse.
	plugins []Plugin
	// config is what every plugin's Init was handed.
	config any
	// workerCount is how many goroutines run k's jobs.
	workerCount int
	// resources holds every resource by its type. Only Init adds to it, so it
	// is never changed once the kernel starts.
	resources map[reflect.Type]*resource
	// dispatchers holds the dispatcher of every event type, each as its
	// *dispatcher[E], its handlers sorted once every plugin has initialized.
	dispatchers map[reflect.Type]eventDispatcher
	// tasks is every task registered in k, by Task.index. Index 0 is the
	// Join task. Only Init adds to it, so it is never changed once the kernel
	// starts.
	tasks []runnable
	// taskLocks is the lock table: every task's taskLocks, by Task.index,
	// beside tasks so the lock combiner reads it without knowing a task's
	// types. Index 0, the Join task's, locks nothing.
	taskLocks []*taskLocks
	// jobs is every job of k and the edges between them.
	jobs jobStorage
	// executors holds the Executors of finished publications, for the next
	// to reuse.
	executors concurrent.Queue[*Executor]

	// arrivals is every job whose dependencies have all finished and that
	// locks something, waiting for the lock combiner.
	arrivals concurrent.Queue[uint32]
	// unlocks is the task index of every finished job whose locks the lock
	// combiner has yet to unlock. A task index, not a slot: the slot is
	// released as the job finishes.
	unlocks concurrent.Queue[uint32]
	// ready is every job whose locks are held, or that locks nothing,
	// waiting to run. Pinned jobs go to the Supervisor instead.
	ready concurrent.Queue[uint32]

	// workers is every goroutine running k's jobs. A worker that finds ready
	// empty counts itself in idle and parks on wake, which has a slot per
	// worker: a job made ready while one is idle puts a token in it, so a
	// worker that parks after the job was pushed still wakes. stop is closed
	// to end them.
	workers sync.WaitGroup
	idle    atomic.Int32
	wake    chan struct{}
	stop    chan struct{}

	// outstanding counts every job not yet finished and every Publish still
	// running. closed is set as k drains, and from then on Publish publishes
	// nothing; the last outstanding job or publication then puts a token in
	// drained.
	outstanding atomic.Int64
	closed      atomic.Bool
	drained     chan struct{}

	// combinerMu is held by whichever goroutine is the lock combiner. It is
	// only ever tried, never waited for: a goroutine that does not get it
	// leaves its work queued for the one that has it.
	combinerMu sync.Mutex
	// Everything below belongs to the lock combiner, and is touched only
	// while combinerMu is held.
	locks lockState
	// waiting is every arrived job whose locks are not free yet, oldest
	// first.
	waiting []uint32
	// arrived and unlocked are the buffers arrivals and unlocks drain
	// into, kept between passes.
	arrived, unlocked []uint32
}

// resource is one resource of a kernel.
type resource struct {
	// index is the resource's bit in every task's lock masks.
	index int
	// value is the *T the resource lives in, which every handle to it binds.
	value any
}

// newKernel creates kernel id of supervisor, logging through supervisor's
// LogHandler, with the config each plugin is handed, running its jobs on
// workerCount workers, and plugins to register in order.
func newKernel(supervisor *Supervisor, id KernelID, config any, workerCount int, plugins []Plugin) *Kernel {
	return &Kernel{
		supervisor:  supervisor,
		id:          id,
		logger:      &Logger{kernel: id, handler: supervisor.logHandler},
		plugins:     plugins,
		config:      config,
		workerCount: workerCount,
		resources:   map[reflect.Type]*resource{},
		dispatchers: map[reflect.Type]eventDispatcher{},
		tasks:       []runnable{joinTask{}},
		taskLocks:   []*taskLocks{{}},
		wake:        make(chan struct{}, workerCount),
		stop:        make(chan struct{}),
		drained:     make(chan struct{}, 1),
	}
}

// ID returns the ID Spawn assigned this kernel.
func (k *Kernel) ID() KernelID { return k.id }

// Publish delivers event to every handler subscribed to E, one by one, on the
// calling goroutine, each receiving the event and out dependency the one before
// returned. The handlers schedule into one Executor, reused from earlier
// publications, whose batch is submitted once the last returns, and Publish
// returns after that.
//
// It answers what the last handler returned: the event, and the out
// dependency later work can wait on. With no subscribers it answers event and
// the zero JobHandle.
//
// Publishing is the only way work is scheduled. A task or a plugin's Start
// that wants jobs run publishes an event whose handlers schedule them. A
// handler publishing on k starts a separate batch; one wanting its nested
// handlers' jobs in its own batch publishes on its Executor instead.
func (k *Kernel) Publish[E any](event E) (E, JobHandle) {
	// Counted before closed is checked, so drain never finds nothing
	// outstanding while a publication that passed the check still runs.
	k.outstanding.Add(1)
	defer k.settle()
	if k.closed.Load() {
		return event, JobHandle{}
	}
	ex := k.acquireExecutor()
	event, out := ex.Publish(JobHandle{}, event)
	ex.submit()
	return event, out
}

// Notify arms sig to be signalled once job has finished, or signals it at once
// if job already has. Checking and arming are one atomic step, so no finish is
// missed however late Notify is called. job must be this
// kernel's.
//
// It allocates nothing once the kernel has armed as many Signals at once
// before, so one Signal can be armed every frame.
func (k *Kernel) Notify(sig *Signal, job JobHandle) {
	if !k.jobs.armSignal(job, sig) {
		sig.send()
	}
}

// Logger returns the Logger k uses to log errors and other messages. It is the
// same Logger every call, and is safe to use from any goroutine. It is valid
// only while k is alive.
func (k *Kernel) Logger() *Logger {
	return k.logger
}

// init brings k up: it runs every plugin's Init in order with this
// kernel's Configurator and settles registration (handler order, lock
// sets), starts k's workerCount workers, then runs every plugin's Start in
// order.
//
// It fails at the first failure, and leaves nothing behind: it stops the
// plugins already started in reverse, not the one that failed, waits for
// every job they scheduled, ends the workers, then destroys the plugins
// already initialized in reverse, logging each Stop and Destroy failure
// through LogErr, and answers that first failure.
func (k *Kernel) init() error {
	for i, plugin := range k.plugins {
		if err := plugin.Init(&Configurator{kernel: k}, k.config); err != nil {
			for initialized := i - 1; initialized >= 0; initialized-- {
				if err := k.plugins[initialized].Destroy(); err != nil {
					k.logger.LogErr(fmt.Errorf("kernel: plugin %d (%T) failed to destroy: %w", initialized, k.plugins[initialized], err))
				}
			}
			return fmt.Errorf("kernel: plugin %d (%T) failed to init: %w", i, plugin, err)
		}
	}
	// No resource or handler can be added from here on.
	for _, d := range k.dispatchers {
		d.sortHandlers()
	}
	k.locks = makeLockState(len(k.resources))

	for range k.workerCount {
		k.workers.Go(k.work)
	}
	for i, plugin := range k.plugins {
		if err := plugin.Start(k); err != nil {
			for started := i - 1; started >= 0; started-- {
				if err := k.plugins[started].Stop(k); err != nil {
					k.logger.LogErr(fmt.Errorf("kernel: plugin %d (%T) failed to stop: %w", started, k.plugins[started], err))
				}
			}
			k.drain()
			for initialized := len(k.plugins) - 1; initialized >= 0; initialized-- {
				if err := k.plugins[initialized].Destroy(); err != nil {
					k.logger.LogErr(fmt.Errorf("kernel: plugin %d (%T) failed to destroy: %w", initialized, k.plugins[initialized], err))
				}
			}
			return fmt.Errorf("kernel: plugin %d (%T) failed to start: %w", i, plugin, err)
		}
	}
	return nil
}

// destroy takes k down: it runs Stop on every plugin in reverse, logging
// each failure through LogErr, then makes Publish publish nothing from then
// on. Publish still works while the Stops run, so they can schedule
// cleanup. It then blocks until every job k has has finished, including those
// the Stops scheduled, ends k's workers, and runs Destroy on every plugin in
// reverse, logging each failure through LogErr.
func (k *Kernel) destroy() {
	for i := len(k.plugins) - 1; i >= 0; i-- {
		if err := k.plugins[i].Stop(k); err != nil {
			k.logger.LogErr(fmt.Errorf("kernel: plugin %d (%T) failed to stop: %w", i, k.plugins[i], err))
		}
	}
	k.drain()
	for i := len(k.plugins) - 1; i >= 0; i-- {
		if err := k.plugins[i].Destroy(); err != nil {
			k.logger.LogErr(fmt.Errorf("kernel: plugin %d (%T) failed to destroy: %w", i, k.plugins[i], err))
		}
	}
}

// runPinned runs pinned job on the calling goroutine, the main thread, and
// finishes it as a worker would. Supervisor.TickMainThread calls it for each
// job k handed over.
func (k *Kernel) runPinned(job JobHandle) {
	k.tasks[k.jobs.jobs.At(job.slot).task].runJob(k, job.slot)
}

// dispatcher returns the dispatcher of E in k, creating it on first use.
func (k *Kernel) dispatcher[E any]() *dispatcher[E] {
	t := reflect.TypeFor[E]()
	if d, ok := k.dispatchers[t]; ok {
		return d.(*dispatcher[E])
	}
	d := &dispatcher[E]{}
	k.dispatchers[t] = d
	return d
}

// acquireExecutor answers an Executor kept from an earlier publication, or a
// new one the first time as many publications run at once.
func (k *Kernel) acquireExecutor() *Executor {
	if ex, ok := k.executors.Pop(); ok {
		return ex
	}
	return &Executor{kernel: k}
}

// arrive hands on the job in slot, whose dependencies have all finished. A
// job that locks nothing needs no lock combiner, and is ready at once;
// otherwise it waits in arrivals for the combiner to grant its locks.
func (k *Kernel) arrive(slot uint32) {
	locks := k.taskLocks[k.jobs.jobs.At(slot).task]
	if locks.locksNothing() {
		k.makeReady(slot, locks)
		return
	}
	k.arrivals.Push(slot)
	k.combineLocks()
}

// makeReady hands on the job in slot, whose locks are held: to the main
// thread if its task is pinned there, otherwise to ready.
func (k *Kernel) makeReady(slot uint32, locks *taskLocks) {
	if locks.pinnedToMainThread {
		k.supervisor.pushPinned(k, k.jobs.handle(slot))
		return
	}
	k.ready.Push(slot)
	if k.idle.Load() > 0 {
		select {
		case k.wake <- struct{}{}:
		default: // every worker already has a token
		}
	}
}

// combineLocks becomes the lock combiner, unless another goroutine is: then
// it returns at once, and that combiner does the work. The combiner unlocks
// the resources of every finished job, then grants locks to waiting jobs,
// oldest first, each whose locks are all free; a waiting writer reserves what
// it writes, so no job behind it locks that before it does.
//
// Before giving the combiner up it looks again, so a job queued while it was
// busy, by a goroutine that then failed to become combiner, is never left
// behind.
func (k *Kernel) combineLocks() {
	for k.combinerMu.TryLock() {
		k.unlocked = k.unlocks.Drain(k.unlocked[:0])
		for _, task := range k.unlocked {
			k.locks.unlock(k.taskLocks[task])
		}
		k.arrived = k.arrivals.Drain(k.arrived[:0])
		k.waiting = append(k.waiting, k.arrived...)

		k.locks.clearReservations()
		stillWaiting := k.waiting[:0]
		for _, slot := range k.waiting {
			locks := k.taskLocks[k.jobs.jobs.At(slot).task]
			if k.locks.canLock(locks) {
				k.locks.lock(locks)
				k.makeReady(slot, locks)
				continue
			}
			k.locks.reserve(locks)
			stillWaiting = append(stillWaiting, slot)
		}
		k.waiting = stillWaiting
		k.combinerMu.Unlock()

		if k.unlocks.Len() == 0 && k.arrivals.Len() == 0 {
			return
		}
	}
}

// completeJob finishes the job in slot, which answered result: it unlocks
// its locks, sends every Signal armed for it, feeds result to every job
// scheduled after it, hands on each successor that waits for nothing else,
// and releases the slot.
func (k *Kernel) completeJob[B any](slot uint32, result B) {
	task := k.jobs.jobs.At(slot).task
	if !k.taskLocks[task].locksNothing() {
		// Unlocked first, so a successor locking the same resources can be
		// granted them in the same pass.
		k.unlocks.Push(task)
		k.combineLocks()
	}
	head := k.jobs.finishJob(slot)
	for index := head; index != concurrent.None; {
		edge := k.jobs.successorEdges.At(index)
		successor := k.jobs.jobs.At(edge.successor)
		if edge.feedsResult {
			k.tasks[successor.task].(argReceiver[B]).receiveArg(successor.arg, result)
		}
		index = edge.next
		if successor.waiting.Add(-1) == 0 {
			k.arrive(edge.successor)
		}
	}
	k.jobs.releaseSuccessors(head)
	k.jobs.releaseJob(slot)
	k.settle()
}

// work is one worker: it runs ready jobs until stop is closed, parking on
// wake while there is none.
func (k *Kernel) work() {
	for {
		if slot, ok := k.ready.Pop(); ok {
			k.tasks[k.jobs.jobs.At(slot).task].runJob(k, slot)
			continue
		}
		// Counted idle before looking again, so a job pushed in between is
		// either seen here or wakes this worker.
		k.idle.Add(1)
		if k.ready.Len() == 0 {
			select {
			case <-k.wake:
			case <-k.stop:
				return
			}
		}
		k.idle.Add(-1)
	}
}

// drain makes Publish publish nothing from now on, blocks until every job and
// publication still outstanding has finished, then ends k's workers.
func (k *Kernel) drain() {
	k.closed.Store(true)
	if k.outstanding.Load() != 0 {
		<-k.drained
	}
	close(k.stop)
	k.workers.Wait()
}

// settle marks one outstanding job or publication finished, waking drain at
// the last once k is closed.
func (k *Kernel) settle() {
	if k.outstanding.Add(-1) == 0 && k.closed.Load() {
		select {
		case k.drained <- struct{}{}:
		default: // drain already has a token
		}
	}
}
