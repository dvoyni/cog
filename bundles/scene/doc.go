// Package scene declares the 3D Bundle: declarative frames - cameras and their
// passes, glTF models, buffer-built meshes, punctual lights and debug shapes -
// recorded into a frame-local queue, and models loaded, queried and unloaded
// through model's persistent Lookup.
//
// scene is a Bundle and a renderer. Its plugin, built by sceneplugin.New,
// depends on the model plugin, requires no Adapter and contributes none. This
// package offers what the renderer has: the OpQueue resource, the recording
// vocabulary (CameraDescr, Pass, Material, MeshDraw, ModelDraw, ...), the
// inspection views, the errors, the pure coordinate helpers, and the ordering
// identity FlushOnUpdate. It declares none of them: each is an alias of, or a
// forwarder into, what scene's internal/ declares, beside the flush that loads
// the models a frame named, culls, sorts and packs a recording into gfx passes
// and draws.
//
// Everything a model file can contain is model's, and is named from there:
// *model.Lookup and its facades, model.ModelRef, model.MeshRef,
// model.ClipPlay, model.LightDescr, model.Vertex, model.Config and the
// ErrModel and ErrMesh reports. scene's root aliases none of them.
//
// Everything recorded is placed by an m.Transform, which scene takes rather
// than declares, because sound is heard from one too. A non-uniform scale costs
// its draw the inverse-transpose normal path in the shader, and only its draw:
// the packer flags the instances whose basis does not scale uniformly, and
// every other instance keeps the plain one.
//
// OpQueue is a concrete type, aliased from internal, so recording a draw is a
// direct method call with nothing between the caller and the queue.
package scene
