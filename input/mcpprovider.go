package input

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/mcp"
)

// input offers its capabilities itself rather than through a separate plugin,
// per the rule that every package hosts its own provider. The alternative — a
// broker dispatching input.ApplyCmd itself — means the broker importing input,
// which is the knower the whole extension point keeps out.
var _ mcp.Provider = (*Plugin)(nil)

// Key crosses the wire as the string it prints as, not as the integer its Go
// kind implies, and says which strings it accepts. That is what turns a
// mistyped key into a client-side rejection rather than a refusal from the
// engine, and it is the only thing in this feature that exists for an agent's
// sake.
var _ mcp.TextValued = (*Key)(nil)

// The two capabilities, rendered as the tools input_send and input_state. They
// are two rather than one because looking and pressing differ in exactly the
// way an annotation is allowed to state: a read is worth auto-approving and a
// press is not. Within input the tool-selection risk is zero rather than
// mitigated — one tool acts, one looks, and there is nothing else to pick
// between.
const (
	sendName  = "send"
	stateName = "state"
)

// sendDescription is prompt text, and it is reproduced in
// input/docs/specs/mcp.md so it is reviewed as prompt text rather than buried
// as a string literal.
const sendDescription = "Send input to the game as a list of steps applied in order. Steps with " +
	"no `delay` between them land in the same tick: `move`, `key_down`, `key_up` in one call is a " +
	"complete click. A `delay` between `key_down` and `key_up` is a held press, and a `move` " +
	"between them is a drag. Coordinates are window units, not image pixels — a capture returns " +
	"both its pixel size and the window size, so divide one by the other to turn a point you can " +
	"see into a point you can click. Keys are names: `w`, `escape`, `space`, `mouse_left`. `text` " +
	"types characters and does **not** press keys; a game that reads keys needs " +
	"`key_down`/`key_up`. Nothing is undone when you disconnect: a key you press stays down until " +
	"something releases it, and the response tells you what is currently held.\n\n" +
	"While the game is paused a `delay` separates nothing, because no tick runs — to hold a key " +
	"for exactly one tick, send `key_down`, step once, send `key_up`, step again."

// stateDescription is prompt text, reproduced in input/docs/specs/mcp.md for
// the same reason.
const stateDescription = "What the input seam holds right now: every key and mouse button that " +
	"is down, and where the pointer is, in window units. Changes nothing. Use it to check whether " +
	"the game is really seeing a key you are holding, or to find a key left down by an earlier call."

// Capabilities reports what input offers an agent: one tool that acts and one
// that looks. Together they are an agent's only reach into the game, by design
// — there is no arbitrary command dispatch.
//
// send is an mcp.Func rather than an mcp.Command because the wait between
// batches must happen outside every lock, and a command handler that slept
// would sleep under the State write lock. state is an mcp.Command with zero
// glue, and it is the read-only one: several clients auto-approve read-only
// tools, and "is W actually down?" is worth asking cheaply and often. send is
// not read-only, because changing the game is its entire purpose.
func (p *Plugin) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(sendName, sendDescription, send),
		mcp.Command[StateCmd, StateRequest, StateResponse](stateName, stateDescription, mcp.ReadOnly()),
	}
}

// send is the input_send body: Play, and nothing else. Everything an agent
// could get wrong about a sequence is refused by Play before a step runs, in
// words it reads and acts on, and neither capability binds to a frame — a
// capture binds to a tick that began after its own request, so press-then-look
// is correct without either side building a wait.
//
// It is a package function rather than a method to keep the capability-body
// rule visible at the call site: the plugin is one pointer away and the body
// still reaches input only by dispatch.
func send(k kernel.Executioner, request SynthesizeRequest) (StateResponse, error) {
	return Play(k, request.Actions)
}
