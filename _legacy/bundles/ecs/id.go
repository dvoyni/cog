package ecs

import "github.com/dvoyni/cog/bundles/ecs/internal"

// Name is the ecs plugin's name and configuration key. A plugin that registers
// Components or Systems declares a dependency on it, because that is what
// registers the authority first.
const Name = internal.Name

// DrainOnUpdate is the subscription type of the ECS's own drain System on
// app.UpdateEvent, which applies everything the deferring handles queued this
// tick. It runs in the Last phase, and it is subscribed unconditionally: a
// queue that is never drained is not a configuration, so there is no flag that
// turns it off.
//
// Last subscribers have no order among themselves, so one that cares orders
// itself against it by name — Before[ecs.DrainOnUpdate] to have its own queue
// drained this Update, After[ecs.DrainOnUpdate] to see this Update's drain.
// Unordered, which side it lands on is unspecified.
//
// It is the last drain of a tick, not the only one an app may have: a System
// calling WriteableEntities.Drain on any event is an ordinary System, and is
// how a change is made visible earlier. See
// bundles/ecs/docs/specs/deferred.md § The drain.
type DrainOnUpdate = internal.DrainOnUpdate
