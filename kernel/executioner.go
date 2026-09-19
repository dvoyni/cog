package kernel

import "reflect"

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
