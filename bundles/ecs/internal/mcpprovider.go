package internal

import (
	"github.com/dvoyni/cog/bundles/mcp"
	"github.com/dvoyni/cog/kernel"
)

// provider is what ecs contributes to the mcp Port, from its own Register
// rather than through a separate plugin, per the rule that every package hosts
// its own provider. It offers the three read Commands of read.go and the three
// write Commands beside them to an Agent. The writes are the one place a
// Component is made or changed without the ownership a Spawn[S] declares, and
// that exception is ecs's own: the authority over every Store, acting as a
// debugger that an app has only by composing the broker. It holds nothing,
// adds no subscription and no resource, and so adds no work to any frame; an
// app that composes no broker binds it to nothing.
type provider struct{}

// The six capabilities, rendered as the tools ecs_world, ecs_entity,
// ecs_query, ecs_spawn, ecs_despawn and ecs_update. The first is the census of
// read.go: the wire keeps the name the tool was specified under, and only Go
// identifiers avoid the word.
const (
	censusName  = "world"
	entityName  = "entity"
	queryName   = "query"
	spawnName   = "spawn"
	despawnName = "despawn"
	updateName  = "update"
)

// callCost is the stall every read imposes, said in every read's description
// because an Agent that loops a read in a busy game slows exactly the game it
// watches.
const callCost = "It changes nothing, needs no tick and works while the game is paused, so do not step the game " +
	"just to look. Every call waits for the ECS Systems running now and holds up the ones queued behind it until " +
	"it returns."

// writeCost is what every write does to the run: where it lands, what it
// stalls, and why the run is no longer the game's alone.
const writeCost = "This changes the game. It lands between Systems, never inside one's run, and needs no tick: it " +
	"works while the game is paused, and the next System to run sees it. Every call waits for the ECS Systems " +
	"running now and holds up the ones queued behind it until it returns. A run you have written to is not the " +
	"run the game alone would make: `ecs_world` counts your writes as `agentWrites`, so keep the calls you made " +
	"if the run must be made again."

// writeRules is what every write shares: all or nothing, and recorded in the
// Hook logs as the same act from a System.
const writeRules = "All or nothing: an unknown name, a value that does not fit its Component or a field it does " +
	"not have refuses the whole call, naming every entry that failed, and nothing is applied. Every Hook reader " +
	"in the game sees the write as the same act from a System would be (a spawn, a despawn, an addition, a " +
	"change, a removal), so an index or a cache the game builds on Hooks stays right."

// contention tells the six tools' own pairs in mcpserver_architecture's
// contention report from the game's. The Command names are kernel.TypeName of
// the six Commands; a test computes them, so a rename fails there rather than
// leaving the prompt naming a type that is gone.
const contention = "In `mcpserver_architecture`'s contention report, `ecs.censusCmd`, `ecs.entityCmd`, " +
	"`ecs.queryCmd`, `ecs.spawnCmd`, `ecs.despawnCmd` and `ecs.updateCmd` appear as writers of `*ecs.Entities` " +
	"conflicting with every ECS System. Those pairs are these six tools, not the game: discount exactly them. " +
	"Every other writer of `*ecs.Entities`, such as `ecs.ShrinkCmd` or a System that spawns or despawns, is the " +
	"game's."

// censusDescription is prompt text, and it is reproduced in
// bundles/ecs/docs/specs/mcp.md so it is reviewed as prompt text rather than
// buried as a string literal.
const censusDescription = "The game's ECS at a glance: every registered Component, named as " +
	"`mcpserver_architecture` names types, with how many Entities carry it; how many Entities are alive; how " +
	"many indices wait on the free list for reuse; and the index space, every index ever allocated. A free list " +
	"or index space that keeps growing is churn or a leak. `agentWrites` counts the `ecs_spawn`, `ecs_despawn` " +
	"and `ecs_update` calls that changed the world since the game started: when it is not 0, this run is not " +
	"the one the game alone would have made. Call this first: the Component names it lists are how " +
	"`ecs_entity`, `ecs_query` and the writes name Components. " + callCost + "\n\n" + contention

// entityDescription is prompt text, reproduced in
// bundles/ecs/docs/specs/mcp.md for the same reason.
const entityDescription = "One Entity and every Component it carries, sorted by name, each with its value. Give " +
	"the Entity in whatever form a log or an earlier answer printed it: `7v2`, `Entity(7v2)` or its decimal " +
	"handle. Give `components` to see only those, in the shape `ecs_update` takes back; a named Component the " +
	"Entity does not carry is left out. A refused Entity is not alive: it was despawned, and when its index was " +
	"reused the reason names the Entity that now holds it, so stop reasoning about the one you asked for.\n\n" +
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

// spawnDescription is prompt text, reproduced in
// bundles/ecs/docs/specs/mcp.md for the same reason.
const spawnDescription = "Spawns one Entity carrying the named Components, and answers it. Give `components` as " +
	"Component name, as `ecs_world` lists it, to value, in the shape `ecs_entity` shows one: a field you leave " +
	"out is zero, and `{}` is a Tag or an all-zero value. Give none for a bare Entity. An `m.List` is an array. " +
	"An `assets.Blob` cannot be written, because its bytes never cross: a spawned one is empty whatever `len` " +
	"you give. A field that is unexported cannot be named, and is zero.\n\n" +
	writeRules + "\n\n" + writeCost + "\n\n" + contention

// despawnDescription is prompt text, reproduced in
// bundles/ecs/docs/specs/mcp.md for the same reason.
const despawnDescription = "Despawns one Entity, given in any form `ecs_entity` takes, with every Component it " +
	"carries. `wasAlive` is false when it had already gone, and then nothing happened: that is not an error. " +
	"Every Hook reader in the game sees a despawn, with each Component's last value, as it would from a System." +
	"\n\n" + writeCost + "\n\n" + contention

// updateDescription is prompt text, reproduced in
// bundles/ecs/docs/specs/mcp.md for the same reason.
const updateDescription = "Changes one Entity's Components, given in any form `ecs_entity` takes. `set` maps " +
	"Component name to value: a Component the Entity carries takes the fields you give and keeps every other, " +
	"so `{\"HP\": 0}` zeroes one field, and one it lacks is added, with the fields you leave out zero. `remove` " +
	"lists Components to take away. Naming one Component in both `set` and `remove` is refused. The answer " +
	"gives each named Component's outcome: `added`; `changed`; `unchanged`, when the value you gave is the value " +
	"it had, so nothing was written, which a value read with `ecs_entity` and sent back unedited always is; " +
	"`removed`; or `absent`, when it was not carried to remove, which is not an error.\n\n" +
	"Values take the shape `ecs_entity` shows. An `m.List` is an array and replaces the whole list. An " +
	"`assets.Blob` cannot be written, because its bytes never cross: it keeps its bytes whatever `len` you give. " +
	"A field that is unexported cannot be named, and keeps its value.\n\n" +
	writeRules + "\n\n" + writeCost + "\n\n" + contention

// Capabilities reports what ecs offers an Agent: three looks and three acts.
// Each is an mcp.Func rather than an mcp.Command for exactly one reason: a
// Command answers a refusal as a successful result, and a Func turns it into
// mcp.Unavailable, the ordinary tool error an Agent reads and acts on. The
// looks are read-only, which lets a client auto-approve them; the acts are not.
func (provider) Capabilities() []mcp.Capability {
	return []mcp.Capability{
		mcp.Func(censusName, censusDescription, readCensus, mcp.ReadOnly()),
		mcp.Func(entityName, entityDescription, readEntityTool, mcp.ReadOnly()),
		mcp.Func(queryName, queryDescription, readQueryTool, mcp.ReadOnly()),
		mcp.Func(spawnName, spawnDescription, writeSpawn),
		mcp.Func(despawnName, despawnDescription, writeDespawn),
		mcp.Func(updateName, updateDescription, writeUpdate),
	}
}

// readCensus, readEntity, readQuery and the three writes are the bodies: one
// dispatch, and a non-empty refusal turned into mcp.Unavailable. They are
// package functions rather than methods to keep the capability-body rule
// visible at the call site: they touch no provider state and no resource, and
// reach ecs only by dispatch.
//
// There is no branch for a zero response. A scheduler that has stopped answers
// with one, and the kernel has already reported the dispatch it could not
// perform; that answer is accepted as it is.
func readCensus(k kernel.Executioner, request CensusRequest) (CensusResponse, error) {
	return answer(k.ExecuteCommand[censusCmd](request), func(r CensusResponse) string { return r.Refusal })
}

func readEntityTool(k kernel.Executioner, request EntityRequest) (EntityResponse, error) {
	return answer(k.ExecuteCommand[entityCmd](request), func(r EntityResponse) string { return r.Refusal })
}

func readQueryTool(k kernel.Executioner, request QueryRequest) (QueryResponse, error) {
	return answer(k.ExecuteCommand[queryCmd](request), func(r QueryResponse) string { return r.Refusal })
}

func writeSpawn(k kernel.Executioner, request SpawnRequest) (SpawnResponse, error) {
	return answer(k.ExecuteCommand[spawnCmd](request), func(r SpawnResponse) string { return r.Refusal })
}

func writeDespawn(k kernel.Executioner, request DespawnRequest) (DespawnResponse, error) {
	return answer(k.ExecuteCommand[despawnCmd](request), func(r DespawnResponse) string { return r.Refusal })
}

func writeUpdate(k kernel.Executioner, request UpdateRequest) (UpdateResponse, error) {
	return answer(k.ExecuteCommand[updateCmd](request), func(r UpdateResponse) string { return r.Refusal })
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
