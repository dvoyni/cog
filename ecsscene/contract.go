// Package ecsscene records drawable Entities into scene.
//
// It is a binding, and a binding is necessarily a third plugin: `ecs` imports
// only `kernel`, `scene` imports nothing of `ecs`, so neither can know about
// the other. There is no binding mechanism in the ECS and that is the decision
// — this package is an ordinary cog plugin that registers its own Components
// and subscribes one ordinary System. The consequence worth stating is that a
// project not using the ECS schedules no ECS Systems: it simply does not
// register this plugin.
//
// The whole of it is one recording System. It walks the Entities carrying a
// Transform and a Drawable, resolves each Drawable's ModelHash to a storage
// path through the Manifest this plugin declares, and appends one
// scene.OpQueue.Model call per drawable. Data flows one way and for scene that
// is not a choice: scene keeps no per-entity state, so there is no scene-side
// object for a drawable Entity to be a copy of, and the Components are the
// source of truth because there is no other candidate.
//
// A game whose drawables are shaped differently — its own transform Component,
// its own idea of what a draw says — writes its own recording System instead
// and does not register this plugin. That is not a fork of anything: the
// binding is an ordinary plugin, and cog ships this one because cog ships
// scene.
//
// README.md is the API, and its prohibitions are load-bearing: a backend
// adopted from outside cog will not have been written with them in mind.
package ecsscene

import (
	"github.com/dvoyni/cog/m"
	"github.com/dvoyni/cog/scene"
)

// ModelHash is the hash of the name a manifest entry registered for one glTF
// file. It is what a Component may hold where the path itself — a string, and
// therefore a pointer — may not.
//
// It is its own type rather than a shared one so that a clip name cannot be
// assigned where a model name belongs, which is the whole point of ecs.HashOf's
// type parameter. A game names one with ecs.HashOf[ecsscene.ModelHash]("crate")
// in a package-level var: hashing is pure, so it needs no engine, no lock and
// no table at initialisation.
type ModelHash uint64

// ClipHash is ModelHash's twin for an animation clip. It resolves to the clip
// name inside the file, which is how scene addresses a clip.
type ClipHash uint64

// Transform is where a drawable stands. It is scene.Transform without the one
// field that would break the binding: scene.Transform.Matrix is a *m.Mat4 that
// scene retains by value until the flush, and the flush is a different System
// running after this one's locks are gone — so a matrix pointing into a
// Component Store would be read unlocked. There is no Matrix here, so the
// prohibition is structural rather than remembered, and the pointer-free rule
// the ECS enforces at registration would have refused one anyway.
//
// Its zero value is the identity, exactly as scene.Transform's is: the zero
// Quat reads as no rotation and a zero Scale reads as 1.
//
// Non-uniform scale is therefore not expressible, which is scene's decision
// rather than the binding's: scene.Transform.Scale is scalar and non-uniform
// scale goes through the Matrix this Component cannot hold.
type Transform struct {
	Position m.Vec3
	Rotation m.Quat
	// Scale is uniform, and zero means 1.
	Scale float32
}

// Drawable is what an Entity draws and which cameras see it.
//
// A Drawable whose Model names nothing the manifest registered draws nothing
// and is not reported: a hash is a plain number and a Component's unset field
// is ecs.NoHash, so "no model yet" is the ordinary state of a drawable being
// assembled rather than a mistake. Layers is scene's own mask, whose zero reads
// as every layer, so the zero Drawable is a well-formed drawable of nothing.
type Drawable struct {
	Model  ModelHash
	Layers scene.LayerMask
}

// MaxPlays is how many clips one drawable may blend. It is scene's own cap:
// scene drops a fifth play by lowest weight and reports it, so exceeding it
// here would buy a report and no animation.
//
// A fixed-capacity array is one of the three answers to variable-length data in
// a Component, and it is the right one here because the bound is small and real
// — it is not a number this package chose.
const MaxPlays = 4

// Play is one clip playing on one drawable. It is scene.ClipPlay with the clip
// named by hash instead of by string, which is the whole difference between
// what a Component may hold and what a draw call takes.
type Play struct {
	// Clip is ecs.NoHash in an unused slot, which is what makes the slot count
	// a property of the data rather than a second field that can disagree with
	// it.
	Clip ClipHash
	// Time is the play head in seconds, already advanced by whoever owns the
	// clock: scene advances nothing. Loop decides what happens outside the
	// clip's duration, and Weight is this play's share of the blend, normalised
	// by scene across the draw's plays.
	Time   float32
	Weight float32
	Loop   bool
}

// Animation is the clips a drawable is blending this frame.
//
// It is optional: the recording System reaches it through an Accessor rather
// than through its Query, so an Entity without one draws its rest pose and
// costs one probe. Putting it in the Query instead would exclude every
// unanimated drawable from the walk, and a second System for those would
// serialise against this one anyway — both hold scene's one queue for write.
type Animation struct {
	Plays [MaxPlays]Play
}
