package mcp

import (
	"github.com/dvoyni/cog/bundles/mcp/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// Command is the whole capability: dispatch this command with the agent's
// request. It carries zero glue — the provider already has a typed command, and
// the capability is simply the statement that an agent may dispatch it — and it
// satisfies the capability-body rule by construction.
func Command[
	TCommand kernel.CommandConstraint[TRequest, TResponse], TRequest any, TResponse any,
](name, description string, opts ...Option) Capability {
	return types.Command[TCommand, TRequest, TResponse](name, description, opts...)
}

// Func is the escape for a capability that is more than one dispatch: a body
// that must arm something and then wait for a frame cannot be a single command,
// because blocking inside a command handler holds the lock set against the very
// work it waits for.
//
// The body still obeys the capability-body rule: it may dispatch and it may
// wait, and it may not touch provider state.
func Func[TRequest any, TResponse any](
	name, description string,
	invoke func(kernel.Executioner, TRequest) (TResponse, error),
	opts ...Option,
) Capability {
	return types.Func[TRequest, TResponse](name, description, invoke, opts...)
}

// ReadOnly states that this capability does not change the game. That is a fact
// about a cog command rather than protocol vocabulary, so a provider may state
// it and the broker translates it into the protocol's read-only hint.
//
// The question every client actually uses the hint for is "safe to
// auto-approve?", and cog answers it in those terms: a capability that writes
// only the file the agent named is still read-only.
func ReadOnly() Option { return types.ReadOnly() }
