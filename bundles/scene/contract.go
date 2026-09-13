// Package scene records declarative 3D frames - cameras and their passes, glTF
// models, buffer-built meshes, punctual lights and debug shapes - into a
// frame-local queue, and loads, queries and unloads models through a persistent
// lookup.
//
// scene is a Bundle. This package is its contract root and declares no plugin:
// the OpQueue and Lookup resources with the LookupAccess facade, the recording
// vocabulary (Transform, CameraDescr, Pass, Material, MeshDraw, ModelDraw,
// LightDescr, ...), the inspection views, the errors, the pure coordinate
// helpers, and the ordering identity FlushOnUpdate. The plugin - the flush that
// culls, sorts and packs a recording into gfx passes and draws, the model load
// commands and the built-in shader mount - is in sceneimpl, which only
// composition roots and tests import. The code the two share, including the
// consume side of the queue, the model table and the glTF loader, is in
// internal.
//
// OpQueue and Lookup are concrete types, aliased from internal, so recording a
// draw is a direct method call with nothing between the caller and the queue.
package scene

import (
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/slots/app"
)

// Name is the scene plugin's name.
const Name kernel.PluginName = "scene"

// FlushOnUpdate is the subscription type of the plugin's flush on
// app.UpdateEvent. It is registered Last and ordered
// Before[gfx.PresentOnUpdate]: it culls, sorts and packs the tick's recording
// into gfx passes and draws and republishes the queue, so everything that
// records into *OpQueue during the tick orders before it.
type FlushOnUpdate kernel.Subscription[app.UpdateEvent]
