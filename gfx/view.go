package gfx

import (
	"strconv"

	"github.com/dvoyni/cog/app"
)

// The view types are the vocabulary cog's snapshots share. gfx declares them
// because gfx owns the descriptors they render - a texture, a parameter, a
// material - and because canvas and ui already depend on gfx, so one value
// reaches an agent in one shape whichever tool showed it. They are deliberately
// not in mcp, which is the contract leaf and must never learn what a texture
// is, and deliberately not one set per package, which is how the same texture
// ends up spelled two ways in two tools.
//
// They exist at all because marshalling the descriptors themselves is a dead
// end: every gfx descriptor has entirely unexported fields, so json.Marshal
// yields {}. The rejected repair was an unsafe cast to a mirror struct with
// public fields, and it fails on four counts - it emits the dead half of a
// tagged union, it base64s inline pixel data into the reply, it puts JSON in
// gfx's public contract for every cog app, and nothing checks that the mirror
// still matches. Each view below is built through gfx's own union-aware
// accessors instead, which the compiler checks.
//
// Two rules hold across all of them, and they are what a reader should be able
// to rely on without reading the code:
//
//   - A tagged union serializes to exactly one value. A parameter that is one
//     float carries one number, not eight empty fields beside it.
//   - Bulk bytes never travel. Inline pixels and raw parameter data are
//     reported as a byte count and left where they are.

// SnapshotView is what every snapshot response carries whatever it is a
// snapshot of: the three coordinate sizes, and whether producing it cost a
// step.
//
// All three sizes, not two. A capture reports pixels and window units and
// deliberately omits the logical viewport, because that is the game's own
// sizing policy and means nothing to an agent looking at a picture. A snapshot
// is the inverse case - its coordinates are in that space - so leaving it out
// breaks the find-the-button-click-the-button flow silently, which is the
// failure mode worth spending a field on.
type SnapshotView struct {
	// PixelWidth and PixelHeight are the framebuffer's size: the units a
	// capture's image is in.
	PixelWidth  int `json:"pixelWidth"`
	PixelHeight int `json:"pixelHeight"`
	// WindowWidth and WindowHeight are the window's size in device-independent
	// units: what input capabilities take, and what a click is expressed in.
	WindowWidth  float32 `json:"windowWidth"`
	WindowHeight float32 `json:"windowHeight"`
	// ViewportWidth and ViewportHeight are the logical world size the game's
	// own sizing policy resolved to. Snapshot coordinates are in this space,
	// and window = viewport x WindowWidth / ViewportWidth.
	ViewportWidth  float32 `json:"viewportWidth"`
	ViewportHeight float32 `json:"viewportHeight"`
	// Stepped reports that the engine was paused and one tick was stepped to
	// have something to record. Joined reports that the step joined one
	// another arm had already raised, which is what makes snapshots armed
	// together describe one tick rather than three.
	Stepped bool `json:"stepped"`
	Joined  bool `json:"joined,omitempty"`
}

// SnapshotViewOf fills in the three coordinate sizes from a viewport. The step
// fields belong to the capability body, which is the only place that knows
// whether one was performed.
func SnapshotViewOf(viewport app.Viewport) SnapshotView {
	return SnapshotView{
		PixelWidth:     int(viewport.FramebufferWidth),
		PixelHeight:    int(viewport.FramebufferHeight),
		WindowWidth:    viewport.WindowWidth,
		WindowHeight:   viewport.WindowHeight,
		ViewportWidth:  viewport.Width,
		ViewportHeight: viewport.Height,
	}
}

// ParameterView is one shader parameter rendered for an agent: its name, which
// of the union's arms it is, and that arm's value alone.
type ParameterView struct {
	Name string `json:"name"`
	// Kind is the arm: color, float, vec4, mat4, texture, sampler, buffer, raw,
	// or none. It says which of the fields below is present, and exactly one of
	// them ever is.
	Kind string `json:"kind"`
	// Value is a numeric parameter's components in shader order - one for a
	// float, four for a color (r, g, b, a) or a vec4 (x, y, z, w), sixteen for
	// a mat4. One field rather than four, because the kind already says how to
	// read it and four fields would mean three empty ones on every parameter.
	Value   []float32    `json:"value,omitempty"`
	Texture *TextureView `json:"texture,omitempty"`
	Sampler *SamplerView `json:"sampler,omitempty"`
	Buffer  *BufferView  `json:"buffer,omitempty"`
	// Bytes is the length of a raw parameter's data. The data itself does not
	// travel: it is already laid out for one shader, so an agent can do nothing
	// with the bytes but carry them.
	Bytes int `json:"bytes,omitempty"`
}

// ParameterViewOf renders one parameter. It reads through the accessors rather
// than the fields, so a new arm on the union that forgets to answer here
// serializes as its kind and no value, instead of as somebody else's value.
func ParameterViewOf(parameter ParameterDescr) ParameterView {
	view := ParameterView{Name: parameter.Name(), Kind: parameter.kind.String()}
	if value, ok := parameter.ColorValue(); ok {
		view.Value = []float32{value.R, value.G, value.B, value.A}
	}
	if value, ok := parameter.FloatValue(); ok {
		view.Value = []float32{value}
	}
	if value, ok := parameter.VecValue(); ok {
		view.Value = []float32{value.X, value.Y, value.Z, value.W}
	}
	if value, ok := parameter.MatValue(); ok {
		view.Value = value[:]
	}
	if value, ok := parameter.TextureValue(); ok {
		texture := TextureViewOf(value)
		view.Texture = &texture
	}
	if value, ok := parameter.SamplerValue(); ok {
		sampler := SamplerViewOf(value)
		view.Sampler = &sampler
	}
	if value, ok := parameter.BufferValue(); ok {
		buffer := BufferViewOf(value)
		buffer.Offset, buffer.Range, _ = parameter.BufferRange()
		view.Buffer = &buffer
	}
	if bytes, ok := parameter.RawLen(); ok {
		view.Bytes = bytes
	}
	return view
}

// ParameterViewsOf renders a parameter list in order. It is the form both
// callers actually want, and it keeps the empty case one nil rather than one
// empty array in every response.
func ParameterViewsOf(parameters []ParameterDescr) []ParameterView {
	if len(parameters) == 0 {
		return nil
	}
	views := make([]ParameterView, len(parameters))
	for i := range parameters {
		views[i] = ParameterViewOf(parameters[i])
	}
	return views
}

// TextureView is one texture rendered for an agent: where it came from, how
// big it is, and how many bytes of pixels it is carrying - never the pixels.
//
// The name is the one the spec family agreed on across three tools. It is not
// a GPU texture view; that is TextureViewDimension and TextureViewID, which
// are a different thing gfx also has.
type TextureView struct {
	// Source is how the texture resolves: resource, bytes or baked.
	Source string `json:"source"`
	// Path is the storage path a resource texture names.
	Path string `json:"path,omitempty"`
	// ID is the baked handle. It is opaque - nothing lists textures to an agent
	// - but it is how two ops naming one texture are recognised as doing so.
	ID     TextureID `json:"id,omitempty"`
	Width  int       `json:"width,omitempty"`
	Height int       `json:"height,omitempty"`
	Format string    `json:"format,omitempty"`
	// Mipmaps reports that a full mip chain is generated at bake.
	Mipmaps bool `json:"mipmaps,omitempty"`
	// Bytes is the size of the inline upload the descriptor carries. An inline
	// texture in a response would be a base64 megabyte nobody asked for, so the
	// count travels and the pixels do not.
	Bytes int `json:"bytes,omitempty"`
}

// TextureViewOf renders one texture descriptor.
func TextureViewOf(texture TextureDescr) TextureView {
	view := TextureView{
		Source:  textureSourceName(texture.source),
		Path:    texture.Path(),
		ID:      texture.ID(),
		Format:  formatName(texture.Format()),
		Mipmaps: texture.Mipmaps(),
		Bytes:   texture.PixelBytes(),
	}
	view.Width, view.Height = texture.Size()
	return view
}

// BufferView is one buffer rendered for an agent, plus the slice of it a
// parameter binds when a parameter is what produced the view.
type BufferView struct {
	// Source is how the buffer resolves: bytes or baked.
	Source string   `json:"source"`
	ID     BufferID `json:"id,omitempty"`
	Size   int      `json:"size,omitempty"`
	// Offset and Range are the bound slice. A zero Range means the whole buffer
	// from Offset, which is how a draw that binds all of one says so.
	Offset int `json:"offset,omitempty"`
	Range  int `json:"range,omitempty"`
	// Bytes is the size of the inline upload the descriptor carries, for the
	// reason TextureView.Bytes gives.
	Bytes int `json:"bytes,omitempty"`
}

// BufferViewOf renders one buffer descriptor. The bound range is not part of a
// buffer and is filled in by whatever bound it.
func BufferViewOf(buffer BufferDescr) BufferView {
	return BufferView{
		Source: bufferSourceName(buffer.source),
		ID:     buffer.ID(),
		Size:   buffer.Size(),
		Bytes:  buffer.InlineBytes(),
	}
}

// SamplerView is one sampler rendered for an agent, with every mode named
// rather than numbered: a sampler is small enough to report whole, and an
// enum ordinal in a debug dump is a lookup an agent cannot perform.
type SamplerView struct {
	AddressU string `json:"addressU"`
	AddressV string `json:"addressV"`
	Mag      string `json:"mag"`
	Min      string `json:"min"`
	Mip      string `json:"mip"`
	// Anisotropy is the maximum anisotropic sample count; 0 and 1 both mean
	// off.
	Anisotropy uint8 `json:"anisotropy,omitempty"`
	// Comparison makes this a comparison sampler, which a shadow map needs, and
	// Compare is the test it applies.
	Comparison bool   `json:"comparison,omitempty"`
	Compare    string `json:"compare,omitempty"`
	Label      string `json:"label,omitempty"`
}

// SamplerViewOf renders one sampler descriptor.
func SamplerViewOf(sampler SamplerDesc) SamplerView {
	view := SamplerView{
		AddressU:   addressModeName(sampler.AddressU),
		AddressV:   addressModeName(sampler.AddressV),
		Mag:        filterModeName(sampler.Mag),
		Min:        filterModeName(sampler.Min),
		Mip:        filterModeName(sampler.Mip),
		Anisotropy: sampler.Anisotropy,
		Comparison: sampler.Comparison,
		Label:      sampler.Label,
	}
	if sampler.Comparison {
		view.Compare = compareFuncName(sampler.Compare)
	}
	return view
}

// MaterialView is one material rendered for an agent: which shader variant
// shades the draw, the fixed pipeline state, and the material's own
// parameters, which a draw's same-named parameters override.
type MaterialView struct {
	Shader     ShaderView        `json:"shader"`
	State      MaterialStateView `json:"state"`
	Parameters []ParameterView   `json:"parameters,omitempty"`
}

// MaterialViewOf renders one material descriptor.
func MaterialViewOf(material MaterialDescr) MaterialView {
	return MaterialView{
		Shader:     ShaderViewOf(material.Shader()),
		State:      MaterialStateViewOf(material.State()),
		Parameters: ParameterViewsOf(material.Params()),
	}
}

// ShaderView names one shader variant. A root source plus one supply is one
// variant, so the supply is part of the name rather than a detail beside it.
type ShaderView struct {
	// Path is the storage path of the root source, and is empty when the
	// source was inline text.
	Path string `json:"path,omitempty"`
	// Inline says the root source was text handed to gfx rather than a file.
	// The text is not reported: a whole WGSL source is not an identity, and it
	// would swamp every draw that used one.
	Inline bool `json:"inline,omitempty"`
	// Supply is the defines and consts the preprocessor resolved the source
	// against, one per line. One path under two supplies is two shaders, which
	// is exactly the difference a snapshot is asked to show.
	Supply string `json:"supply,omitempty"`
}

// ShaderViewOf renders one shader descriptor.
func ShaderViewOf(shader ShaderDescr) ShaderView {
	path := shader.Path()
	return ShaderView{Path: path, Inline: path == "", Supply: shader.Supply()}
}

// MaterialStateView is fixed pipeline state with its enums named. Depth
// compare and depth write are separate here because they are separate in the
// engine: test but do not write is a state 3D needs and one flag cannot say.
type MaterialStateView struct {
	Blend        string `json:"blend"`
	DepthCompare string `json:"depthCompare"`
	DepthWrite   bool   `json:"depthWrite"`
	Cull         string `json:"cull"`
	FrontFace    string `json:"frontFace"`
}

// MaterialStateViewOf renders one pipeline state.
func MaterialStateViewOf(state MaterialState) MaterialStateView {
	return MaterialStateView{
		Blend:        blendModeName(state.Blend),
		DepthCompare: compareFuncName(state.DepthCompare),
		DepthWrite:   state.DepthWrite,
		Cull:         cullModeName(state.Cull),
		FrontFace:    frontFaceName(state.FrontFace),
	}
}

// The name tables. They are unexported on purpose: naming an enum for a debug
// document is not the same promise as giving every gfx enum a String, which
// would put a rendering of every member into the package's public surface for
// the sake of one reader. An unknown value names its ordinal rather than
// falling back to a legal-looking name, so a member added without touching
// this file is visible instead of mislabelled.

func textureSourceName(source textureSource) string {
	switch source {
	case TextureSourceResource:
		return "resource"
	case TextureSourceBytes:
		return "bytes"
	case TextureSourceBaked:
		return "baked"
	}
	return unknownName(int(source))
}

func bufferSourceName(source bufferSource) string {
	switch source {
	case BufferSourceBytes:
		return "bytes"
	case BufferSourceBaked:
		return "baked"
	}
	return unknownName(int(source))
}

func bufferKindName(kind BufferKind) string {
	switch kind {
	case BufferVertex:
		return "vertex"
	case BufferIndex:
		return "index"
	case BufferUniform:
		return "uniform"
	case BufferStorage:
		return "storage"
	}
	return unknownName(int(kind))
}

func addressModeName(mode AddressMode) string {
	switch mode {
	case AddressClamp:
		return "clamp"
	case AddressRepeat:
		return "repeat"
	case AddressMirror:
		return "mirror"
	}
	return unknownName(int(mode))
}

// FilterModeName is the view vocabulary's spelling of a sampler filter. It is
// the one name table that is exported, and the rule is narrow: a name crosses
// the package boundary only where a sibling snapshot reports a gfx enum of its
// own - canvas records a FilterMode on every sprite transform. Exporting it
// rather than letting canvas keep a second table is the same promise the views
// make, that one value reaches an agent in one shape. It is still not
// FilterMode.String: naming an enum for a debug document is not a commitment to
// render every gfx enum for every cog app.
func FilterModeName(mode FilterMode) string { return filterModeName(mode) }

func filterModeName(mode FilterMode) string {
	switch mode {
	case FilterLinear:
		return "linear"
	case FilterNearest:
		return "nearest"
	}
	return unknownName(int(mode))
}

func blendModeName(mode BlendMode) string {
	switch mode {
	case BlendAlpha:
		return "alpha"
	case BlendOpaque:
		return "opaque"
	case BlendAdditive:
		return "additive"
	case BlendMultiply:
		return "multiply"
	}
	return unknownName(int(mode))
}

func compareFuncName(compare CompareFunc) string {
	switch compare {
	case CompareAlways:
		return "always"
	case CompareNever:
		return "never"
	case CompareLess:
		return "less"
	case CompareLessEqual:
		return "lessEqual"
	case CompareGreater:
		return "greater"
	case CompareGreaterEqual:
		return "greaterEqual"
	case CompareEqual:
		return "equal"
	case CompareNotEqual:
		return "notEqual"
	}
	return unknownName(int(compare))
}

func cullModeName(mode CullMode) string {
	switch mode {
	case CullNone:
		return "none"
	case CullFront:
		return "front"
	case CullBack:
		return "back"
	}
	return unknownName(int(mode))
}

func frontFaceName(face FrontFace) string {
	switch face {
	case FrontCCW:
		return "ccw"
	case FrontCW:
		return "cw"
	}
	return unknownName(int(face))
}

func loadOpName(load LoadOp) string {
	switch load {
	case LoadPreserve:
		return "preserve"
	case LoadClear:
		return "clear"
	case LoadDiscard:
		return "discard"
	}
	return unknownName(int(load))
}

func storeOpName(store StoreOp) string {
	switch store {
	case StoreKeep:
		return "keep"
	case StoreDiscard:
		return "discard"
	}
	return unknownName(int(store))
}

// unknownName spells a value no table names, so an enum that grew reads as
// unknown rather than as whichever name happened to be last.
func unknownName(value int) string {
	return "unknown(" + strconv.Itoa(value) + ")"
}
