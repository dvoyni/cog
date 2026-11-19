package canvas

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// Inspection is one frame written down in canvas's terms: its beat; each
// camera it was seen through, in drawing order, with the batches it drew
// and their entries; and, beside it, render's own inspection of the same
// frame, which shows what reached the GPU. Assets.ReadInspection answers the
// one Assets.RequestInspection asked for.
type Inspection struct {
	Beat    metronome.Beat
	Cameras []InspectedCamera
	// Render is render's inspection of the frame, as render's
	// Frame.ReadInspection answered it.
	Render render.Inspection
}

// InspectedCamera is one camera of an inspected frame that drew: the
// camera as set, or the default camera for a frame setting none; the window
// it drew into, its own or the main window when it named no target, and
// zero for a camera into its Texture; its viewport in the target's pixels;
// and its batches, in the order drawn. A camera skipped, of an empty viewport, a window not open or
// a target refused, or one with nothing to draw and no clear, is not
// inspected.
type InspectedCamera struct {
	Camera   Camera
	Window   platform.Window
	Viewport m.Recti
	Batches  []InspectedBatch
}

// InspectedBatch is one batch of an inspected camera, a run of entries in
// sort order sharing a 2D material and a clip, drawn as one instanced draw:
// its material, its clip, its instance count, and the entries it drew, in
// sort order, those the camera culled left out. A
// fill and a line are one instance; an entry drawing several, such as a
// stroke, its four sides, or a run of text, a glyph each, makes the count
// larger than the entries.
type InspectedBatch struct {
	Material      Material
	Clip          m.Rect
	InstanceCount uint32
	Entries       []InspectedEntry
}

// InspectedEntry is one entry of an inspected batch: its kind, the entry as
// recorded, its sort key, and its place in the frame: the place of the draw
// list that recorded it, among the frame's lists in registration order, and
// its own place in that list, in recording order.
type InspectedEntry struct {
	Kind EntryKind
	// Shape is the entry recorded, when Kind is EntryKindShape.
	Shape           Shape
	Layer           int32
	Order           int32
	DrawListIndex   int
	IndexInDrawList int
}

// EntryKind is what kind of entry an inspected entry is.
type EntryKind uint8

const (
	// EntryKindShape is a Shape.
	EntryKindShape EntryKind = iota + 1
)
