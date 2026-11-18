package canvas

// This file is the prototype's harness, standing in for canvas on the kernel
// and for its drawing: nothing here is canvas's API.

import (
	"cmp"
	"fmt"
	"io/fs"
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/dvoyni/cog/libs/m"
	"prototype.invalid/canvasframe/meshfile"
	"prototype.invalid/canvasframe/platform"
	"prototype.invalid/canvasframe/render"
)

// Prototype stands in for canvas's plugin and the kernel around it.
type Prototype struct {
	fonts   Fonts
	targets Targets
	window  m.Vec2
	scale   float32
}

// NewPrototype answers canvas configured by config, reading files, drawing
// into a main window of window points at scale.
func NewPrototype(config Config, files platform.FS, window m.Vec2, scale float32) *Prototype {
	tables.config, tables.files = config, files
	tables.fonts.language = config.Language
	return &Prototype{fonts: tables.fonts, targets: tables.targets, window: window, scale: scale}
}

// PrototypeImageSize tells the prototype an image file's size in texels,
// which it cannot read; an image it is not told of is 32 by 32.
func PrototypeImageSize(path string, width, height float32) {
	tables.sizes[path] = m.Vec2{X: width, Y: height}
}

// Fonts answers the Fonts resource, for kernel.Share.
func (p *Prototype) Fonts() *Fonts { return &p.fonts }

// Targets answers the Targets resource, for kernel.Share.
func (p *Prototype) Targets() *Targets { return &p.targets }

// Assets answers the Assets parameter.
func (p *Prototype) Assets() Assets { return Assets{} }

// BeginList answers the list of the recording task label.
func (p *Prototype) BeginList(label string) DrawList {
	entry := &list{label: label, index: len(tables.lists)}
	tables.lists = append(tables.lists, entry)
	return DrawList{entry}
}

// End draws the frame: it prints what canvas would draw, then forgets the
// entries and records each target's size as last drawn.
func (p *Prototype) End() {
	tables.frame++
	fmt.Printf("\n══ frame %d ══\n", tables.frame)
	for _, line := range tables.events {
		fmt.Println("  · " + line)
	}
	for _, line := range tables.reports {
		fmt.Println("  ! reported once: " + line)
	}
	tables.events, tables.reports = nil, nil
	var cameras []cameraCall
	var entries []entry
	for _, entry := range tables.lists {
		cameras = append(cameras, entry.cameras...)
		entries = append(entries, entry.entries...)
	}
	if len(cameras) == 0 {
		cameras = []cameraCall{{camera: Camera{Clear: true, ClearColor: m.Color{A: 1}}}}
		fmt.Println("  no camera set: the default camera draws")
	}
	slices.SortStableFunc(cameras, func(a, b cameraCall) int {
		return cmp.Or(cmp.Compare(a.camera.Order, b.camera.Order), cmp.Compare(isWindow(a.camera), isWindow(b.camera)))
	})
	for index, call := range cameras {
		p.drawCamera(index, call, entries)
	}
	tables.targets.windows[platform.Window{}] = targetSize{size: p.window, scale: p.scale}
	for _, call := range cameras {
		if info, ok := render.Info(call.camera.Texture); ok {
			tables.targets.textures[call.camera.Texture] = m.Vec2{X: float32(info.Width), Y: float32(info.Height)}
		}
	}
	tables.lists = nil
}

func isWindow(camera Camera) int {
	if camera.Texture != (render.Texture{}) {
		return 0
	}
	return 1
}

func (p *Prototype) drawCamera(index int, call cameraCall, entries []entry) {
	camera := call.camera
	target, pixels, dpi := "main window", m.Vec2{X: p.window.X * p.scale, Y: p.window.Y * p.scale}, p.scale
	if info, ok := render.Info(camera.Texture); ok {
		target, pixels, dpi = "texture "+info.Label, m.Vec2{X: float32(info.Width), Y: float32(info.Height)}, 1
	}
	view := computeView(camera, pixels, dpi)
	fmt.Printf("\n  camera %d, order %d → %s %.0f×%.0f px\n", index+1, camera.Order, target, pixels.X, pixels.Y)
	fmt.Printf("    viewport (%.0f,%.0f) %.0f×%.0f px · %s · sees (%.0f,%.0f)–(%.0f,%.0f)\n",
		view.viewport.X, view.viewport.Y, view.viewport.Width, view.viewport.Height, view.describeScale(),
		view.visible.X, view.visible.Y, view.visible.X+view.visible.Width, view.visible.Y+view.visible.Height)
	if camera.Clear {
		fmt.Printf("    clears its viewport to %s\n", describeColor(camera.ClearColor))
	}
	if camera.PixelPerfect {
		fmt.Println("    pixel-perfect: entries snap to the world-unit grid")
	}
	if camera.Samples == 4 {
		fmt.Println("    4 samples, resolved into the target")
	}
	var seen []entry
	culled := 0
	for _, entry := range entries {
		if !inRanges(camera.Layers, entry.layer) {
			continue
		}
		bounds := grow(entry.bounds, tables.materials[entry.material.id].descriptor.CullMargin)
		if !overlaps(bounds, view.visible) || (entry.clip != (m.Rect{}) && !overlaps(entry.clip, view.visible)) {
			culled++
			continue
		}
		seen = append(seen, entry)
	}
	slices.SortStableFunc(seen, func(a, b entry) int {
		if order := cmp.Or(cmp.Compare(a.layer, b.layer), cmp.Compare(a.order, b.order)); order != 0 || !slices.Contains(tables.config.RegroupedLayers, a.layer) {
			return order
		}
		return strings.Compare(batchKey(a), batchKey(b))
	})
	batch, instances, lastLayer := 0, 0, int32(math.MinInt32)
	var describes []string
	flush := func(entry entry) {
		if len(describes) == 0 {
			return
		}
		batch++
		clip := "no clip"
		if entry.clip != (m.Rect{}) {
			clip = fmt.Sprintf("clip (%.0f,%.0f %.0f×%.0f)", entry.clip.X, entry.clip.Y, entry.clip.Width, entry.clip.Height)
		}
		fmt.Printf("      batch %d: %s · %s · %s · %s · %d instances: %s\n", batch, tables.materialName(entry.material), entry.atlas,
			filterName(entry.filter), clip, instances, strings.Join(collapse(describes), ", "))
		describes, instances = nil, 0
	}
	for position, entry := range seen {
		if entry.layer != lastLayer {
			fmt.Printf("    layer %d\n", entry.layer)
			lastLayer = entry.layer
		}
		describes = append(describes, entry.describe)
		instances += entry.instances
		if position+1 == len(seen) || batchKey(seen[position+1]) != batchKey(entry) || seen[position+1].layer != entry.layer {
			flush(entry)
		}
	}
	fmt.Printf("    %d entries drawn in %d draws, %d culled\n", len(seen), batch, culled)
}

func batchKey(entry entry) string {
	return fmt.Sprintf("%d|%s|%d|%v|%s", entry.material.id, entry.atlas, entry.filter, entry.clip, entry.batch)
}

func inRanges(ranges [4]LayerRange, layer int32) bool {
	if ranges == ([4]LayerRange{}) {
		return true
	}
	for _, r := range ranges {
		if layer >= r.From && layer < r.To {
			return true
		}
	}
	return false
}

func collapse(items []string) []string {
	var out []string
	for index := 0; index < len(items); {
		run := 1
		for index+run < len(items) && items[index+run] == items[index] {
			run++
		}
		if run > 1 {
			out = append(out, fmt.Sprintf("%s ×%d", items[index], run))
		} else {
			out = append(out, items[index])
		}
		index += run
	}
	return out
}

func filterName(filter Filter) string {
	if filter == FilterNearest {
		return "nearest"
	}
	return "linear"
}

func describeColor(color m.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", uint8(color.R*255), uint8(color.G*255), uint8(color.B*255))
}

// view is a camera's mapping onto its target.
type view struct {
	viewport  m.Rect // in the target's pixels
	drawn     m.Rect // the part entries draw into: the viewport, less Inscribe's bars
	scale     m.Vec2 // pixels per world unit
	visible   m.Rect
	position  m.Vec2
	pivot     m.Vec2 // in the target's pixels
	rotation  float32
	dpi       float32
	pixelGrid bool
}

func computeView(camera Camera, pixels m.Vec2, dpi float32) view {
	fraction := camera.Viewport
	if fraction == (m.Rect{}) {
		fraction = m.Rect{Width: 1, Height: 1}
	}
	viewport := m.Rect{X: fraction.X * pixels.X, Y: fraction.Y * pixels.Y, Width: fraction.Width * pixels.X, Height: fraction.Height * pixels.Y}
	logical := camera.Size
	switch {
	case logical.X == 0 && logical.Y == 0:
		logical = m.Vec2{X: viewport.Width / dpi, Y: viewport.Height / dpi}
	case logical.X == 0:
		logical.X = logical.Y * viewport.Width / viewport.Height
	case logical.Y == 0:
		logical.Y = logical.X * viewport.Height / viewport.Width
	}
	zoom := cmpOr(camera.Zoom, 1)
	fit := camera.Fit
	if camera.PixelPerfect && fit == FitStretch {
		tables.report("a pixel-perfect camera's FitStretch is drawn as FitInscribe")
		fit = FitInscribe
	}
	across, down := viewport.Width/logical.X, viewport.Height/logical.Y
	var scale m.Vec2
	switch fit {
	case FitExpand, FitInscribe:
		scale = m.Vec2{X: min(across, down) * zoom, Y: min(across, down) * zoom}
	case FitOverlap:
		scale = m.Vec2{X: max(across, down) * zoom, Y: max(across, down) * zoom}
	case FitStretch:
		scale = m.Vec2{X: across * zoom, Y: down * zoom}
	}
	if camera.PixelPerfect {
		whole := float32(math.Floor(float64(scale.X)))
		if fit == FitOverlap {
			whole = float32(math.Ceil(float64(scale.X)))
		}
		whole = max(whole, 1)
		scale = m.Vec2{X: whole, Y: whole}
	}
	drawn := viewport
	if fit == FitInscribe {
		size := m.Vec2{X: logical.X / zoom * scale.X, Y: logical.Y / zoom * scale.Y}
		drawn = m.Rect{X: viewport.X + (viewport.Width-size.X)/2, Y: viewport.Y + (viewport.Height-size.Y)/2, Width: size.X, Height: size.Y}
	}
	pivot := m.Vec2{X: drawn.X + (0.5+camera.Pivot.X)*drawn.Width, Y: drawn.Y + (0.5+camera.Pivot.Y)*drawn.Height}
	result := view{viewport: viewport, drawn: drawn, scale: scale, position: camera.Position, pivot: pivot, rotation: camera.Rotation,
		dpi: dpi, pixelGrid: camera.PixelPerfect}
	low, high := m.Vec2{X: math.MaxFloat32, Y: math.MaxFloat32}, m.Vec2{X: -math.MaxFloat32, Y: -math.MaxFloat32}
	for _, corner := range [4]m.Vec2{{X: drawn.X, Y: drawn.Y}, {X: drawn.X + drawn.Width, Y: drawn.Y}, {X: drawn.X, Y: drawn.Y + drawn.Height}, {X: drawn.X + drawn.Width, Y: drawn.Y + drawn.Height}} {
		point := result.pixelToWorld(corner)
		low, high = m.Vec2{X: min(low.X, point.X), Y: min(low.Y, point.Y)}, m.Vec2{X: max(high.X, point.X), Y: max(high.Y, point.Y)}
	}
	result.visible = m.Rect{X: low.X, Y: low.Y, Width: high.X - low.X, Height: high.Y - low.Y}
	return result
}

func (v view) pixelToWorld(pixel m.Vec2) m.Vec2 {
	offset := pixel.Sub(v.pivot).Div(v.scale)
	sine, cosine := math.Sincos(float64(v.rotation))
	return v.position.Add(m.Vec2{X: offset.X*float32(cosine) - offset.Y*float32(sine), Y: offset.X*float32(sine) + offset.Y*float32(cosine)})
}

func (v view) worldToPixel(point m.Vec2) m.Vec2 {
	offset := point.Sub(v.position)
	sine, cosine := math.Sincos(float64(-v.rotation))
	turned := m.Vec2{X: offset.X*float32(cosine) - offset.Y*float32(sine), Y: offset.X*float32(sine) + offset.Y*float32(cosine)}
	return v.pivot.Add(turned.Mul(v.scale))
}

func (v view) toWorld(point m.Vec2) (m.Vec2, bool) {
	pixel := point.MulS(v.dpi)
	return v.pixelToWorld(pixel), contains(v.drawn, pixel)
}

func (v view) toScreen(point m.Vec2) (m.Vec2, bool) {
	pixel := v.worldToPixel(point)
	return pixel.DivS(v.dpi), contains(v.drawn, pixel)
}

func contains(rect m.Rect, point m.Vec2) bool {
	return point.X >= rect.X && point.X < rect.X+rect.Width && point.Y >= rect.Y && point.Y < rect.Y+rect.Height
}

func (v view) describeScale() string {
	if v.scale.X == v.scale.Y {
		return fmt.Sprintf("%.2f px/unit", v.scale.X)
	}
	return fmt.Sprintf("%.2f×%.2f px/unit", v.scale.X, v.scale.Y)
}

func (c Camera) view(targets Targets) (view, bool) {
	if c.Texture != (render.Texture{}) {
		size, drawn := targets.Texture(c.Texture)
		return computeView(c, size, 1), drawn
	}
	size, scale, drawn := targets.Window(c.Window)
	if !drawn {
		return view{}, false
	}
	return computeView(c, size.MulS(scale), scale), true
}

// The tables canvas keeps.

type imageKey struct {
	path    string
	texture render.Texture
	options ImageOptions
}

type imageEntry struct {
	key    imageKey
	size   m.Vec2
	loaded bool
}

func (e imageEntry) name() string {
	if info, ok := render.Info(e.key.texture); ok {
		return "texture " + info.Label
	}
	if e.key.path == "" {
		return "white texel"
	}
	if e.key.options.Convert != nil {
		return fmt.Sprintf("%s as %T", e.key.path, e.key.options.Convert)
	}
	return e.key.path
}

func (e imageEntry) atlas() string {
	if info, ok := render.Info(e.key.texture); ok {
		return "texture " + info.Label
	}
	if e.key.options.ColorSpace == ColorSpaceLinear {
		return "sprites/linear"
	}
	return "sprites/sRGB"
}

func (e imageEntry) frameSize() m.Vec2 {
	grid := e.key.options.Sheet
	if grid == (Grid{}) {
		return e.size
	}
	return m.Vec2{X: e.size.X / float32(grid.Columns), Y: e.size.Y / float32(grid.Rows)}
}

type meshEntry struct {
	path     string
	loaded   bool
	failed   bool
	standard bool
	bounds   m.Box3
}

type materialEntry struct{ descriptor MaterialDescriptor }

type targetSize struct {
	size  m.Vec2
	scale float32
}

type targetsResource struct {
	windows  map[platform.Window]targetSize
	textures map[render.Texture]m.Vec2
}

func (t *targetsResource) Window(window platform.Window) (m.Vec2, float32, bool) {
	entry, ok := t.windows[window]
	return entry.size, entry.scale, ok
}

func (t *targetsResource) Texture(texture render.Texture) (m.Vec2, bool) {
	size, ok := t.textures[texture]
	return size, ok
}

type canvasTables struct {
	config    Config
	files     fs.FS
	images    []imageEntry
	imageIDs  map[imageKey]Image
	packed    map[packedKey]bool
	sizes     map[string]m.Vec2
	meshes    []meshEntry
	meshIDs   map[string]MeshFile
	materials []materialEntry
	fonts     *fontsResource
	targets   *targetsResource
	lists     []*list
	frame     int
	events    []string
	reports   []string
	reported  map[string]bool
}

var tables = &canvasTables{
	images:   []imageEntry{{size: m.Vec2{X: 1, Y: 1}, loaded: true}},
	imageIDs: map[imageKey]Image{},
	packed:   map[packedKey]bool{},
	sizes:    map[string]m.Vec2{},
	meshes:   []meshEntry{{}},
	meshIDs:  map[string]MeshFile{},
	materials: []materialEntry{
		{MaterialDescriptor{Label: "default"}},
		{MaterialDescriptor{Label: "sdf", Argument: ArgumentOf[Profile]()}},
		{MaterialDescriptor{Label: "text", Argument: ArgumentOf[Profile]()}},
	},
	fonts:    &fontsResource{sets: []FontSetDescriptor{{Label: "default"}}, icons: map[string]IconDescriptor{}, colors: map[string]m.Color{}},
	targets:  &targetsResource{windows: map[platform.Window]targetSize{}, textures: map[render.Texture]m.Vec2{}},
	reported: map[string]bool{},
}

func (t *canvasTables) report(message string) {
	if !t.reported[message] {
		t.reported[message] = true
		t.reports = append(t.reports, message)
	}
}

func (t *canvasTables) event(format string, arguments ...any) {
	t.events = append(t.events, fmt.Sprintf(format, arguments...))
}

func (t *canvasTables) internImage(key imageKey) Image {
	if image, ok := t.imageIDs[key]; ok {
		return image
	}
	image := Image{uint32(len(t.images))}
	size, ok := t.sizes[key.path]
	if info, isTexture := render.Info(key.texture); isTexture {
		size, ok = m.Vec2{X: float32(info.Width), Y: float32(info.Height)}, true
	}
	if !ok {
		size = m.Vec2{X: 32, Y: 32}
	}
	t.images = append(t.images, imageEntry{key: key, size: size})
	t.imageIDs[key] = image
	return image
}

// packedKey is what an image is packed by: its options less those that
// change no pixel, the pivot and the slice insets.
type packedKey struct {
	path       string
	sourceRect m.Rect
	colorSpace ColorSpace
	wrap       Wrap
	sheet      Grid
	convert    Converter
}

func packedOf(key imageKey) packedKey {
	options := key.options
	return packedKey{key.path, options.SourceRect, options.ColorSpace, options.Wrap, options.Sheet, options.Convert}
}

func (t *canvasTables) loadImage(image Image) imageEntry {
	entry := &t.images[image.id]
	if !entry.loaded {
		entry.loaded = true
		if packed := packedOf(entry.key); entry.key.texture == (render.Texture{}) && !t.packed[packed] {
			t.packed[packed] = true
			t.event("%s loaded and packed into %s at its first use", entry.name(), entry.atlas())
		}
	}
	return *entry
}

func (t *canvasTables) internMeshFile(path string) MeshFile {
	if file, ok := t.meshIDs[path]; ok {
		return file
	}
	file := MeshFile{uint32(len(t.meshes))}
	t.meshes = append(t.meshes, meshEntry{path: path})
	t.meshIDs[path] = file
	return file
}

func (t *canvasTables) loadMeshFile(file MeshFile) (meshEntry, bool) {
	entry := &t.meshes[file.id]
	if !entry.loaded {
		entry.loaded = true
		data, err := fs.ReadFile(t.files, entry.path)
		var file meshfile.File
		if err == nil {
			file, err = meshfile.Decode(data)
		}
		if err != nil {
			entry.failed = true
			t.report(fmt.Sprintf("mesh file %s draws nothing: %v", entry.path, err))
		} else {
			entry.bounds = file.Bounds
			entry.standard = slices.Equal(file.Layout.Attributes, Vertex{}.VertexLayout().Attributes) && file.Layout.Stride == 32
			t.event("%s loaded at its first draw: %d vertices, %d indices of %d bits", entry.path, file.VertexCount, file.IndexCount, file.IndexBits)
		}
	}
	return *entry, !entry.failed
}

func (t *canvasTables) unload(path string) {
	for index := range t.images {
		if entry := &t.images[index]; entry.loaded && entry.key.path != "" && under(entry.key.path, path) {
			entry.loaded = false
			if packed := packedOf(entry.key); t.packed[packed] {
				delete(t.packed, packed)
				t.event("%s unloaded", entry.name())
			}
		}
	}
	for index := range t.meshes {
		if entry := &t.meshes[index]; entry.loaded && under(entry.path, path) {
			entry.loaded, entry.failed = false, false
			t.event("%s unloaded", entry.path)
		}
	}
}

func under(path, prefix string) bool {
	return path == prefix || (strings.HasSuffix(prefix, "/") && strings.HasPrefix(path, prefix))
}

func (t *canvasTables) makeMaterial(descriptor MaterialDescriptor) Material {
	t.materials = append(t.materials, materialEntry{descriptor})
	t.event("material %q made from %s", descriptor.Label, descriptor.Shader)
	return Material{uint32(len(t.materials) - 1)}
}

func (t *canvasTables) materialName(material Material) string {
	return t.materials[material.id].descriptor.Label
}

// The Fonts resource.

type fontsResource struct {
	language string
	sets     []FontSetDescriptor
	icons    map[string]IconDescriptor
	colors   map[string]m.Color
}

func (f *fontsResource) Language() string { return f.language }

func (f *fontsResource) Metrics(set FontSet, size float32, language string) Metrics {
	return Metrics{Ascent: size * 0.95, Descent: size * 0.25, LineHeight: size * 1.25, XHeight: size * 0.5, CapHeight: size * 0.7}
}

func (f *fontsResource) Layout(text Text) Layout {
	layout, _ := f.layout(text)
	return layout
}

func (f *fontsResource) Preload(set FontSet, text string) {
	tables.event("glyphs of %q made ahead of their first draw in font set %q", shorten(text, 20), f.sets[set.id].Label)
}

func (f *fontsResource) MakeFontSet(descriptor FontSetDescriptor) FontSet {
	files := 1
	for _, entry := range descriptor.Languages {
		files += len(entry.Fonts)
	}
	f.sets = append(f.sets, descriptor)
	tables.event("font set %q made, its %d font files loaded", descriptor.Label, files)
	return FontSet{uint32(len(f.sets) - 1)}
}

func (f *fontsResource) DestroyFontSet(set FontSet) {}

func (f *fontsResource) SetLanguage(tag string) {
	f.language = tag
	tables.event("language set to %q", tag)
}

func (f *fontsResource) RegisterIcon(name string, icon IconDescriptor) {
	f.icons[name] = icon
	tables.loadImage(icon.Image)
}

func (f *fontsResource) RegisterColor(name string, color m.Color) { f.colors[name] = color }

type textRun struct {
	plain  string
	glyphs int
	icons  int
}

// layout lays text out roughly: half an em a character, a quarter more for
// the line gap.
func (f *fontsResource) layout(text Text) (Layout, textRun) {
	run := f.flatten(text)
	em := cmpOr(text.Size, tables.config.TextSize, 16)
	advance := em*0.55 + cmpOr(text.Tracking)*em
	lineHeight := em * 1.25 * cmpOr(text.LineHeight, 1)
	var lines []float32
	for _, paragraph := range strings.Split(run.plain, "\n") {
		width := float32(len([]rune(paragraph))) * advance
		for text.WrapWidth > 0 && width > text.WrapWidth {
			lines = append(lines, text.WrapWidth)
			width -= text.WrapWidth
		}
		lines = append(lines, width)
	}
	widest := slices.Max(lines)
	if text.WrapWidth > 0 {
		widest = text.WrapWidth
	}
	clusters := make([]m.Vec2, 0, len(run.plain))
	for index := range []rune(run.plain) {
		clusters = append(clusters, m.Vec2{X: float32(index) * advance})
	}
	return Layout{size: m.Vec2{X: widest, Y: float32(len(lines)) * lineHeight}, lines: lines, baseline: em * 0.95, clusters: clusters}, run
}

// flatten answers text's characters, an icon each one, reading its markup or
// its spans.
func (f *fontsResource) flatten(text Text) textRun {
	var run textRun
	var out strings.Builder
	icon := func(name string) {
		if _, ok := f.icons[name]; !ok {
			tables.report(fmt.Sprintf("icon %q is not registered: drawn as the missing-glyph box", name))
		}
		run.icons++
		out.WriteRune('▣')
	}
	switch {
	case len(text.Spans) > 0:
		for _, span := range text.Spans {
			if span.Icon != "" {
				icon(span.Icon)
			}
			out.WriteString(span.Text)
		}
	case text.Markup:
		if !f.readMarkup(text.String, &out, icon) {
			tables.report(fmt.Sprintf("markup %q does not parse: drawn as written", shorten(text.String, 24)))
			out.Reset()
			out.WriteString(text.String)
		}
	default:
		out.WriteString(text.String)
	}
	run.plain = out.String()
	for _, r := range run.plain {
		if !unicode.IsSpace(r) {
			run.glyphs++
		}
	}
	return run
}

func (f *fontsResource) readMarkup(source string, out *strings.Builder, icon func(string)) bool {
	runes := []rune(source)
	depth := []int{0}
	isName := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-/", r) }
	for index := 0; index < len(runes); index++ {
		r := runes[index]
		switch {
		case r == '\\' && index+1 < len(runes) && (runes[index+1] == '\\' || runes[index+1] == '}'):
			out.WriteRune(runes[index+1])
			index++
		case r == '\\' && index+2 < len(runes) && runes[index+2] == ':' && (runes[index+1] == 'i' || runes[index+1] == 'c'):
			end := index + 3
			for end < len(runes) && (isName(runes[end]) || runes[index+1] == 'c' && runes[end] == '#') {
				end++
			}
			name := string(runes[index+3 : end])
			if runes[index+1] == 'i' {
				icon(name)
				index = end - 1
				continue
			}
			if end >= len(runes) || runes[end] != '{' {
				return false
			}
			if _, ok := f.colors[name]; !ok && !strings.HasPrefix(name, "#") {
				tables.report(fmt.Sprintf("colour %q is not registered", name))
			}
			depth = append(depth, 0)
			index = end
		case r == '{' && len(depth) > 1:
			depth[len(depth)-1]++
			out.WriteRune(r)
		case r == '}' && len(depth) > 1:
			if depth[len(depth)-1] == 0 {
				depth = depth[:len(depth)-1]
				continue
			}
			depth[len(depth)-1]--
			out.WriteRune(r)
		default:
			out.WriteRune(r)
		}
	}
	return len(depth) == 1
}
