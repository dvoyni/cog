// Package ecs describes things in the world as Entities carrying Components.
//
// A Component is a plain value an Entity either has or has not, addressed by
// its Go type and held in a Store of its own. A System is a plain Go func whose
// parameter types say what it touches: a Query over the Components it iterates,
// whose field pointer-ness is its access mode, narrowed by Without and With
// filters that yield nothing and still declare a read; a Spawn over the Component
// set it creates; the WriteableEntities that can retire one; the Get, Set and
// Remove accessors that reach one Component of an Entity it did not iterate to;
// the Hooks that say what happened to one Component since its last run;
// the Read and Write handles that name another plugin's resource; the In that
// carries a value projected out of the event; and, for a command, the Resp it
// answers through. ToHandler turns one into the factory an ordinary cog
// subscription takes and ToExecute into the factory a command takes, so the ECS
// contributes no scheduler, no ordering and no registration API of its own.
//
// A Component names an engine-side thing — a model, a clip, a node, a pass tag
// — however the plugin that resolves those names wants it named. A Component may
// hold a string, so the ECS has no opinion and supplies no naming scheme: how a
// name is spelled, and what resolves it, belong to the two plugins that share
// it. There is deliberately nothing central, and no process-wide interner.
//
// No structural change is a Command. A structural change is a direct call on a
// handle the System already holds, and the exclusion it needs was arranged
// before the frame started: Spawn and WriteableEntities declare
// write{*Entities}, which is a total barrier because Entities holds a reference
// to every Store. The one Command here is ShrinkCmd, which changes no
// membership: nothing gives memory back on its own, and the app executes it
// after a spike.
//
// There is no binding mechanism, and that is the decision. A plugin that is not
// the ECS attaches to the world by being an ordinary plugin: it registers
// Components if it has any, subscribes Systems like anything else, and reaches
// its own frame-local resource from inside them through Read and Write. No
// binding type, no adapter, no registration call of the ECS's own. See
// bundles/ecs/docs/specs/ecs.md, which this package is judged against.
//
// The two decisions the rest of the design rests on are made here. Storage is
// sparse sets rather than archetype tables, so one Component type is exactly one
// object and therefore exactly one lock unit. And an Entity's generation lives
// in the sparse slot, so the compare that finds a row is the compare that
// rejects a stale handle: liveness is not an extra structure, it is the probe.
//
// ecs is a Bundle. Its plugin, built by ecsplugin.New and configured by Config,
// publishes the authority, *Entities, registers ShrinkCmd and six unexported
// Commands that read and write the world by Component name, registers the
// Store of m.Transform, where an Entity stands,
// and subscribes the one System it owns: the general drainer, DrainOnUpdate,
// which applies what the deferring handles queued, Last on app.UpdateEvent and
// unconditionally. It registers nothing else. It requires no Adapter, and contributes
// one to mcp's collected Port, as McpProvider, offering those Commands to an
// Agent as the tools ecs_world, ecs_entity, ecs_query, ecs_spawn, ecs_despawn
// and ecs_update.
package ecs
