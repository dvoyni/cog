package kernel

import (
	"context"
	"reflect"
	"runtime/debug"
	"sync"
)

// command is a registered command: the body its factory produced, and the lock
// set that body's Lock declared. Both are fixed at registration.
//
// execute holds the concrete Execute[TRequest, TResponse] the factory returned.
// It sits behind an interface because the registry is keyed by reflect.Type and
// cannot be generic, but dispatch asserts it back to its exact type once per
// invocation, so request and response values are never boxed.
type command struct {
	id        reflect.Type
	owner     PluginName
	boundary  string
	resources *ResourceAccess
	execute   any
	// invocations recycles commandContext values. Its New is installed at
	// registration, where the request and response types are still known.
	invocations sync.Pool
}

// commandContext carries one command invocation through the scheduler: the locks
// to acquire, the typed request, and the slot for the typed result. It doubles as
// the invocation's context.Context, so the scheduler needs no second value.
type commandContext[TRequest any, TResponse any] struct {
	context.Context
	engine      *Engine
	command     *command
	scope       context.Context
	read, write map[reflect.Type]struct{}
	execute     Execute[TRequest, TResponse]
	request     TRequest
	result      TResponse
}

func (c *commandContext[TRequest, TResponse]) locks(context.Context) (read, write map[reflect.Type]struct{}) {
	return c.read, c.write
}

func (c *commandContext[TRequest, TResponse]) run(context.Context) (err error) {
	cmd := c.command
	// Recovery is inlined rather than routed through callPluginBoundary so the
	// dispatch path allocates neither a closure nor a boundary string.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = ErrPluginPanic{
				Plugin: cmd.owner, Boundary: cmd.boundary,
				Recovered: recovered, Stack: debug.Stack(),
			}
		}
	}()
	handler := Kernel{
		engine:  c.engine,
		ctx:     c,
		scope:   c.scope,
		bounded: true,
	}
	c.result, err = c.execute(handler, c.request)
	return err
}

// release clears the invocation before recycling it, so a finished command does
// not pin its request, response, or context.
func (c *commandContext[TRequest, TResponse]) release() {
	var zeroRequest TRequest
	var zeroResponse TResponse
	c.Context, c.engine, c.command, c.scope = nil, nil, nil, nil
	c.read, c.write, c.execute = nil, nil, nil
	c.request, c.result = zeroRequest, zeroResponse
}

// usage is what a Uses declaration returns to its dispatcher closure. command is
// filled at composition, once every plugin has registered, so a Uses declaration
// does not depend on registration order.
type usage struct {
	command *command
}

// Uses declares that this handler dispatches TCommand and returns the dispatcher
// to call it with. Composition folds TCommand's lock closure into this handler's
// own set, so the caller never names the callee's resources, and the dispatch
// then reuses the locks the handler already holds.
func (r ResourceAccess) Uses[
	TCommand CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
]() func(Kernel, TRequest) (TResponse, error) {
	id := reflect.TypeFor[TCommand]()
	target := r.uses[id]
	if target == nil {
		target = &usage{}
		r.uses[id] = target
	}
	return func(k Kernel, request TRequest) (TResponse, error) {
		engine := k.bound()
		ctx := k.ctx
		var stop func() bool
		var cancel context.CancelFunc
		if engine.ctx != nil && !k.bounded {
			ctx, cancel = context.WithCancel(ctx)
			stop = context.AfterFunc(engine.ctx, cancel)
		}
		response, err := dispatch[TRequest, TResponse](
			engine, target.command, ctx, k.scope, noLocks, noLocks, request)
		// Unwound explicitly rather than deferred: dispatch recovers plugin panics
		// itself, and a defer would cost every dispatch to serve a branch few take.
		if cancel != nil {
			stop()
			cancel()
		}
		return response, err
	}
}

// dispatch runs one command invocation to completion on the calling goroutine,
// with read and write as the lock set the scheduler must grant it.
func dispatch[TRequest any, TResponse any](
	engine *Engine, cmd *command, ctx, scope context.Context,
	read, write map[reflect.Type]struct{}, request TRequest,
) (TResponse, error) {
	invocation := cmd.invocations.Get().(*commandContext[TRequest, TResponse])
	invocation.Context = ctx
	invocation.engine = engine
	invocation.command = cmd
	invocation.scope = scope
	invocation.read, invocation.write = read, write
	invocation.execute = cmd.execute.(Execute[TRequest, TResponse])
	invocation.request = request

	err := engine.runTask(invocation, invocation)
	response := invocation.result
	invocation.release()
	cmd.invocations.Put(invocation)
	if err != nil {
		var zero TResponse
		return zero, err
	}
	return response, nil
}
