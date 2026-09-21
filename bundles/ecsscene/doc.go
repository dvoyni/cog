// Package ecsscene records Entities into scene.
//
// It is a binding, and a binding is necessarily a third plugin: ecs imports
// nothing of scene and scene imports nothing of ecs, so what attaches them is
// an ordinary cog plugin that imports both. A project not using the ECS does
// not register it and schedules no ECS Systems.
//
// It is thin on purpose. Its Components hold scene's own types — a
// scene.ModelRef, a scene.MeshRef, scene.ClipPlays, gfx.ParameterDescrs,
// scene.Passes — and its one System copies each matching Entity into scene's
// op queue once a tick, at the m.Transform the Entity carries. There is no manifest, no hash and
// no name table: a Component holds the glTF path itself, and scene resolves it
// exactly as it resolves a path any other recorder names.
//
// Data flows one way. Scene keeps no per-entity state, so there is no
// scene-side object for an Entity to be a copy of, and the Components are the
// source of truth because there is no other candidate.
//
// ecsscene is a Bundle. Its plugin, built by ecssceneplugin.New, requires no
// Adapter and contributes none. This package declares what it offers: the
// Components a game spawns (Model, Mesh, Animation, Params, Material, Light,
// Camera), MaterialTag and MaxPlays, Name, and the ordering
// identity RecordOnUpdate. The Component registrations, the recording scratch
// and the one System are in ecsscene's internal/. The Components are still
// registered by the plugin that defines their Go type, because that plugin
// ships in this same Bundle under this package's Name. The Components are plain
// data with no methods, so there is no internal/types.
//
// Where an Entity stands is not one of them. It is an m.Transform, whose one
// Store the ecs plugin registers, so that ecsaudio and a game's own Systems
// read the same placement this binding draws from rather than a copy of it.
// Every recorded Entity needs one, and an Entity without one is not recorded
// whatever else it carries.
//
// A game whose drawables are shaped differently writes its own recording
// System and does not register this plugin. docs/README.md is the API, and
// its prohibitions are what a second binding has to keep true.
//
// The Components are in types.go.
package ecsscene
