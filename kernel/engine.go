// Package kernel is a small microkernel: plugins register resources, commands,
// and event subscriptions, then communicate through them. Events can be
// published asynchronously to ordered subscribers; commands run synchronously
// and return a result; resources are shared state whose access is serialized by
// a lock scheduler.
//
// The Engine owns composition and lifetime. Everything a plugin does at runtime
// goes through a Kernel: a small value created for each dispatch that carries
// the engine, the invocation context, and the caller's held locks.
package kernel

import (
	"context"
	"errors"
	"iter"
	"log"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Engine is the composed microkernel. Its registry is built during sequential
// plugin registration and read afterward; its scheduler serializes resource
// access; and ctx bounds its lifetime.
type Engine struct {
	config        map[PluginName]any
	pluginNames   map[PluginName]struct{}
	registry      *registry
	scheduler     *scheduler
	ctx           context.Context
	cancel        context.CancelCauseFunc
	plugins       []Plugin
	host          PluginHost
	schedulerDone chan error
	errorMu       sync.Mutex
	errorHandler  ErrorHandler
	// reported is the set of keys ReportErrorOnce has already spoken under. It
	// lives here, under errorMu, so that the dedupe and the report it guards
	// are taken under one lock: the conditions it gates are noticed from at
	// least two threads - a lookup on the update thread, a render handler on
	// the render thread - and the per-plugin maps this replaced each guarded
	// themselves, or did not.
	//
	// The key is boxed, and interface equality includes the dynamic type, so a
	// plugin's own key type never collides with another's however its values
	// compare. Entries are dropped only by ForgetReportedError and
	// ForgetReportedErrors; a condition nobody ever fixes holds one entry for
	// the engine's life, which is the point.
	reported   map[any]struct{}
	terminated bool
	// failure carries the terminating cause for the dispatch path to read. It
	// mirrors terminated, which only the locked paths read, because runTask
	// consults it on every command and every subscriber run: taking errorMu
	// there would put one shared mutex across every parallel handler in the
	// engine. It is written under errorMu wherever terminated is set, so the
	// two never disagree, and read with a single relaxed load.
	failure   atomic.Pointer[error]
	ready     chan struct{}
	readyOnce sync.Once
}

// New creates an unstarted engine with plugin configuration keyed by name.
func New(config map[PluginName]any) *Engine {
	e := &Engine{
		config:       config,
		pluginNames:  map[PluginName]struct{}{},
		scheduler:    newScheduler(),
		errorHandler: defaultErrorHandler,
		ready:        make(chan struct{}),
	}
	e.registry = &registry{
		resources:     map[reflect.Type]*resource{},
		commands:      map[reflect.Type]*command{},
		subscriptions: map[reflect.Type][]subscription{},
		publications:  map[reflect.Type]*publicationPlan{},

		adapterContributions: map[reflect.Type][]adapterContribution{},
	}
	return e
}

// Handler sets the centralized error handler. A nil handler restores the
// default, which logs errors and terminates the engine.
func (e *Engine) Handler(errorHandler ErrorHandler) *Engine {
	if errorHandler == nil {
		errorHandler = defaultErrorHandler
	}
	e.errorHandler = errorHandler
	return e
}

// Ready is closed after Run has started the scheduler and attempted plugin
// startup. It says the attempt is over, not that it succeeded: a composition
// that failed closes Ready too, so a caller that waits on it asks Err what it
// woke up to.
func (e *Engine) Ready() <-chan struct{} { return e.ready }

// Err reports the cause that terminated the engine, or nil while it is live,
// the way context.Err does. A failed composition and a plugin panic mid-run
// both answer here, and it is the same cause a refused dispatch returns inside
// ErrEngineTerminated. It takes no lock and is safe from any goroutine.
func (e *Engine) Err() error {
	if cause := e.failure.Load(); cause != nil {
		return *cause
	}
	return nil
}

// WithPlugins validates, orders, and registers plugins before Run starts them.
func (e *Engine) WithPlugins(plugins ...Plugin) *Engine {
	accepted := make([]Plugin, 0, len(plugins))
	for _, plugin := range plugins {
		if _, ok := e.pluginNames[plugin.Name()]; ok {
			e.failComposition(ErrConflictingPluginName{plugin.Name()})
			return e
		}
		e.pluginNames[plugin.Name()] = struct{}{}
		accepted = append(accepted, plugin)
	}

	// Every declared dependency must be among the registered plugins; the engine
	// validates presence only and never reorders or auto-adds plugins.
	for _, plugin := range accepted {
		for _, dep := range plugin.Dependencies() {
			if _, ok := e.pluginNames[dep]; ok {
				continue
			}
			e.failComposition(ErrMissingPluginDependency{Plugin: plugin.Name(), Dependency: dep})
			return e
		}
	}
	ordered, cycle := orderPlugins(accepted)
	if cycle != nil {
		e.failComposition(ErrPluginDependencyCycle{Plugins: cycle})
		return e
	}
	accepted = ordered

	for _, candidate := range pluginsOf[PluginHost](accepted) {
		if e.host != nil {
			e.failComposition(ErrMultipleHosts{First: e.host.Name(), Second: candidate.Name()})
			return e
		}
		e.host = candidate
	}

	closure := e.dependencyClosure(accepted)
	for _, plugin := range accepted {
		registrar := &Registrar{registry: e.registry, owner: plugin.Name(), allowed: closure[plugin.Name()]}
		if err := callPluginBoundary(plugin.Name(), "Register", func() error {
			return plugin.Register(registrar, e.config[plugin.Name()])
		}); err != nil {
			e.failComposition(err)
			return e
		}
	}
	if errs := e.registry.finalize(closure); len(errs) > 0 {
		e.failComposition(errors.Join(errs...))
		return e
	}
	e.plugins = accepted

	return e
}

// dependencyClosure maps each plugin to the plugins it may couple to: itself plus
// the transitive closure of its declared dependencies.
func (e *Engine) dependencyClosure(plugins []Plugin) map[PluginName]map[PluginName]struct{} {
	direct := make(map[PluginName][]PluginName, len(plugins))
	for _, plugin := range plugins {
		direct[plugin.Name()] = plugin.Dependencies()
	}
	closure := make(map[PluginName]map[PluginName]struct{}, len(plugins))
	for _, plugin := range plugins {
		reachable := map[PluginName]struct{}{plugin.Name(): {}}
		pending := append([]PluginName(nil), direct[plugin.Name()]...)
		for len(pending) > 0 {
			next := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if _, seen := reachable[next]; seen {
				continue
			}
			reachable[next] = struct{}{}
			pending = append(pending, direct[next]...)
		}
		closure[plugin.Name()] = reachable
	}
	return closure
}

func (e *Engine) failComposition(err error) {
	e.reportError(err)
	e.errorMu.Lock()
	e.terminated = true
	e.recordFailureLocked(err)
	e.errorMu.Unlock()
	e.markReady()
}

// recordFailureLocked publishes the terminating cause to the dispatch path,
// keeping the first one: the error that terminated the engine is the one worth
// reporting, and whatever it knocked over afterwards is not.
func (e *Engine) recordFailureLocked(err error) {
	if err == nil || e.failure.Load() != nil {
		return
	}
	e.failure.Store(&err)
}

func (e *Engine) markReady() { e.readyOnce.Do(func() { close(e.ready) }) }

// pluginsOf yields every plugin satisfying T, in registration order, paired
// with its index in plugins. It is the engine's only filtered plugin lookup:
// the host, start and stop passes all ask it, and nothing outside the engine
// can. The index is what lets Run cut the list at a plugin whose Start failed.
func pluginsOf[T any](plugins []Plugin) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for index, plugin := range plugins {
			match, ok := any(plugin).(T)
			if !ok {
				continue
			}
			if !yield(index, match) {
				return
			}
		}
	}
}

func orderPlugins(plugins []Plugin) ([]Plugin, []PluginName) {
	byName := make(map[PluginName]int, len(plugins))
	for i, plugin := range plugins {
		byName[plugin.Name()] = i
	}

	ordered := make([]Plugin, 0, len(plugins))
	completed := make(map[PluginName]struct{}, len(plugins))
	for len(ordered) < len(plugins) {
		next := -1
		for i, plugin := range plugins {
			if _, ok := completed[plugin.Name()]; ok {
				continue
			}
			ready := true
			for _, dependency := range plugin.Dependencies() {
				if _, registered := byName[dependency]; !registered {
					continue
				}
				if _, done := completed[dependency]; !done {
					ready = false
					break
				}
			}
			if ready {
				next = i
				break
			}
		}
		if next < 0 {
			cycle := make([]PluginName, 0, len(plugins)-len(ordered))
			for _, plugin := range plugins {
				if _, ok := completed[plugin.Name()]; !ok {
					cycle = append(cycle, plugin.Name())
				}
			}
			return nil, cycle
		}
		plugin := plugins[next]
		ordered = append(ordered, plugin)
		completed[plugin.Name()] = struct{}{}
	}
	return ordered, nil
}

// Run starts plugins in dependency order, runs the optional Host, and stops
// successfully started plugins in reverse order. Without a Host it blocks until
// ctx is canceled.
//
// An engine whose composition failed never starts: Run returns at once, and Err
// carries the cause for the caller that waited on Ready.
func (e *Engine) Run(ctx context.Context) *Engine {
	if e.terminated || e.ctx != nil {
		return e
	}
	e.ctx, e.cancel = context.WithCancelCause(ctx)
	e.schedulerDone = make(chan error, 1)
	go func() {
		err := e.scheduler.run(e.ctx)
		if err != nil && !isCancellation(err) {
			e.reportError(err)
		}
		e.schedulerDone <- err
		e.cancel(nil)
	}()

	runtime := e.executioner(e.ctx)
	// started is the prefix of the plugin list that Run is responsible for
	// stopping: every plugin up to, but not including, the one whose Start failed.
	started := e.plugins
	for index, starter := range pluginsOf[PluginStarter](e.plugins) {
		if err := callPluginBoundary(starter.Name(), "Start", func() error {
			return starter.Start(runtime)
		}); err != nil {
			e.reportError(err)
			started = e.plugins[:index]
			break
		}
	}
	e.markReady()
	if len(started) == len(e.plugins) && e.ctx.Err() == nil {
		if e.host != nil {
			if err := callPluginBoundary(e.host.Name(), "Run", func() error {
				return e.host.Run(runtime)
			}); err != nil {
				e.reportError(err)
			}
		} else {
			<-e.ctx.Done()
		}
	}
	e.cancel(nil)
	shutdown := e.executioner(context.WithoutCancel(ctx))
	stoppers := make([]PluginStopper, 0, len(started))
	for _, stopper := range pluginsOf[PluginStopper](started) {
		stoppers = append(stoppers, stopper)
	}
	var shutdownErrs []error
	for i := len(stoppers) - 1; i >= 0; i-- {
		stopper := stoppers[i]
		if err := callPluginBoundary(stopper.Name(), "Stop", func() error {
			return stopper.Stop(shutdown)
		}); err != nil {
			shutdownErrs = append(shutdownErrs, err)
		}
	}
	if err := errors.Join(shutdownErrs...); err != nil {
		e.observeShutdownError(err)
	}
	<-e.schedulerDone
	return e
}

// Executioner returns a root Executioner for dispatching from outside a plugin,
// such as from a composition root or a test. It holds no locks, so commands
// acquire their own declared set from the scheduler. On a terminated engine
// everything it dispatches is refused with ErrEngineTerminated, so a caller that
// went ahead after Ready closed reads the cause rather than a plugin's panic.
func (e *Engine) Executioner() Executioner { return e.executioner(e.ctx) }

// executioner builds a root Executioner: bound to ctx, holding no locks, so
// commands it dispatches acquire their own from the scheduler.
func (e *Engine) executioner(ctx context.Context) Executioner {
	if ctx == nil {
		ctx = context.Background()
	}
	return Executioner{Kernel{engine: e, ctx: ctx, scope: ctx, bounded: ctx == e.ctx}}
}

// runTask executes one scheduled unit of work. It is the one funnel every
// dispatch passes: commands through dispatch, events through runPublication.
//
// A terminated engine refuses here, before the task runs, so no plugin code is
// entered on an engine whose composition never bound its handles.
//
// Before Run there is no coordinator to grant locks, so the task runs directly.
// That path is for a healthy engine that has not started yet - registration is
// single-threaded - which is why the refusal keys on termination and not on the
// absent scheduler.
func (e *Engine) runTask(t task, ctx context.Context) error {
	if refused := e.refusal(); refused != nil {
		return refused
	}
	if e.ctx == nil {
		return t.run(ctx)
	}
	return e.scheduler.execute(t, ctx)
}

// refusal is what a dispatch on a terminated engine gets, or nil while the
// engine is live. One relaxed load and no lock: it sits on the dispatch path.
func (e *Engine) refusal() error {
	if cause := e.failure.Load(); cause != nil {
		return ErrEngineTerminated{Cause: *cause}
	}
	return nil
}

func (e *Engine) observeShutdownError(err error) {
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	e.errorHandler(err)
}

// reportError sends err to the centralized error handler and reports whether it
// requested engine termination. Handler calls are serialized; a terminating
// error becomes the engine cancellation cause.
func (e *Engine) reportError(err error) bool {
	if err == nil {
		return e.terminated
	}
	if e.ctx != nil && e.ctx.Err() != nil && isCancellation(err) {
		return true
	}
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	return e.reportLocked(err)
}

// reportLocked is one report with errorMu already held, so that a caller which
// has more to do under that lock - deduping a key, firing the rest of a burst -
// does it without releasing and retaking it.
func (e *Engine) reportLocked(err error) bool {
	if e.terminated || (e.ctx != nil && e.ctx.Err() != nil) {
		return true
	}
	var panicErr ErrPluginPanic
	handlerTerminate := e.errorHandler(err)
	terminate := errors.As(err, &panicErr) || handlerTerminate
	if terminate {
		e.terminated = true
		e.recordFailureLocked(err)
		if e.cancel != nil {
			e.cancel(err)
		}
		return true
	}
	return false
}

// reportErrorOnce fires a burst of reports the first time key is seen and drops
// it every time after, reporting whether engine termination was requested.
//
// The burst is claimed and fired under one hold of errorMu, so two threads
// noticing the same condition in the same instant report it once rather than
// racing between the check and the reports.
//
// An empty burst claims nothing: a load that gathered no faults must leave the
// key free for the one that does.
func (e *Engine) reportErrorOnce(key any, errs []error) bool {
	if len(errs) == 0 {
		return e.terminated
	}
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	if e.terminated || (e.ctx != nil && e.ctx.Err() != nil) {
		return true
	}
	if _, done := e.reported[key]; done {
		return false
	}
	if e.reported == nil {
		e.reported = map[any]struct{}{}
	}
	e.reported[key] = struct{}{}
	terminate := false
	for _, err := range errs {
		if err == nil {
			continue
		}
		terminate = e.reportLocked(err) || terminate
	}
	return terminate
}

// forgetReportedError drops one key, so the next report under it speaks again.
func (e *Engine) forgetReportedError(key any) {
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	delete(e.reported, key)
}

// forgetReportedErrors drops every key match accepts. It scans the whole table,
// which is why the kernel offers it only for the family case a single key
// cannot express.
func (e *Engine) forgetReportedErrors(match func(any) bool) {
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	for key := range e.reported {
		if match(key) {
			delete(e.reported, key)
		}
	}
}

func callPluginBoundary(plugin PluginName, boundary string, call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = ErrPluginPanic{Plugin: plugin, Boundary: boundary, Recovered: recovered, Stack: debug.Stack()}
		}
	}()
	return call()
}

func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func defaultErrorHandler(err error) bool {
	log.Printf("kernel: %v", err)
	return true
}
