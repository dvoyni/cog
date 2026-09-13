// Package ecsscene records Entities into scene.
//
// It is a binding, and a binding is necessarily a third plugin: ecs imports
// nothing of scene and scene imports nothing of ecs, so what attaches them is
// an ordinary cog plugin that imports both. A project not using the ECS does
// not register it and schedules no ECS Systems.
//
// It is thin on purpose. Its Components hold scene's own types — a
// scene.Transform, a scene.ModelRef, a scene.MeshRef, scene.ClipPlays,
// gfx.ParameterDescrs, scene.Passes — and its one System copies each matching
// Entity into scene's op queue once a tick. There is no manifest, no hash and
// no name table: a Component holds the glTF path itself, and scene resolves it
// exactly as it resolves a path any other recorder names.
//
// Data flows one way. Scene keeps no per-entity state, so there is no
// scene-side object for an Entity to be a copy of, and the Components are the
// source of truth because there is no other candidate.
//
// ecsscene is a Bundle. This package is its contract root and declares no
// plugin: the Components a game spawns (Transform, Model, Mesh, Animation,
// Params, Material, Light, Camera), MaterialTag and MaxPlays, Name, and the
// ordering identity RecordOnUpdate. The plugin - the Component registrations,
// the recording scratch and the one System - is in ecssceneimpl, which only
// composition roots and tests import. The Components are still registered by
// the plugin that defines their Go type, because that plugin ships in this same
// Bundle under this package's Name. The root and the plugin share nothing a
// consumer must not see, so there is no internal package.
//
// A game whose drawables are shaped differently writes its own recording
// System and does not register this plugin. README.md is the API, and its
// prohibitions are what a second binding has to keep true.
//
// The Components are in components.go.
package ecsscene

import (
	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/scene"
	"github.com/dvoyni/cog/extensions/gfx"
	"github.com/dvoyni/cog/extensions/gfx/gpu"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the binding plugin's kernel name, the owner of every Component Store
// it registers, and the name a plugin whose Systems read those Components
// declares a dependency on.
const Name kernel.PluginName = "ecsscene"

// RecordOnUpdate is the subscription type of the binding's recording System on
// app.UpdateEvent: it copies every matching Entity into scene's op queue. A
// game System that moves Transforms orders itself Before it.
//
// It declares no ordering of its own: scene.FlushOnUpdate is subscribed Last,
// so anything that does not ask to be last already runs before it.
type RecordOnUpdate kernel.Subscription[app.UpdateEvent]

// MaxPlays is how many clips one Animation blends. It is scene's own cap:
// scene drops a fifth play by lowest weight and reports it, so a larger array
// here would buy a report and no animation.
const MaxPlays = 4

// MaterialTag is one pass tag of a Material Component: scene.MaterialTag with
// its gfx.MaterialDescr spelled out as the three things it is made of.
//
// It cannot hold a gfx.MaterialDescr, because a descriptor keeps its params as
// a bare slice, which a Component may not hold. The recording System rebuilds
// the descriptor every draw from these fields, in scratch, and scene copies it
// into its frame arenas at record.
type MaterialTag struct {
	// Tag is the pass this entry serves; zero reads as scene.TagForward.
	Tag    scene.PassTag
	Shader gfx.ShaderDescr
	State  gpu.MaterialState
	Params ecs.List[gfx.ParameterDescr]
}
