package canvas

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/metronome"
	"github.com/dvoyni/cog/plugins/platform"
	"github.com/dvoyni/cog/plugins/render"
)

// Inspection is one frame written down in canvas's terms: its beat; each
// camera it was seen through, in drawing order, with the batches it drew,
// their entries, and the entries it culled; the frame's first uses and
// reports; how long canvas's jobs took; and, beside it, render's own
// inspection of the same frame, which shows what reached the GPU.
// Assets.ReadInspection answers the one Assets.RequestInspection asked for,
// and String writes it as text, for an agent to log.
//
// The first uses and reports are those canvas made and logged in any job
// after the frame before it was written down, up to its own drawing: those
// of the jobs recording its entries and drawing it, and of any job between,
// such as Assets.MakeMaterial's. A frame not written down, drawn while the
// request is pending, leaves its own to the next.
type Inspection struct {
	Beat    metronome.Beat
	Cameras []InspectedCamera
	// PackedImages are the frames of images packed at their first draw,
	// into a sprite atlas or a texture of their own, each the first to draw
	// its texels, an image of a render texture never among them;
	// LoadedMeshFiles the mesh files loaded at their first draw, one that
	// failed reported instead; MadeLayouts the runs of text laid out on a
	// miss of the layout cache, drawn or measured, each its Language the
	// one it was shaped in; and MadeGlyphs the runs whose glyphs' fields
	// were made, drawn or preloaded. Each is in the order made, runs made
	// by jobs at once in any order.
	PackedImages    []InspectedImage
	LoadedMeshFiles []MeshFile
	MadeLayouts     []Text
	MadeGlyphs      []InspectedGlyphCount
	// Reports are the reports canvas logged, each the first of its subject
	// since its key was last forgotten, in the order logged.
	Reports []Report
	// SortTime is how long canvas's job merging and sorting the frame's
	// entries took, and DrawTime its job drawing them, each measured by
	// metronome's Clock, through stopwatches named SortStopwatchName and
	// DrawStopwatchName.
	SortTime time.Duration
	DrawTime time.Duration
	// Render is render's inspection of the frame, as render's
	// Frame.ReadInspection answered it.
	Render render.Inspection
}

// String writes the inspection as text, for an agent to log, as canvas's
// prototype printed its frames: the frame's beat; its first uses and its
// reports, one a line; how long canvas took to sort and to draw it; then
// each camera, in drawing order: its target, its viewport, scale and the
// world it sees, its clear, its batches under the layers they draw, each
// with its material, texture, filter, clip, instance count and entries,
// equal entries in a row written once and counted, and how many entries it
// drew and culled, the culled named.
func (i Inspection) String() string {
	var text strings.Builder
	fmt.Fprintf(&text, "frame %d\n", i.Beat.Tick)
	for _, packed := range i.PackedImages {
		fmt.Fprintf(&text, "  · %s packed at its first draw\n", describeImage(packed.Image, packed.Frame))
	}
	for _, file := range i.LoadedMeshFiles {
		fmt.Fprintf(&text, "  · %s loaded at its first draw\n", meshFileNames.findKeyAt(file.index))
	}
	for _, laidOut := range i.MadeLayouts {
		fmt.Fprintf(&text, "  · %s laid out\n", describeText(laidOut))
	}
	for _, made := range i.MadeGlyphs {
		fmt.Fprintf(&text, "  · %d glyphs of %s made\n", made.Count, describeText(made.Text))
	}
	for _, report := range i.Reports {
		fmt.Fprintf(&text, "  ! %s\n", report.Error())
	}
	fmt.Fprintf(&text, "  sorted in %v, drawn in %v\n", i.SortTime, i.DrawTime)
	for index := range i.Cameras {
		i.Cameras[index].write(&text, index+1)
	}
	return text.String()
}

// SortStopwatchName and DrawStopwatchName name the stopwatches canvas times
// its sorting and drawing jobs by, for a test's metronome.ManualClock to
// lengthen with LengthenNext. Only a frame inspected is timed, so a
// lengthening applies to the next frame inspected.
const (
	SortStopwatchName = "canvas sort"
	DrawStopwatchName = "canvas draw"
)

// InspectedCamera is one camera of an inspected frame: the camera as set,
// or the default camera for a frame setting none; the window it drew into,
// its own or the main window when it named no target, and zero for a
// camera into its Texture, and the label render holds that texture under;
// its target's size in pixels; its viewport in them; its scale,
// the target's pixels to a world unit along each axis; the world rectangle
// it sees, the bounding box of its viewport's corners in the world; its
// batches, in the order drawn; and the entries it culled, those of its
// layers whose bounds miss what it sees, in sort order. A camera skipped,
// of an empty viewport, a window not open or a target refused, is not
// inspected; one that culled every entry of its layers, or had none, and
// does not clear, drew nothing, and is inspected with no batches.
type InspectedCamera struct {
	Camera       Camera
	Window       platform.Window
	TextureLabel string
	TargetSize   m.Vec2i
	Viewport     m.Recti
	Scale        m.Vec2
	VisibleRect  m.Rect
	Batches      []InspectedBatch
	Culled       []InspectedEntry
}

// write writes c, the number-th camera of its frame, to text, as
// Inspection.String does.
func (c *InspectedCamera) write(text *strings.Builder, number int) {
	target := fmt.Sprintf("window %v", c.Window)
	if c.Camera.Texture != (render.Texture{}) {
		target = fmt.Sprintf("texture %q", c.TextureLabel)
	}
	fmt.Fprintf(text, "\n  camera %d, order %d → %s %d×%d px\n", number, c.Camera.Order, target, c.TargetSize.X, c.TargetSize.Y)
	scale := fmt.Sprintf("%.2f px/unit", c.Scale.X)
	if c.Scale.X != c.Scale.Y {
		scale = fmt.Sprintf("%.2f×%.2f px/unit", c.Scale.X, c.Scale.Y)
	}
	seenMin := c.VisibleRect.Min()
	seenMax := c.VisibleRect.Max()
	fmt.Fprintf(text, "    viewport (%d,%d) %d×%d px · %s · sees (%g,%g)–(%g,%g)\n", c.Viewport.X, c.Viewport.Y, c.Viewport.Width, c.Viewport.Height, scale, seenMin.X, seenMin.Y, seenMax.X, seenMax.Y)
	if c.Camera.Clear {
		color := c.Camera.ClearColor
		fmt.Fprintf(text, "    clears its viewport to (%g, %g, %g, %g)\n", color.R, color.G, color.B, color.A)
	}
	if c.Camera.PixelPerfect {
		text.WriteString("    pixel-perfect: entries snap to the world-unit grid\n")
	}
	if c.Camera.SampleCount == multisampledCameraSampleCount {
		fmt.Fprintf(text, "    %d samples, resolved and drawn over its viewport\n", multisampledCameraSampleCount)
	}
	layers := ""
	drawnCount := 0
	for index, batch := range c.Batches {
		if batchLayers := describeLayers(batch.Entries); batchLayers != layers {
			fmt.Fprintf(text, "    %s\n", batchLayers)
			layers = batchLayers
		}
		clip := "no clip"
		if batch.Clip != (m.Rect{}) {
			clip = fmt.Sprintf("clip (%g,%g) %g×%g", batch.Clip.X, batch.Clip.Y, batch.Clip.Width, batch.Clip.Height)
		}
		fmt.Fprintf(text, "      batch %d: %s · %s · %s · %s · %d instances: %s\n", index+1, batch.MaterialLabel, batch.TextureLabel, describeFilter(batch.Filter), clip, batch.InstanceCount, describeEntries(batch.Entries))
		drawnCount += len(batch.Entries)
	}
	fmt.Fprintf(text, "    %d entries drawn in %d draws, %d culled\n", drawnCount, len(c.Batches), len(c.Culled))
	if len(c.Culled) > 0 {
		fmt.Fprintf(text, "    culled: %s\n", describeEntries(c.Culled))
	}
}

// InspectedImage is a frame of an image.
type InspectedImage struct {
	Image Image
	Frame uint32
}

// InspectedGlyphCount is how many glyphs were made for a run of text: the
// run, as given, or a String of the characters FontLibrary.Preload was given
// in its Font set, its Language the one it was shaped in; and how many
// glyphs' fields were made for it, those it shares with runs made before it
// not counted.
type InspectedGlyphCount struct {
	Text  Text
	Count int
}

// InspectedBatch is one batch of an inspected camera, a run of entries in
// sort order sharing a 2D material, a texture, a filter and a clip, drawn
// as one instanced draw: its material, and its descriptor's Label, or its
// Shader when it has none; the texture its entries' images are
// drawn from, the atlas's they are packed into, shapes' the sRGB sprite
// atlas's, holding the white texel, or a render texture's, and the label
// render holds it under; its filter, the first of its entries' that names one, the white
// texel's taking none, and FilterDefault for a batch of the white texel
// alone; its clip; its instance count; and the
// entries it drew, in sort order, those the camera culled left out. A fill,
// a line and a sprite are one instance; an entry drawing several, such as a
// stroke, its four sides, or a run of text, a glyph each, makes the count
// larger than the entries.
type InspectedBatch struct {
	Material      Material
	MaterialLabel string
	Texture       render.Texture
	TextureLabel  string
	Filter        Filter
	Clip          m.Rect
	InstanceCount uint32
	Entries       []InspectedEntry
}

// InspectedEntry is one entry of an inspected batch: its kind, the entry as
// recorded, its sort key, and its place in the frame: the place of the
// command list that recorded it, among the frame's lists in registration
// order, and its own place in that list, in recording order.
type InspectedEntry struct {
	Kind EntryKind
	// Shape is the entry recorded, when Kind is EntryKindShape, Sprite when
	// it is EntryKindSprite, Text when it is EntryKindText,
	// TrianglesOptions, the options per-frame triangles were drawn with,
	// when it is EntryKindTriangles, and Mesh when it is EntryKindMesh.
	Shape              Shape
	Sprite             Sprite
	Text               Text
	TrianglesOptions   DrawTrianglesOptions
	Mesh               Mesh
	Layer              int32
	Order              int32
	CommandListIndex   int
	IndexInCommandList int
}

// EntryKind is what kind of entry an entry is.
type EntryKind uint8

const (
	// EntryKindShape is a Shape.
	EntryKindShape EntryKind = iota + 1
	// EntryKindSprite is a Sprite.
	EntryKindSprite
	// EntryKindText is a run of Text.
	EntryKindText
	// EntryKindTriangles is a per-frame mesh, DrawTriangles's.
	EntryKindTriangles
	// EntryKindMesh is a Mesh, of a mesh file.
	EntryKindMesh
)

// describeLayers answers the layers entries, a batch's, are of: one, or
// the first and last of several.
func describeLayers(entries []InspectedEntry) string {
	first := entries[0].Layer
	last := entries[len(entries)-1].Layer
	if first == last {
		return fmt.Sprintf("layer %d", first)
	}
	return fmt.Sprintf("layers %d–%d", first, last)
}

// describeEntries answers entries, each described, equal descriptions in a
// row written once and counted.
func describeEntries(entries []InspectedEntry) string {
	var text strings.Builder
	description := ""
	count := 0
	write := func() {
		if text.Len() > 0 {
			text.WriteString(", ")
		}
		text.WriteString(description)
		if count > 1 {
			fmt.Fprintf(&text, " ×%d", count)
		}
	}
	for _, entry := range entries {
		next := describeEntry(entry)
		if count > 0 && next != description {
			write()
			count = 0
		}
		description = next
		count++
	}
	if count > 0 {
		write()
	}
	return text.String()
}

// describeEntry answers what entry draws: a shape's kind, a sprite's image,
// a run's text, or a mesh's file, and the image triangles or a mesh sample.
func describeEntry(entry InspectedEntry) string {
	switch entry.Kind {
	case EntryKindShape:
		shape := entry.Shape
		description := [...]string{Fill: "fill", Stroke: "stroke", Line: "line"}[shape.Kind]
		if shape.Kind != Fill && shape.Width == 0 {
			description += " hairline"
		}
		if shape.Radius > 0 {
			description += " rounded"
		}
		return description
	case EntryKindSprite:
		description := describeImage(entry.Sprite.Image, entry.Sprite.Frame)
		switch entry.Sprite.DrawMode {
		case Sliced:
			description += " sliced"
		case Tiled:
			description += " tiled"
		}
		if entry.Sprite.Crop != (Crop{}) {
			description += " cropped"
		}
		return description
	case EntryKindText:
		return describeText(entry.Text)
	case EntryKindTriangles:
		return describeSampled("per-frame triangles", entry.TrianglesOptions.Image, entry.TrianglesOptions.Frame)
	}
	return describeSampled(meshFileNames.findKeyAt(entry.Mesh.File.index), entry.Mesh.Image, entry.Mesh.Frame)
}

// describeSampled answers drawn, a mesh, sampling frame of image, the white
// texel when image is the zero one.
func describeSampled(drawn string, image Image, frame uint32) string {
	if image == (Image{}) {
		return drawn
	}
	return drawn + " over " + describeImage(image, frame)
}

// describeImage answers frame of image: the white texel for the zero
// Image, else its path, or its render texture, the frame's number after it
// when the image is a sheet, and its converter's type.
func describeImage(image Image, frame uint32) string {
	if image == (Image{}) {
		return "white texel"
	}
	key := imageNames.findKey(image)
	description := key.findSubject()
	if key.options.Sheet != (Grid{}) {
		description += fmt.Sprintf("#%d", frame)
	}
	if key.options.Convert != nil {
		description += fmt.Sprintf(" as %T", key.options.Convert)
	}
	return description
}

// describedTextLength is the most characters describeText quotes of a run.
const describedTextLength = 32

// describeText answers text quoted, its spans' text joined, cut at
// describedTextLength characters, and its language after it when it names
// one.
func describeText(text Text) string {
	characters := text.String
	if len(text.Spans) > 0 {
		var joined strings.Builder
		for _, span := range text.Spans {
			joined.WriteString(span.Text)
		}
		characters = joined.String()
	}
	if utf8.RuneCountInString(characters) > describedTextLength {
		cut := 0
		for range describedTextLength {
			_, size := utf8.DecodeRuneInString(characters[cut:])
			cut += size
		}
		characters = characters[:cut] + "…"
	}
	description := strconv.Quote(characters)
	if text.Language != "" {
		description += " (" + text.Language + ")"
	}
	return description
}

// describeFilter answers filter's name, none for FilterDefault, which a
// batch of the white texel alone has.
func describeFilter(filter Filter) string {
	switch filter {
	case FilterLinear:
		return "linear"
	case FilterNearest:
		return "nearest"
	}
	return "no filter"
}
