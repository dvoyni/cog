package internal

import (
	"github.com/dvoyni/cog/bundles/ecs/internal/types"
	"github.com/dvoyni/cog/kernel"
)

// The three read Commands, which read the world by Component name for a caller
// outside Go that knows a Component only as the string kernel.TypeName renders
// for it. They are unexported and declared here rather than in ecs's root,
// because nothing outside ecs dispatches them: the mcp provider that will
// offer them to an Agent is ecs's own.
//
// Each holds write{*ecs.Entities} and nothing else. That excludes every ECS
// System, because every handler touching a Store holds read{*ecs.Entities}: a
// call waits for every running System to release the authority, and under
// Conflict-aware FIFO every System queued behind it waits until it returns,
// which the query's limit bounds. No frame's lock set widens, because these
// are Commands rather than Systems, and a frame nobody reads from pays nothing;
// Describe().Contention lists them as writers of *ecs.Entities conflicting with
// every System.
type (
	// censusCmd reports every registered Component name with its Store's
	// population, the live Entity count, the free-list depth and the index
	// space.
	censusCmd kernel.Command[types.CensusRequest, types.CensusResponse]
	// entityCmd reports one Entity, named as "7v2", "Entity(7v2)" or its
	// decimal handle, with every Component it carries and its value.
	entityCmd kernel.Command[types.EntityRequest, types.EntityResponse]
	// queryCmd reports the Entities carrying every named Component, with those
	// Components' values, up to a limit.
	queryCmd kernel.Command[types.QueryRequest, types.QueryResponse]
)

// The three write Commands, which change the world by Component name for an
// Agent setting up a situation in a running game: bundles/ecs/docs/specs/mcp.md
// § Writing. They hold what the reads hold, write{*ecs.Entities} and nothing
// else, so a write lands between Systems and never inside one's run, and they
// declare no Store: the ownership a Spawn[S] declares is the one thing they
// skip, as ecs's own debugging authority. Every act is recorded in the Hook
// logs as the same act from a System would be.
type (
	// spawnCmd spawns one Entity carrying the named Components.
	spawnCmd kernel.Command[types.SpawnRequest, types.SpawnResponse]
	// despawnCmd despawns one Entity, and answers whether it was alive.
	despawnCmd kernel.Command[types.DespawnRequest, types.DespawnResponse]
	// updateCmd sets and removes Components of one Entity, all or none.
	updateCmd kernel.Command[types.UpdateRequest, types.UpdateResponse]
)
