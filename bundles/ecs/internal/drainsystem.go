package internal

import "github.com/dvoyni/cog/bundles/ecs"

// drainSystem is the general drainer: it applies everything the deferring
// handles queued in this Engine, whatever System and whatever event queued it.
// It is the ECS's own System, and the only one it has.
//
// Its signature is its whole lock set — write{*ecs.Entities}, a total barrier —
// and its body is one call, because a drain is not a point the engine picks out
// of the frame: it is the same call an app's own drain System makes, scheduled
// where the ECS can promise it happens at all.
//
// It is subscribed as ecs.DrainOnUpdate, Last, unconditionally. See
// bundles/ecs/docs/specs/deferred.md § The drain.
func drainSystem(entities *ecs.WriteableEntities) { entities.Drain() }
