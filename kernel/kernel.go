package kernel

import (
	"reflect"

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
	// graph is k's job graph, which hands every job that waits for nothing
	// any more to the combiner.
	graph jobGraph
	// executors holds the Executors of finished publications, for the next
	// to reuse.
	executors concurrent.Queue[*Executor]

	// combiner grants every job whose dependencies have all finished its
	// locks, and hands it to the runner.
	combiner lockCombiner

	// lifecycle runs k's plugins through Init, Start, Stop and Destroy.
	lifecycle lifecycle

	// runner runs k's jobs, on its workers or on the main thread for pinned
	// ones, and counts what is outstanding so k can drain.
	runner runner
}

// resource is one resource of a kernel.
type resource struct {
	// index is the resource's bit in every task's lock masks.
	index int
	// value is the *T the resource lives in, which every handle to it binds.
	value any
}

// newKernel creates kernel id of supervisor, logging through supervisor's
// LogHandler, with the config each plugin is handed its part of, running its
// jobs on workerCount workers, and plugins to register in order.
func newKernel(supervisor *Supervisor, id KernelID, config map[PluginName]any, workerCount int, plugins []Plugin) *Kernel {
	k := &Kernel{
		supervisor:  supervisor,
		id:          id,
		logger:      &Logger{kernel: id, handler: supervisor.logHandler},
		workerCount: workerCount,
		resources:   map[reflect.Type]*resource{},
		dispatchers: map[reflect.Type]eventDispatcher{},
		tasks:       []runnable{joinTask{}},
		taskLocks:   []*taskLocks{{}},
		lifecycle:   lifecycle{plugins: plugins, config: config},
	}
	k.graph.kernel = k
	k.combiner.kernel = k
	k.runner.kernel = k
	k.lifecycle.kernel = k
	return k
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
	open := k.runner.add(1)
	defer k.runner.settle()
	if !open {
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
	if !k.graph.armSignal(job, sig) {
		sig.send()
	}
}

// Logger returns the Logger k uses to log errors and other messages. It is the
// same Logger every call, and is safe to use from any goroutine. It is valid
// only while k is alive.
func (k *Kernel) Logger() *Logger {
	return k.logger
}

// init brings k up: it runs every plugin's Init and settles registration
// (handler order, lock sets), starts k's workerCount workers, then runs every
// plugin's Start.
//
// It fails at the first failure, and leaves nothing behind: a failed Init has
// already destroyed the plugins initialized before it; after a failed Start,
// once the plugins started before it have stopped, init waits for every job
// they scheduled, ends the workers, and destroys every plugin. It answers
// that first failure.
func (k *Kernel) init() error {
	if err := k.lifecycle.init(); err != nil {
		return err
	}
	// No resource or handler can be added from here on.
	for _, d := range k.dispatchers {
		d.sortHandlers()
	}
	k.combiner.locks = makeLockState(len(k.resources))

	k.runner.start(k.workerCount)
	if err := k.lifecycle.start(); err != nil {
		k.runner.drain()
		k.lifecycle.destroy()
		return err
	}
	return nil
}

// destroy takes k down: it runs Stop on every plugin in reverse, then makes
// Publish publish nothing from then on. Publish still works while the Stops
// run, so they can schedule cleanup. It then blocks until every job k has
// has finished, including those the Stops scheduled, ends k's workers, and
// runs Destroy on every plugin in reverse. Each failure is logged through
// LogErr.
func (k *Kernel) destroy() {
	k.lifecycle.stop()
	k.runner.drain()
	k.lifecycle.destroy()
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

// completeJob finishes the job in slot, which answered result: it releases
// its locks, then finishes it in the job graph, which hands on every
// successor that waits for nothing else.
func (k *Kernel) completeJob[B any](slot uint32, result B) {
	// Released first, so a successor locking the same resources can be
	// granted them in the same pass.
	k.combiner.release(k.graph.task(slot))
	k.graph.finishJob(slot, result)
	k.runner.settle()
}
