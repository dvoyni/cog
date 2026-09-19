package kernel

import (
	"reflect"
)

// Kernel is the handle a plugin uses at runtime. It is created per dispatch and
// always passed by value: it carries the engine it belongs to and nothing else.
// Because it is scoped to one dispatch, retaining it past the handler that
// received it is a bug.
//
// A Kernel is only ever constructed by the engine, so there is no route by which
// a caller supplies one and no zero value to guard against.
type Kernel struct {
	engine *Engine
}

// ReportError sends err to the centralized error handler. It answers nothing:
// what happens next is the handler's decision, and the reporter has finished
// with the failure either way.
func (k Kernel) ReportError(err error) {
	k.engine.reportError(err)
}

// ReportErrorOnce sends errs to the centralized error handler the first time it
// is called under key, and drops them every time after.
//
// It exists because a condition that is true every frame - a missing sprite, a
// backend that never came up, a model whose node name is a typo - is worth
// saying once and is noise at the frame rate. Say it under a key that names the
// condition, and forget that key when the condition could have changed.
//
// The burst is gated as a whole rather than per error, so one load reporting a
// missing texture and an unbounded primitive reports both facts while a second
// load of the same path repeats neither. Passing no errors claims nothing.
//
// The key is any comparable value, and it must be comparable because it is a
// map key; distinct key types never collide, however their values compare, so a
// plugin naming its own type owns its own namespace. A plugin sharing a type
// with another - two plugins keyed by string - shares one, which is what the
// "sprite:" and "model:" prefixes on those keys are for. A singleton condition
// therefore names its own empty struct type rather than a bare struct{}, or
// every singleton in the engine is one condition.
//
// What does not belong here: a dedupe whose quiet is scoped to something
// narrower than the engine, and a dedupe that gates an error the holder
// returns rather than reports. A per-frame set cleared every frame wants a map
// of its own, not a family forgotten through the whole table each frame; and a
// render-thread object with no Kernel - gfx's translator, an Adapter's backend
// - hands its error back to whoever does hold one, so what it dedupes is its
// own return value rather than a report.
func (k Kernel) ReportErrorOnce[T comparable](key T, errs ...error) {
	k.engine.reportErrorOnce(key, errs)
}

// ForgetReportedError clears key, so the next ReportErrorOnce under it reports
// again. A path that failed, was fixed and reloaded must be able to speak.
//
// Forgetting is not optional for a condition that can be repaired: without the
// paired call, a sprite that failed to load once stays silent about failing
// again for the engine's life. A condition that cannot be repaired - a backend
// that never installed - simply never calls it.
func (k Kernel) ForgetReportedError[T comparable](key T) {
	k.engine.forgetReportedError(key)
}

// ForgetReportedErrors clears every key of type T that match accepts, for the
// family a single key cannot name: a model whose selector keys hang off its
// path, cleared together when the model unloads.
//
// match is never shown a key of another type, so a plugin clearing a family of
// its own keys cannot reach another plugin's. It scans the whole table, so it
// belongs on a cold path - an unload, not a frame.
func (k Kernel) ForgetReportedErrors[T comparable](match func(T) bool) {
	k.engine.forgetReportedErrors(func(key any) bool {
		typed, ok := key.(T)
		return ok && match(typed)
	})
}

// PublishEvent starts one event publication and returns its completion handle.
// Subscribers whose dependencies are satisfied run concurrently; separate
// publications are independent and may interleave. Publishing is
// fire-and-forget: the publication outlives the invocation that published it.
func (k Kernel) PublishEvent[TEvent any](event TEvent) *Publication {
	engine := k.engine
	plan := engine.registry.publications[reflect.TypeFor[TEvent]()]
	publication := newPublication()
	if plan == nil || len(plan.nodes) == 0 {
		// An event nobody subscribes to is not a fault, and neither is one whose
		// plans a failed composition never built: that composition already came
		// back from Run.
		publication.complete()
		return publication
	}

	invocation := &eventContext[TEvent]{engine: engine, event: event}
	go engine.runPublication(plan, invocation, publication)
	return publication
}

// ExecuteCommandAsync dispatches a command as an independent top-level task and
// returns immediately, with no response and no completion handle. The task
// acquires its own declared locks rather than inheriting the caller's, and its
// error goes to the centralized error handler.
//
// Because the task runs after the caller's locks are gone, the request must not
// carry anything derived from a locked resource.
func (k Kernel) ExecuteCommandAsync[
	TCommand CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
](request TRequest) {
	engine := k.engine
	cmd, ok := engine.registry.commands[reflect.TypeFor[TCommand]()]
	if !ok {
		engine.reportError(ErrExecutingUnknownCommand[TCommand]{})
		return
	}
	go dispatch[TRequest, TResponse](engine, cmd, cmd.resources.read, cmd.resources.write, request)
}

// Executioner is a Kernel that may also dispatch commands synchronously without
// declaring them. Only the engine mints one, for plugin lifecycle methods and
// host callbacks: they run outside any handler, so every command they dispatch
// acquires its own lock set. A handler receives a plain Kernel and therefore
// dispatches only through a declared Uses dispatcher or ExecuteCommandAsync.
type Executioner struct{ Kernel }

// Quitting is closed when the engine has been asked to stop, by Quit or by a
// report the error handler terminated on. It is for a plugin that owns
// something outside the engine — a listening socket, a watcher, a worker it
// started in Start — and must stop accepting new work before the engine tears
// down. It says nothing about dispatch: work already in flight finishes, and a
// dispatch after it still runs until the scheduler stops.
//
// Only lifecycle methods get an Executioner, which is where such a thing is
// started, so a per-dispatch handler cannot reach this and has no reason to.
func (e Executioner) Quitting() <-chan struct{} { return e.engine.quit }

// Describe returns the finalized architecture. An Executioner exists only once
// Run begins, so the description it returns is always the final one. The value
// is detached: it reads registry state that is immutable after finalization and
// so needs no lock, no tick and no scheduler.
func (e Executioner) Describe() ArchitectureDescription { return e.engine.Describe() }

// ExecuteCommand runs a command synchronously and returns its response.
//
// It answers with the response alone. A request this command rejects for a
// reason the caller should act on says so in that response; a dispatch the
// kernel could not perform — an unregistered command, a scheduler that has
// stopped — is reported, and the caller receives the zero response.
func (e Executioner) ExecuteCommand[
	TCommand CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
](request TRequest) TResponse {
	engine := e.engine
	cmd, ok := engine.registry.commands[reflect.TypeFor[TCommand]()]
	if !ok {
		engine.reportError(ErrExecutingUnknownCommand[TCommand]{})
		var zero TResponse
		return zero
	}
	return dispatch[TRequest, TResponse](
		engine, cmd, cmd.resources.read, cmd.resources.write, request)
}
