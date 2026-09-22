// Package ecsscene records Entities into scene.
//
// It is a binding, and a binding is necessarily a third plugin: ecs imports
// nothing of scene and scene imports nothing of ecs, so what attaches them is
// an ordinary cog plugin that imports both. A project not using the ECS does
// not register it and schedules no ECS Systems.
//
// Its Components wrap model's values — a model.ModelRef, a model.MeshRef,
// model.ClipPlays, a model.LightDescr — and gfx.ParameterDescrs, beside
// ecsscene's own copy of the camera, layer and pass vocabulary (CameraID,
// ProjectionKind, PassTag, Pass, LayerMask), which keeps scene's names, shapes
// and zero values without naming scene. There is no manifest, no hash and no
// name table: a Component holds the glTF path itself. Until the recording
// System draws into gfx itself, it copies each matching Entity into scene's op
// queue once a tick, at the m.Transform the Entity carries.
//
// Data flows one way. Scene keeps no per-entity state, so there is no
// scene-side object for an Entity to be a copy of, and the Components are the
// source of truth because there is no other candidate.
//
// ecsscene is a Bundle. Its plugin, built by ecssceneplugin.New, requires no
// Adapter and contributes none. This package declares what it offers: the
// Components a game spawns (Model, Mesh, Animation, Params, Material, Light,
// Camera), MaterialTag, the camera, layer and pass vocabulary, Name, and the
// ordering identities LoadOnUpdate and RecordOnUpdate. The Component
// registrations, the two scratches and the two Systems are in ecsscene's
// internal/: the load System, which keys changed Entities into Batches and is
// the only one that loads, and the recording System. The Components are still
// registered by the plugin that defines their Go type, because that plugin
// ships in this same Bundle under this package's Name. The Components are plain
// data with no methods; internal/types declares only the vocabulary the root
// aliases, because Layer forwards there.
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
// The Components and the vocabulary's aliases are in types.go, and Layer in
// utils.go.
package ecsscene
