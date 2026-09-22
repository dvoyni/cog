package internal

import (
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// provider is what ecs contributes to the mcp Port, from its own Register
// rather than through a separate plugin, per the rule that every package hosts
// its own provider. It offers the three read Commands of read.go to an Agent
// and nothing else: there is no mutation, because a write an Agent makes
// through Entities is the capability most able to corrupt a run nobody can
// reproduce. It holds nothing, adds no subscription and no resource, and so
// adds no work to any frame; an app that composes no broker binds it to
// nothing.
type provider struct{}

// The three capabilities, rendered as the tools ecs_world, ecs_entity and
// ecs_query. The first is the census of read.go: the wire keeps the name the
// tool was specified under, and only Go identifiers avoid the word.
const (
	censusName = "world"
	entityName = "entity"
	queryName  = "query"
)

// callCost is the stall every call imposes, said in every description because
// an Agent that loops a read in a busy game slows exactly the game it watches.
const callCost = "It changes nothing, needs no tick and works while the game is paused, so do not step the game " +
	"just to look. Every call waits for the ECS Systems running now and holds up the ones queued behind it until " +
	"it returns."

// contention tells the three tools' own pairs in mcpserver_architecture's
// contention report from the game's. The Command names are kernel.TypeName of
// censusCmd, entityCmd and queryCmd; a test computes them, so a rename fails
// here rather than leaving the prompt naming a type that is gone.
const contention = "In `mcpserver_architecture`'s contention report, `ecs.censusCmd`, `ecs.entityCmd` and " +
	"`ecs.queryCmd` appear as writers of `*ecs.Entities` conflicting with every ECS System. Those pairs are these " +
	"three tools, not the game: discount exactly them. Every other writer of `*ecs.Entities`, such as " +
	"`ecs.ShrinkCmd` or a System that spawns or despawns, is the game's."

// censusDescription is prompt text, and it is reproduced in
// bundles/ecs/docs/specs/mcp.md so it is reviewed as prompt text rather than
// buried as a string literal.
const censusDescription = "The game's ECS at a glance: every registered Component, named as " +
	"`mcpserver_architecture` names types, with how many Entities carry it; how many Entities are alive; how " +
	"many indices wait on the free list for reuse; and the index space, every index ever allocated. A free list " +
	"or index space that keeps growing is churn or a leak. Call this first: the Component names it lists are " +
	"how `ecs_entity` and `ecs_query` name Components. " + callCost + "\n\n" + contention

// entityDescription is prompt text, reproduced in
// bundles/ecs/docs/specs/mcp.md for the same reason.
const entityDescription = "One Entity and every Component it carries, sorted by name, each with its value. Give " +
	"the Entity in whatever form a log or an earlier answer printed it: `7v2`, `Entity(7v2)` or its decimal " +
	"handle. A refused Entity is not alive: it was despawned, and when its index was reused the reason names " +
	"the Entity that now holds it, so stop reasoning about the one you asked for.\n\n" +
	"Values are the Component's exported fields as JSON. An `m.List` is an array, an `assets.Blob` is " +
	"`{\"len\":N}` (its length, never its bytes), an Entity Reference is its decimal handle, which you can pass " +
	"back to `ecs_entity`, and an `m.Maybe` is its value, or `null` when absent. Fields that are unexported are " +
	"not shown, so a Component whose fields are all unexported shows as `{}`: a missing field is not a zero " +
	"one. A Component with an `error` instead of a value could not be encoded, and the error says why.\n\n" +
	callCost

// queryDescription is prompt text, reproduced in
// bundles/ecs/docs/specs/mcp.md for the same reason.
const queryDescription = "The Entities that carry every one of the named Components, in ascending index order, " +
	"each with the values of only those Components: call `ecs_entity` for the rest of one. Name Components as " +
	"`ecs_world` lists them; values render as `ecs_entity` describes. At most `limit` Entities come back: 50 " +
	"when you give none, and never more than 500. `total` is how many matched and `truncated` is true when you " +
	"were shown fewer, so never read a truncated answer as the whole world.\n\n" +
	"It changes nothing, needs no tick and works while the game is paused. Every call waits for the ECS Systems " +
	"running now and holds up the ones queued behind it until it returns, for longer the more it walks: keep " +
	"`limit` small in a busy game, and do not loop it every tick.\n\n" + contention

// Capabilities reports what ecs offers an Agent: three looks and no act. Each
// is an mcp.Func rather than an mcp.Command for exactly one reason: a Command
// answers a refusal as a successful result, and a Func turns it into
// mcp.Unavailable, the ordinary tool error an Agent reads and acts on. All
// three are read-only, which lets a client auto-approve them.
func (provider) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(censusName, censusDescription, readCensus, mcp.ReadOnly()),
		mcp.Func(entityName, entityDescription, readEntity, mcp.ReadOnly()),
		mcp.Func(queryName, queryDescription, readQuery, mcp.ReadOnly()),
	}
}

// readCensus, readEntity and readQuery are the bodies: one dispatch, and a
// non-empty refusal turned into mcp.Unavailable. They are package functions
// rather than methods to keep the capability-body rule visible at the call
// site: they touch no provider state and no resource, and reach ecs only by
// dispatch.
//
// There is no branch for a zero response. A scheduler that has stopped answers
// with one, and the kernel has already reported the dispatch it could not
// perform; that answer is accepted as it is.
func readCensus(k kernel.Executioner, request types.CensusRequest) (types.CensusResponse, error) {
	return answer(k.ExecuteCommand[censusCmd](request), func(r types.CensusResponse) string { return r.Refusal })
}

func readEntity(k kernel.Executioner, request types.EntityRequest) (types.EntityResponse, error) {
	return answer(k.ExecuteCommand[entityCmd](request), func(r types.EntityResponse) string { return r.Refusal })
}

func readQuery(k kernel.Executioner, request types.QueryRequest) (types.QueryResponse, error) {
	return answer(k.ExecuteCommand[queryCmd](request), func(r types.QueryResponse) string { return r.Refusal })
}

// answer is a response, or mcp.Unavailable carrying its refusal when it has
// one.
func answer[TResponse any](response TResponse, refusal func(TResponse) string) (TResponse, error) {
	if reason := refusal(response); reason != "" {
		var zero TResponse
		return zero, mcp.Unavailable{Reason: reason}
	}
	return response, nil
}
