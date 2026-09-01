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
