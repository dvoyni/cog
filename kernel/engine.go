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
	"reflect"
	"runtime/debug"
	"slices"
	"strings"
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

func (e *Engine) markReady() { e.readyOnce.Do(func() { close(e.ready) }) }

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

// Describe returns a detached description of the finalized engine architecture.
func (e *Engine) Describe() ArchitectureDescription {
	description := ArchitectureDescription{
		Plugins:       make([]PluginDescription, 0, len(e.plugins)),
		Resources:     make([]ResourceDescription, 0, len(e.registry.resources)),
		Ports:         make([]PortDescription, 0, len(e.registry.adapterDeclarations)),
		Commands:      make([]CommandDescription, 0, len(e.registry.commands)),
		Subscriptions: make([]SubscriptionDescription, 0),
	}
	for _, plugin := range e.plugins {
		_, host := plugin.(PluginHost)
		_, starts := plugin.(PluginStarter)
		_, stops := plugin.(PluginStopper)
		description.Plugins = append(description.Plugins, PluginDescription{
			Name: plugin.Name(), Dependencies: slices.Clone(plugin.Dependencies()),
			Host: host, Starts: starts, Stops: stops,
		})
	}
	for resourceType, resource := range e.registry.resources {
		description.Resources = append(description.Resources, ResourceDescription{Type: resourceType, Owner: resource.owner})
	}
	for _, declaration := range e.registry.adapterDeclarations {
		description.Ports = append(description.Ports, PortDescription{
			Type: declaration.port, Interface: declaration.iface, Owner: declaration.owner,
			Collects: declaration.collects,
			Adapters: describeAdapters(e.registry.adapterContributions[declaration.port]),
		})
	}
	for commandType, command := range e.registry.commands {
		reads, writes, uses, selfExclusive := describeAccess(command.resources)
		description.Commands = append(description.Commands, CommandDescription{
			Type: commandType, Owner: command.owner, Reads: reads, Writes: writes, Uses: uses,
			SelfExclusive: selfExclusive,
		})
	}
	for eventType, plan := range e.registry.publications {
		for i, node := range plan.nodes {
			dependencyTypes := make([]reflect.Type, 0, node.dependsOn)
			for dependencyIndex, dependency := range plan.nodes {
				if slices.Contains(dependency.dependents, i) {
					dependencyTypes = append(dependencyTypes, plan.nodes[dependencyIndex].task.orderID())
				}
			}
			owner, access := node.task.coupling()
			reads, writes, uses, selfExclusive := describeAccess(access)
			description.Subscriptions = append(description.Subscriptions, SubscriptionDescription{
				Event: eventType, Type: node.task.orderID(), Owner: owner,
				Phase: subscriptionPhase(node.task), DependsOn: dependencyTypes,
				Reads: reads, Writes: writes, Uses: uses, SelfExclusive: selfExclusive,
			})
		}
	}
	slices.SortFunc(description.Resources, func(a, b ResourceDescription) int { return compareTypes(a.Type, b.Type) })
	slices.SortStableFunc(description.Ports, func(a, b PortDescription) int {
		if portOrder := compareTypes(a.Type, b.Type); portOrder != 0 {
			return portOrder
		}
		return strings.Compare(string(a.Owner), string(b.Owner))
	})
	slices.SortFunc(description.Commands, func(a, b CommandDescription) int { return compareTypes(a.Type, b.Type) })
	slices.SortFunc(description.Subscriptions, func(a, b SubscriptionDescription) int {
		if eventOrder := compareTypes(a.Event, b.Event); eventOrder != 0 {
			return eventOrder
		}
		return compareTypes(a.Type, b.Type)
	})
	description.Contention = e.registry.describeContention()
	return description
}

func callPluginBoundary(plugin PluginName, boundary string, call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = ErrPluginPanic{Plugin: plugin, Boundary: boundary, Recovered: recovered, Stack: debug.Stack()}
		}
	}()
	return call()
}
