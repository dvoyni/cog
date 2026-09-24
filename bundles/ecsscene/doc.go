// Package ecsscene draws Entities: it is the ECS's renderer over model, beside
// scene rather than on top of it.
//
// It is a binding, and a binding is necessarily a third plugin: ecs imports
// nothing of model and model imports nothing of ecs, so what attaches them is
// an ordinary cog plugin that imports both. A project not using the ECS does
// not register it and schedules no ECS Systems. An app runs ecsscene or scene,
// never both.
//
// Its Components wrap model's values — a model.ModelRef, a model.MeshRef,
// model.ClipPlays, a model.LightDescr — and gfx.ParameterDescrs, beside
// ecsscene's own copy of the camera, layer and pass vocabulary (CameraID,
// ProjectionKind, PassTag, Pass, LayerMask), which keeps scene's names, shapes
// and zero values without naming scene. There is no manifest, no hash and no
// name table: a Component holds the glTF path itself. ecsscene repeats scene's
// path, recording to gfx itself: once a tick it buckets every drawable Entity
// into a Batch by the key the load System wrote on change, and draws each
// Batch's surviving instances as one instanced draw, at the m.Transform each
// Entity carries. It imports nothing of scene.
//
// Data flows one way. The frame is rebuilt from the Stores every tick, and the
// Components are the source of truth because there is no other candidate.
//
// ecsscene is a Bundle. Its plugin, built by ecssceneplugin.New, requires no
// Adapter and contributes one, StorageReadMount, which mounts the debug
// shapes' shader in storage. This package is an index of what it offers, every
// name an alias of what its internal/ declares: the
// Components a game spawns (Model, Mesh, Animation, Params, Material, Light,
// Camera, and the debug shapes DebugBox, DebugSphere, DebugPlane, DebugLine
// and DebugWireBox), MaterialTag, the camera, layer and pass vocabulary, Name,
// and the ordering identities LoadOnUpdate, RecordOnUpdate and DebugOnUpdate.
// The Component registrations, the scratches and the Systems are in
// ecsscene's internal/: the load System, which keys changed Entities into
// Batches and is the only one that loads, the recording System, and the debug
// shapes' two Systems per shape, which bake each shape into a Mesh. internal/
// declares the Components and registers them under this package's Name; they
// are plain data with no methods and every field exported. internal/ also
// declares the vocabulary, which Layer forwards into.
//
// Where an Entity stands is not one of them. It is an m.Transform, whose one
// Store the ecs plugin registers, so that ecsaudio and a game's own Systems
// read the same placement this binding draws from rather than a copy of it.
// Every recorded Entity needs one, and an Entity without one is not recorded
// whatever else it carries.
//
// A game whose drawables are shaped differently writes its own recording
// System and does not register this plugin. docs/README.md is the API, and
// its prohibitions are what a second binding has to keep true;
// docs/specs/ecsscene.md is the design record.
//
// The Components' aliases are in components.go, the vocabulary's and
// MaterialTag's in types.go, StorageReadMount's in adapters.go, the errors' in
// err.go, and Layer's forwarder in utils.go.
package ecsscene
