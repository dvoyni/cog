// Package kernel is a small microkernel: plugins register resources, commands,
// and event subscriptions, then communicate through them. Events can be
// published asynchronously to ordered subscribers; commands run synchronously
// and return a result; resources are shared state whose access is serialized by
// a lock scheduler.
//
// The Engine owns composition and lifetime. Everything a plugin does at runtime
// goes through a Kernel: a small value created for each dispatch, carrying the
// engine it belongs to. A run ends when the Host returns, which Quit asks it to
// do, or when a reported error terminates it.
package kernel

import (
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
	config       map[PluginName]any
	pluginNames  map[PluginName]struct{}
	registry     *registry
	scheduler    *scheduler
	plugins      []Plugin
	host         PluginHost
	errorMu      sync.Mutex
	errorHandler ErrorHandler
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
	reported map[any]struct{}
	// cause is the first non-nil verdict of the engine's life, and what Run
	// returns. It is written under errorMu and read once Run's goroutines have
	// joined. composition is the initialization failure, settled before any of
	// them exist.
	cause       error
	composition error

	ready     chan struct{}
	readyOnce sync.Once
	// quit is closed by Quit and by a terminating report. It is what a Run
	// without a Host blocks on, and what tells a running engine to unwind.
	quit     chan struct{}
	quitOnce sync.Once
	running  atomic.Bool
}

// New creates an unstarted engine with plugin configuration keyed by name.
func New(config map[PluginName]any) *Engine {
	e := &Engine{
		config:       config,
		pluginNames:  map[PluginName]struct{}{},
		scheduler:    newScheduler(),
		errorHandler: defaultErrorHandler,
		ready:        make(chan struct{}),
		quit:         make(chan struct{}),
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

// Ready is closed after Run has attempted plugin startup. It says the attempt
// is over, not that it succeeded: a composition that failed closes Ready too,
// and what it failed with is what Run returns.
func (e *Engine) Ready() <-chan struct{} { return e.ready }

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
	if err := e.registry.finalize(closure); err != nil {
		e.failComposition(err)
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

// failComposition records the initialization failure Run answers with. It runs
// during WithPlugins, which is single-threaded, and reports it too, so that the
// handler says it the way it says everything else.
func (e *Engine) failComposition(err error) {
	e.composition = err
	e.reportError(err)
	e.markReady()
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
// It answers how the run ended: nil on an ordinary quit, the initialization
// failure of a composition that never started, or the first error the handler
// terminated on.
func (e *Engine) Run() error {
	if e.composition != nil {
		return e.composition
	}
	if !e.running.CompareAndSwap(false, true) {
		return e.terminatingCause()
	}

	runtime := e.executioner()
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
	if len(started) == len(e.plugins) && !e.quitting() {
		if e.host != nil {
			// The Host owns a blocking loop, so something has to ask it to leave
			// one when the engine is told to stop or a report terminates the run.
			// It happens here rather than at the point of the verdict, so that
			// plugin code is never entered while the report lock is held.
			returned := make(chan struct{})
			go func() {
				select {
				case <-e.quit:
					e.host.Quit()
				case <-returned:
				}
			}()
			err := callPluginBoundary(e.host.Name(), "Run", func() error {
				return e.host.Run(runtime)
			})
			close(returned)
			if err != nil {
				e.reportError(err)
			}
		} else {
			<-e.quit
		}
	}
	e.markQuit()
	shutdown := e.executioner()
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
		e.reportError(err)
	}
	e.scheduler.shutdown()
	return e.terminatingCause()
}

// terminatingCause reports the first non-nil verdict, or nil on an ordinary
// quit. It is the whole of what an engine remembers about how it ended.
func (e *Engine) terminatingCause() error {
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	return e.cause
}

// Quit asks a running engine to stop, with no failure. A Run without a Host
// returns once every started plugin has stopped; a Run with one asks the Host
// to leave its loop first, because the Host owns that loop and only it can
// leave it. It is safe from any goroutine and safe to call twice.
func (e *Engine) Quit() { e.markQuit() }

// markQuit closes the quit channel exactly once.
func (e *Engine) markQuit() { e.quitOnce.Do(func() { close(e.quit) }) }

// quitting reports whether the engine has been asked to stop.
func (e *Engine) quitting() bool {
	select {
	case <-e.quit:
		return true
	default:
		return false
	}
}

// Executioner returns a root Executioner for dispatching from outside a plugin,
// such as from a composition root or a test. It holds no locks, so commands
// acquire their own declared set from the scheduler. Nothing it dispatches is
// refused for being late: work runs until the scheduler stops, and how the run
// ended is what Run answers with.
func (e *Engine) Executioner() Executioner { return e.executioner() }

// executioner builds a root Executioner: it holds no locks, so commands it
// dispatches acquire their own from the scheduler.
func (e *Engine) executioner() Executioner {
	return Executioner{Kernel{engine: e}}
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
func (e *Engine) runTask(t task, invocation any) error {
	// An empty lock set can never conflict with anything, so the coordinator has
	// no decision to take: a declared dispatch was granted its callee's locks at
	// composition, and a command declaring none asks for none. Going to the
	// coordinator anyway costs a channel round-trip to be told what finalisation
	// already settled - 1113 ns against 0.49 ns for the call itself, which is why
	// the ECS wrote Uses off. This changes no lock decision; it declines to pay
	// for one already taken.
	read, write := t.locks()
	if len(read) == 0 && len(write) == 0 {
		if e.scheduler.stopped() {
			return ErrSchedulerStopped{}
		}
		return t.run(invocation)
	}
	return e.scheduler.execute(t, invocation, read, write)
}

// reportError sends err to the centralized error handler. Handler calls are
// serialized, and the first error the handler terminates on is the one Run
// answers with.
func (e *Engine) reportError(err error) {
	if err == nil {
		return
	}
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	e.reportLocked(err)
}

// reportLocked is one report with errorMu already held, so that a caller which
// has more to do under that lock - deduping a key, firing the rest of a burst -
// does it without releasing and retaking it.
//
// Every report reaches the handler, including one arriving after the engine is
// already going down: silence there would hide whatever the first failure
// knocked over. Only the first non-nil verdict becomes the cause.
func (e *Engine) reportLocked(err error) {
	verdict := e.errorHandler(err)
	if verdict == nil {
		return
	}
	if e.cause == nil {
		e.cause = verdict
	}
	e.markQuit()
}

// reportErrorOnce fires a burst of reports the first time key is seen and drops
// it every time after.
//
// The burst is claimed and fired under one hold of errorMu, so two threads
// noticing the same condition in the same instant report it once rather than
// racing between the check and the reports.
//
// An empty burst claims nothing: a load that gathered no faults must leave the
// key free for the one that does.
func (e *Engine) reportErrorOnce(key any, errs []error) {
	if len(errs) == 0 {
		return
	}
	e.errorMu.Lock()
	defer e.errorMu.Unlock()
	if _, done := e.reported[key]; done {
		return
	}
	if e.reported == nil {
		e.reported = map[any]struct{}{}
	}
	e.reported[key] = struct{}{}
	for _, err := range errs {
		if err != nil {
			e.reportLocked(err)
		}
	}
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

// shutdownAside drops ErrSchedulerStopped and passes everything else through.
// A dispatch that arrives after the coordinator has gone is the engine ending,
// not a failure in the thing that dispatched: Quit is an ordinary end, and a
// report would turn every in-flight call at shutdown into a fault the handler
// has to recognise and forgive.
func shutdownAside(err error) error {
	var stopped ErrSchedulerStopped
	if errors.As(err, &stopped) {
		return nil
	}
	return err
}

func callPluginBoundary(plugin PluginName, boundary string, call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = ErrPluginPanic{Plugin: plugin, Boundary: boundary, Recovered: recovered, Stack: debug.Stack()}
		}
	}()
	return call()
}

// defaultErrorHandler is the engine's opinion of last resort, and it is only an
// opinion: a game that installs its own replaces it whole.
//
// It says every error out loud and terminates on a plugin panic alone. A panic
// means a handler stopped in the middle of what it was doing, so whatever it
// was mutating is in a state nobody described. Anything else - a missing
// texture, a model that would not parse - is a thing a game is expected to
// survive, and terminating on it would make the frame rate a liability.
func defaultErrorHandler(err error) error {
	log.Printf("kernel: %v", err)
	var panicErr ErrPluginPanic
	if errors.As(err, &panicErr) {
		return err
	}
	return nil
}
