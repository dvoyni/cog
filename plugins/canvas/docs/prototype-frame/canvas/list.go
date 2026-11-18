package canvas

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/mx"
)

// DrawList is one recording task's entries for a frame. In canvas it is a kernel
// parameter each recording task declares, so parallel tasks never contend;
// here Prototype.BeginList answers it. The lists are merged by sort key when
// the frame is drawn, a list's order breaking ties before the call order.
type DrawList struct{ list *list }

type list struct {
	label   string
	index   int
	entries []entry
	cameras []cameraCall
	calls   int
}

type cameraCall struct {
	camera     Camera
	list, call int
}

// entry is one thing recorded into a list: a sprite, a shape, a run of text
// or a mesh, with its sort key.
type entry struct {
	layer, order int32
	list, call   int
	material     Material
	atlas        string
	filter       Filter
	clip         m.Rect
	bounds       m.Rect
	batch        string
	instances    int
	describe     string
}

func (l DrawList) next() int {
	l.list.calls++
	return l.list.calls
}

func (l DrawList) add(recorded entry) {
	recorded.list, recorded.call = l.list.index, l.next()
	if recorded.filter == FilterDefault {
		recorded.filter = tables.config.DefaultFilter
		if recorded.filter == FilterDefault {
			recorded.filter = FilterLinear
		}
	}
	l.list.entries = append(l.list.entries, recorded)
}

// checkArgument answers whether argument may go to material, reporting once
// when it may not.
func checkArgument(material Material, argument any) bool {
	declared := tables.materials[material.id].descriptor.Argument.goType
	switch {
	case argument == nil:
		return true
	case declared == nil:
		tables.report(fmt.Sprintf("an argument of %T to %s, which takes none, is refused", argument, tables.materialName(material)))
		return false
	case reflect.TypeOf(argument) != declared:
		tables.report(fmt.Sprintf("an argument of %T to %s, which takes %s, is refused", argument, tables.materialName(material), declared))
		return false
	}
	return true
}

func (l DrawList) recordSprite(sprite Sprite, argument any) {
	if !checkArgument(sprite.Material, argument) {
		return
	}
	image := tables.loadImage(sprite.Image)
	frameSize := image.frameSize()
	size := sizeOf(sprite.Size, frameSize)
	pivot := image.key.options.Pivot
	offset := pivot.Fraction.Mul(size).Add(pivot.Texels.Mul(size.Div(frameSize)))
	local := m.Rect{X: -size.X/2 - offset.X, Y: -size.Y/2 - offset.Y, Width: size.X, Height: size.Y}
	describe := image.name()
	if image.key.options.Sheet != (Grid{}) {
		describe += fmt.Sprintf("#%d", sprite.Frame)
	}
	switch sprite.DrawMode {
	case Sliced:
		describe += " sliced"
	case Tiled:
		describe += " tiled"
	}
	if sprite.Crop != (Crop{}) {
		describe += fmt.Sprintf(" cropped %.0f%%", 100*(1-sprite.Crop.Right-sprite.Crop.Left))
	}
	l.add(entry{layer: sprite.Layer, order: sprite.Order, material: sprite.Material, atlas: image.atlas(), filter: sprite.Filter,
		clip: sprite.Clip, bounds: transformRect(sprite.Transform, local), instances: 1, describe: describe})
}

func (l DrawList) recordShape(shape Shape, argument any) {
	if !checkArgument(shape.Material, argument) {
		return
	}
	local, instances, describe := shape.Rect, 1, "fill"
	switch shape.Kind {
	case Stroke:
		instances, describe = 4, "stroke"
	case Line:
		half := shape.Width / 2
		local = m.Rect{X: min(shape.From.X, shape.To.X) - half, Y: min(shape.From.Y, shape.To.Y) - half,
			Width: abs(shape.To.X-shape.From.X) + shape.Width, Height: abs(shape.To.Y-shape.From.Y) + shape.Width}
		describe = "line"
	}
	if shape.Width == 0 && shape.Kind != Fill {
		describe += " hairline"
	}
	l.add(entry{layer: shape.Layer, order: shape.Order, material: shape.Material, atlas: "sprites/sRGB", clip: shape.Clip,
		bounds: transformRect(shape.Transform, local), instances: instances, describe: describe})
}

func (l DrawList) recordText(text Text, argument any) {
	material := text.Material
	if material == (Material{}) {
		material = TextMaterial
	}
	if !checkArgument(material, argument) {
		return
	}
	layout, run := tables.fonts.layout(text)
	size := layout.Size()
	pivot := text.Pivot
	em := cmpOr(text.Size, tables.config.TextSize, 16)
	offset := pivot.Fraction.Mul(size).Add(pivot.Texels.MulS(em))
	local := m.Rect{X: -size.X/2 - offset.X, Y: -size.Y/2 - offset.Y, Width: size.X, Height: size.Y}
	atlas := "glyphs"
	if run.icons > 0 {
		atlas = "glyphs+sprites"
	}
	describe := `"` + shorten(run.plain, 28) + `"`
	if language := cmpOr(text.Language, tables.fonts.language); language != tables.fonts.language || text.Language != "" {
		describe += " (" + language + ")"
	}
	if profile, ok := argument.(Profile); ok && profile != (Profile{}) {
		describe += " outlined"
	}
	// A run's zero filter is its font's, never Config.DefaultFilter: linear
	// for a distance field, nearest for a bitmap font.
	filter := text.Filter
	if filter == FilterDefault {
		filter = FilterLinear
		if tables.fonts.sets[text.Font.id].Default.Bitmap > 0 {
			filter = FilterNearest
		}
	}
	l.add(entry{layer: text.Layer, order: text.Order, material: material, atlas: atlas, filter: filter, clip: text.Clip,
		bounds: transformRect(text.Transform, local), instances: run.glyphs, describe: describe})
}

func (l DrawList) recordMesh(mesh Mesh, argument any) {
	if !checkArgument(mesh.Material, argument) {
		return
	}
	file, ok := tables.loadMeshFile(mesh.File)
	if !ok {
		return
	}
	if !file.standard && tables.materials[mesh.Material.id].descriptor.VertexEntryPoint == "" {
		tables.report(fmt.Sprintf("%s's own layout drawn with %s, which has no vertex stage, is refused", file.path, tables.materialName(mesh.Material)))
		return
	}
	image := tables.loadImage(mesh.Image)
	bounds := m.Rect{X: file.bounds.Min.X, Y: file.bounds.Min.Y, Width: file.bounds.Max.X - file.bounds.Min.X, Height: file.bounds.Max.Y - file.bounds.Min.Y}
	l.add(entry{layer: mesh.Layer, order: mesh.Order, material: mesh.Material, atlas: image.atlas(), filter: mesh.Filter,
		clip: mesh.Clip, bounds: transformRect(mesh.Transform, bounds), batch: fmt.Sprintf("mesh file %d", mesh.File.id),
		instances: 1, describe: file.path + " over " + image.name()})
}

func (l DrawList) recordTriangles(options DrawTrianglesOptions, vertexCount int, box m.Box3, standard bool, argument any) {
	if !checkArgument(options.Material, argument) {
		return
	}
	if box == (m.Box3{}) {
		tables.report("DrawTriangles without bounds is refused")
		return
	}
	if !standard && tables.materials[options.Material.id].descriptor.VertexEntryPoint == "" {
		tables.report(fmt.Sprintf("triangles of another layout drawn with %s, which has no vertex stage, are refused", tables.materialName(options.Material)))
		return
	}
	image := tables.loadImage(options.Image)
	bounds := m.Rect{X: box.Min.X, Y: box.Min.Y, Width: box.Max.X - box.Min.X, Height: box.Max.Y - box.Min.Y}
	l.add(entry{layer: options.Layer, order: options.Order, material: options.Material, atlas: image.atlas(), filter: options.Filter,
		clip: options.Clip, bounds: transformRect(options.Transform, bounds), batch: fmt.Sprintf("triangles %d.%d", l.list.index, l.list.calls+1),
		instances: 1, describe: fmt.Sprintf("per-frame triangles, %d vertices", vertexCount)})
}

func (l DrawList) recordCamera(camera Camera) {
	l.list.cameras = append(l.list.cameras, cameraCall{camera: camera, list: l.list.index, call: l.next()})
}

// sizeOf answers an entry's size: size, its zero the frame's, one zero axis
// keeping the aspect ratio.
func sizeOf(size, frame m.Vec2) m.Vec2 {
	switch {
	case size.X == 0 && size.Y == 0:
		return frame
	case size.X == 0:
		return m.Vec2{X: size.Y * frame.X / frame.Y, Y: size.Y}
	case size.Y == 0:
		return m.Vec2{X: size.X, Y: size.X * frame.Y / frame.X}
	}
	return size
}

func transformRect(transform mx.Mat2x3, rect m.Rect) m.Rect {
	if transform == (mx.Mat2x3{}) {
		transform = mx.Identity2x3()
	}
	corners := [4]m.Vec2{{X: rect.X, Y: rect.Y}, {X: rect.X + rect.Width, Y: rect.Y}, {X: rect.X, Y: rect.Y + rect.Height}, {X: rect.X + rect.Width, Y: rect.Y + rect.Height}}
	low, high := m.Vec2{X: math.MaxFloat32, Y: math.MaxFloat32}, m.Vec2{X: -math.MaxFloat32, Y: -math.MaxFloat32}
	for _, corner := range corners {
		point := transform.TransformPoint(corner)
		low, high = m.Vec2{X: min(low.X, point.X), Y: min(low.Y, point.Y)}, m.Vec2{X: max(high.X, point.X), Y: max(high.Y, point.Y)}
	}
	return m.Rect{X: low.X, Y: low.Y, Width: high.X - low.X, Height: high.Y - low.Y}
}

func overlaps(a, b m.Rect) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

func grow(rect m.Rect, by float32) m.Rect {
	return m.Rect{X: rect.X - by, Y: rect.Y - by, Width: rect.Width + 2*by, Height: rect.Height + 2*by}
}

func abs(value float32) float32 { return float32(math.Abs(float64(value))) }

func cmpOr[T comparable](values ...T) T {
	var zero T
	for _, value := range values {
		if value != zero {
			return value
		}
	}
	return zero
}

func shorten(text string, length int) string {
	runes := []rune(strings.ReplaceAll(text, "\n", "⏎"))
	if len(runes) <= length {
		return string(runes)
	}
	return string(runes[:length-1]) + "…"
}
