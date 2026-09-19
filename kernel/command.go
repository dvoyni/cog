package kernel

import (
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
// to acquire, the typed request, and the slot for the typed result. It is both
// the task the scheduler runs and the invocation handed back to that task.
type commandContext[TRequest any, TResponse any] struct {
	engine      *Engine
	command     *command
	read, write map[reflect.Type]struct{}
	execute     Execute[TRequest, TResponse]
	request     TRequest
	result      TResponse
}

func (c *commandContext[TRequest, TResponse]) locks() (read, write map[reflect.Type]struct{}) {
	return c.read, c.write
}

func (c *commandContext[TRequest, TResponse]) run(any) (err error) {
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
	c.result = c.execute(Kernel{engine: c.engine}, c.request)
	return nil
}

// release clears the invocation before recycling it, so a finished command does
// not pin its request or response.
func (c *commandContext[TRequest, TResponse]) release() {
	var zeroRequest TRequest
	var zeroResponse TResponse
	c.engine, c.command = nil, nil
	c.read, c.write, c.execute = nil, nil, nil
	c.request, c.result = zeroRequest, zeroResponse
}

// dispatch runs one command invocation to completion on the calling goroutine,
// with read and write as the lock set the scheduler must grant it.
//
// It answers with the response alone. What can go wrong here is the kernel's
// own business — a body that panicked, a scheduler that has stopped — and it is
// reported rather than handed back, because no caller has anything to do with
// it that reporting has not already done. A dispatch that failed answers with
// the zero response.
func dispatch[TRequest any, TResponse any](
	engine *Engine, cmd *command,
	read, write map[reflect.Type]struct{}, request TRequest,
) TResponse {
	invocation := cmd.invocations.Get().(*commandContext[TRequest, TResponse])
	invocation.engine = engine
	invocation.command = cmd
	invocation.read, invocation.write = read, write
	invocation.execute = cmd.execute.(Execute[TRequest, TResponse])
	invocation.request = request

	err := engine.runTask(invocation, invocation)
	response := invocation.result
	invocation.release()
	cmd.invocations.Put(invocation)
	if err != nil {
		engine.reportError(shutdownAside(err))
		var zero TResponse
		return zero
	}
	return response
}
