package internal

import "github.com/dvoyni/cog/kernel"

// Uses is the System parameter that dispatches a kernel Command, and it is the
// kernel's own declaration reached through the signature: prepare calls
// kernel.ResourceAccess.Uses, and composition folds the Command's lock closure
// into the System's set, exactly as it does for a hand-written handler.
//
//	func load(
//	    compile *ecs.Uses[gfx.CompileShaderCmd, gfx.CompileShaderRequest, gfx.CompileShaderResponse],
//	    …
//	) { resp := compile.Execute(k, gfx.CompileShaderRequest{…}) }
//
// So the signature shows everything the System holds: a Command that locks a
// resource makes the System hold it for its whole run, and serialise against a
// writer of it, the way naming it through Write would; a Command whose lock is
// empty adds nothing, and the System loses no parallelism however long the
// Command runs.
//
// It takes three type arguments, as ResourceAccess.Uses does, because Go infers
// none for a type: the request and response cannot be read off the Command
// type in a parameter's declaration.
//
// A dispatch acquires nothing. The fold already granted the System the
// Command's locks, so the kernel hands the nested dispatch an empty lock set
// and runs it on the System's own goroutine, with no coordinator round-trip.
// See kernel/engine-dispatch.go runTask.
//
// A Command no plugin registered fails composition with
// kernel.ErrUsingUnknownCommand, naming the System's identity type.
type Uses[TCommand kernel.CommandConstraint[TRequest, TResponse], TRequest any, TResponse any] struct {
	// dispatch is the kernel's dispatcher. It is bound to its Command when
	// composition resolves the declaration and never changes after, so there is
	// nothing to resolve an invocation.
	dispatch func(kernel.Kernel, TRequest) TResponse
}

// prepare declares the use and keeps the dispatcher. It runs once, at
// registration.
func (u *Uses[TCommand, TRequest, TResponse]) prepare(_ *Entities, access kernel.ResourceAccess) {
	u.dispatch = access.Uses[TCommand, TRequest, TResponse]()
}

// Execute dispatches the Command with request and returns its response. k is
// the kernel value the System names, which is what carries the engine the
// dispatch runs in.
//
// Execute is also the plain func a library taking a dispatcher wants: a
// System hands compile.Execute to one, and the library never sees the ECS.
func (u *Uses[TCommand, TRequest, TResponse]) Execute(k kernel.Kernel, request TRequest) TResponse {
	return u.dispatch(k, request)
}
