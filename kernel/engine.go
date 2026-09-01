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
	"log"
	"reflect"
	"runtime/debug"
	"sync"
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
	terminated    bool
	ready         chan struct{}
	readyOnce     sync.Once
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

// Ready is closed after Run has started the scheduler and attempted plugin startup.
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

	for _, plugin := range accepted {
		if candidate, ok := plugin.(PluginHost); ok {
			if e.host != nil {
				e.failComposition(ErrMultipleHosts{First: e.host.Name(), Second: plugin.Name()})
				return e
			}
			e.host = candidate
		}
		registrar := &Registrar{registry: e.registry, owner: plugin.Name()}
		if err := callPluginBoundary(plugin.Name(), "Register", func() error {
			return plugin.Register(registrar, e.config[plugin.Name()])
		}); err != nil {
			e.failComposition(err)
			return e
		}
	}
	if errs := e.registry.finalize(e.dependencyClosure(accepted)); len(errs) > 0 {
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
	e.terminated = true
	e.markReady()
}

func (e *Engine) markReady() { e.readyOnce.Do(func() { close(e.ready) }) }

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
	started := make([]Plugin, 0, len(e.plugins))
	for _, plugin := range e.plugins {
		if starter, ok := plugin.(PluginStarter); ok {
			if err := callPluginBoundary(plugin.Name(), "Start", func() error {
				return starter.Start(runtime)
			}); err != nil {
				e.reportError(err)
				break
			}
		}
		started = append(started, plugin)
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
	var shutdownErrs []error
	for i := len(started) - 1; i >= 0; i-- {
		plugin := started[i]
		stopper, ok := plugin.(PluginStopper)
		if !ok {
			continue
		}
		if err := callPluginBoundary(plugin.Name(), "Stop", func() error {
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
// acquire their own declared set from the scheduler.
func (e *Engine) Executioner() Executioner { return e.executioner(e.ctx) }

// executioner builds a root Executioner: bound to ctx, holding no locks, so
// commands it dispatches acquire their own from the scheduler.
func (e *Engine) executioner(ctx context.Context) Executioner {
	if ctx == nil {
		ctx = context.Background()
	}
	return Executioner{Kernel{engine: e, ctx: ctx, scope: ctx, bounded: ctx == e.ctx}}
}

// runTask executes one scheduled unit of work. Before Run there is no coordinator
// to grant locks, so the task runs directly; registration is single-threaded and
// an engine whose composition failed must fail its dispatches rather than block
// forever on a channel nobody is reading.
func (e *Engine) runTask(t task, ctx context.Context) error {
	if e.ctx == nil {
		return t.run(ctx)
	}
	return e.scheduler.execute(t, ctx)
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
	if e.terminated || (e.ctx != nil && e.ctx.Err() != nil) {
		return true
	}
	var panicErr ErrPluginPanic
	handlerTerminate := e.errorHandler(err)
	terminate := errors.As(err, &panicErr) || handlerTerminate
	if terminate {
		e.terminated = true
		if e.cancel != nil {
			e.cancel(err)
		}
		return true
	}
	return false
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
