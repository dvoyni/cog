package kernel

import (
	"context"
	"reflect"
)

// Kernel is the handle a plugin uses at runtime. It is created per dispatch and
// always passed by value: the engine it belongs to, the invocation context, and
// the lifetime asynchronous work inherits. Because it is scoped to one dispatch,
// retaining it past the handler that received it is a bug.
//
// ctx bounds this invocation; scope bounds work that outlives it. They differ
// inside a dispatch: a command's ctx ends when the command returns, but an event
// it publishes is fire-and-forget and must survive that.
//
// The zero Kernel is not usable; every method panics on it.
type Kernel struct {
	engine *Engine
	ctx    context.Context
	scope  context.Context
	// bounded records that ctx already derives from the engine context, so a
	// dispatch does not need to wrap it again to inherit engine cancellation.
	bounded bool
}

func (k Kernel) bound() *Engine {
	if k.engine == nil {
		panic("kernel: zero Kernel used outside a handler scope")
	}
	return k.engine
}

// Context reports the invocation context: the publication's for a subscriber,
// the caller's for a command, and the engine's for a plugin lifecycle method.
func (k Kernel) Context() context.Context {
	k.bound()
	return k.ctx
}

// WithContext derives a kernel bound to ctx, adding a caller deadline or
// cancellation scope to whatever it dispatches or publishes.
func (k Kernel) WithContext(ctx context.Context) Kernel {
	k.bound()
	if ctx == nil {
		ctx = context.Background()
	}
	k.ctx, k.scope, k.bounded = ctx, ctx, false
	return k
}

// ReportError sends err to the centralized error handler and reports whether it
// requested engine termination.
func (k Kernel) ReportError(err error) bool {
	return k.bound().reportError(err)
}

// ReportErrorOnce sends errs to the centralized error handler the first time it
// is called under key, and drops them every time after. It reports whether the
// handler asked for engine termination, exactly as ReportError does.
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
func (k Kernel) ReportErrorOnce[T comparable](key T, errs ...error) bool {
	return k.bound().reportErrorOnce(key, errs)
}

// ForgetReportedError clears key, so the next ReportErrorOnce under it reports
// again. A path that failed, was fixed and reloaded must be able to speak.
//
// Forgetting is not optional for a condition that can be repaired: without the
// paired call, a sprite that failed to load once stays silent about failing
// again for the engine's life. A condition that cannot be repaired - a backend
// that never installed - simply never calls it.
func (k Kernel) ForgetReportedError[T comparable](key T) {
	k.bound().forgetReportedError(key)
}

// ForgetReportedErrors clears every key of type T that match accepts, for the
// family a single key cannot name: a model whose selector keys hang off its
// path, cleared together when the model unloads.
//
// match is never shown a key of another type, so a plugin clearing a family of
// its own keys cannot reach another plugin's. It scans the whole table, so it
// belongs on a cold path - an unload, not a frame.
func (k Kernel) ForgetReportedErrors[T comparable](match func(T) bool) {
	k.bound().forgetReportedErrors(func(key any) bool {
		typed, ok := key.(T)
		return ok && match(typed)
	})
}

// PublishEvent starts one event publication and returns its completion handle.
// Subscribers whose dependencies are satisfied run concurrently; separate
// publications are independent and may interleave. Publishing is
// fire-and-forget, so the publication is bounded by the kernel's scope rather
// than by the invocation that published it.
func (k Kernel) PublishEvent[TEvent any](event TEvent) *Publication {
	engine := k.bound()
	plan := engine.registry.publications[reflect.TypeFor[TEvent]()]
	publication := newPublication(k.scope)
	if plan == nil || len(plan.nodes) == 0 {
		publication.complete(nil)
		return publication
	}

	ctx := k.scope
	stop := func() bool { return false }
	cancel := func() {}
	if engine.ctx != nil && !k.bounded {
		ctx, cancel = context.WithCancel(k.scope)
		stop = context.AfterFunc(engine.ctx, cancel)
	}
	invocation := &eventContext[TEvent]{Context: ctx, engine: engine, scope: k.scope, event: event}
	go func() {
		defer func() {
			stop()
			cancel()
		}()
		engine.runPublication(plan, invocation, publication)
	}()
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
	engine := k.bound()
	cmd, ok := engine.registry.commands[reflect.TypeFor[TCommand]()]
	if !ok {
		engine.reportError(ErrExecutingUnknownCommand[TCommand]{})
		return
	}
	scope, bounded := k.scope, k.bounded
	go func() {
		ctx := scope
		if engine.ctx != nil && !bounded {
			combined, cancel := context.WithCancel(ctx)
			stop := context.AfterFunc(engine.ctx, cancel)
			defer func() {
				stop()
				cancel()
			}()
			ctx = combined
		}
		_, err := dispatch[TRequest, TResponse](
			engine, cmd, ctx, scope, cmd.resources.read, cmd.resources.write, request)
		if err != nil {
			engine.reportError(err)
		}
	}()
}

// Executioner is a Kernel that may also dispatch commands synchronously without
// declaring them. Only the engine mints one, for plugin lifecycle methods and
// host callbacks: they run outside any handler, so every command they dispatch
// acquires its own lock set. A handler receives a plain Kernel and therefore
// dispatches only through a declared Uses dispatcher or ExecuteCommandAsync.
type Executioner struct{ Kernel }

// WithContext derives an Executioner bound to ctx, adding a caller deadline or
// cancellation scope to whatever it dispatches or publishes.
func (e Executioner) WithContext(ctx context.Context) Executioner {
	return Executioner{e.Kernel.WithContext(ctx)}
}

// Describe returns the finalized architecture. An Executioner exists only once
// Run begins, so the description it returns is always the final one. The value
// is detached: it reads registry state that is immutable after finalization and
// so needs no lock, no tick and no scheduler.
func (e Executioner) Describe() ArchitectureDescription { return e.bound().Describe() }

// ExecuteCommand runs a command synchronously and returns its response or system
// error. Expected request rejection belongs in the response; callers propagate
// system errors to an event, lifecycle, or host boundary for centralized reporting.
func (e Executioner) ExecuteCommand[
	TCommand CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
](request TRequest) (TResponse, error) {
	engine := e.bound()
	cmd, ok := engine.registry.commands[reflect.TypeFor[TCommand]()]
	if !ok {
		var zero TResponse
		return zero, ErrExecutingUnknownCommand[TCommand]{}
	}

	ctx := e.ctx
	var stop func() bool
	var cancel context.CancelFunc
	if engine.ctx != nil && !e.bounded {
		ctx, cancel = context.WithCancel(ctx)
		stop = context.AfterFunc(engine.ctx, cancel)
	}
	response, err := dispatch[TRequest, TResponse](
		engine, cmd, ctx, e.scope, cmd.resources.read, cmd.resources.write, request)
	// Unwound explicitly rather than deferred: dispatch recovers plugin panics
	// itself, and a defer would cost every dispatch to serve a branch few take.
	if cancel != nil {
		stop()
		cancel()
	}
	return response, err
}
