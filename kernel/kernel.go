package kernel

import (
	"fmt"
	"reflect"
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
	// singleThreaded kernels run every job on one goroutine of their own,
	// locked to an OS thread.
	singleThreaded bool
	// resources holds every resource by its type. Only Init adds to it, so it
	// is never changed once the kernel starts.
	resources map[reflect.Type]*resource
	// dispatchers holds the dispatcher of every event type, each as its
	// *dispatcher[E], its handlers sorted once every plugin has initialized.
	dispatchers map[reflect.Type]eventDispatcher
}

// resource is one resource of a kernel.
type resource struct {
	// index is the resource's bit in every task's lock masks.
	index int
	// value is the *T the resource lives in, which every handle to it binds.
	value any
}

// newKernel creates kernel id of supervisor, logging through supervisor's
// LogHandler, with the config each plugin is handed, running single-threaded
// if singleThreaded is set, and plugins to register in order.
func newKernel(supervisor *Supervisor, id KernelID, config any, singleThreaded bool, plugins []Plugin) *Kernel {
	return &Kernel{
		supervisor:     supervisor,
		id:             id,
		logger:         &Logger{kernel: id, handler: supervisor.logHandler},
		plugins:        plugins,
		config:         config,
		singleThreaded: singleThreaded,
		resources:      map[reflect.Type]*resource{},
		dispatchers:    map[reflect.Type]eventDispatcher{},
	}
}

// ID returns the ID Spawn assigned this kernel.
func (k *Kernel) ID() KernelID { return k.id }

// Publish delivers event to every handler subscribed to E, one by one, on the
// calling goroutine, each receiving the event and out dependency the one before
// returned. The handlers schedule into one pooled Executor, whose batch is
// submitted once the last returns, and Publish returns after that.
//
// It answers what the last handler returned: the event, and the out
// dependency later work can wait on. With no subscribers it answers event and
// the zero JobHandle.
//
// Publishing is the only way work is scheduled. A task or a plugin's Start
// that wants jobs run publishes an event whose handlers schedule them. A
// handler publishing on k starts a separate batch; one wanting its nested
// handlers' jobs in its own batch publishes on its Executor instead.
func (k *Kernel) Publish[E any](event E) (E, JobHandle) { return event, JobHandle{} }

// Notify arms sig to be signalled once job has finished, or signals it at once
// if job already has. Checking and arming happen under the kernel's lock, so
// no finish is missed however late Notify is called. job must be this
// kernel's.
//
// It allocates nothing, so one Signal can be armed every frame.
func (k *Kernel) Notify(sig *Signal, job JobHandle) {}

// Logger returns the Logger k uses to log errors and other messages. It is the
// same Logger every call, and is safe to use from any goroutine. It is valid
// only while k is alive.
func (k *Kernel) Logger() *Logger {
	return k.logger
}

// init brings k up: it runs every plugin's Init in order with this
// kernel's Configurator and settles registration (handler order, lock
// sets), starts the goroutines k's jobs run on (one locked to its
// own OS thread when k is singleThreaded, otherwise GOMAXPROCS of them), then
// runs every plugin's Start in order.
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
	for _, d := range k.dispatchers {
		d.sortHandlers()
	}
	for i, plugin := range k.plugins {
		if err := plugin.Start(k); err != nil {
			for started := i - 1; started >= 0; started-- {
				if err := k.plugins[started].Stop(k); err != nil {
					k.logger.LogErr(fmt.Errorf("kernel: plugin %d (%T) failed to stop: %w", started, k.plugins[started], err))
				}
			}
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
	for i := len(k.plugins) - 1; i >= 0; i-- {
		if err := k.plugins[i].Destroy(); err != nil {
			k.logger.LogErr(fmt.Errorf("kernel: plugin %d (%T) failed to destroy: %w", i, k.plugins[i], err))
		}
	}
}

// runPinned runs pinned job on the calling goroutine, the main thread, and
// finishes it as a worker would. Supervisor.TickMainThread calls it for each
// job k handed over.
func (k *Kernel) runPinned(job JobHandle) {}

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
