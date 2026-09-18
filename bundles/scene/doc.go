// Package scene declares the 3D Bundle: declarative frames - cameras and their
// passes, glTF models, buffer-built meshes, punctual lights and debug shapes -
// recorded into a frame-local queue, and models loaded, queried and unloaded
// through a persistent lookup.
//
// scene is a Bundle. Its plugin, built by sceneplugin.New, requires no Adapter
// and contributes none. This package declares what it offers: the OpQueue and
// Lookup resources with the LookupAccess and LookupDeviceAccess facades, the recording vocabulary
// (Transform, CameraDescr, Pass, Material, MeshDraw, ModelDraw, LightDescr,
// ...), the inspection views, Config, the errors, the pure coordinate helpers,
// and the ordering identity FlushOnUpdate. The flush that loads the models a
// frame named, culls, sorts and packs a recording into gfx passes and draws, and
// the built-in shader mount, are in scene's internal/.
//
// OpQueue and Lookup are concrete types, aliased from internal/types, so
// recording a draw is a direct method call with nothing between the caller and
// the queue.
package scene
