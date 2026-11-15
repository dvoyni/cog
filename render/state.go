package render

import (
	"cmp"

	"github.com/dvoyni/cog/gfx"
)

// maxColorTargetCount is how many colour targets a pass may have by WebGPU's
// default limits, and so how many a render state describes.
const maxColorTargetCount = 8

// State is a material's render state: how its draws blend and mask each
// colour target, test, write and bias depth, test stencil, and cull faces,
// the whole pipeline state a material sets, by value and comparable. Its zero value,
// and the zero value of each of its fields, is WebGPU's default: no
// blending, every channel written, the depth test passing always with no
// depth written or biased, the stencil test passing always and keeping
// every value, through every bit, no face culled, counter-clockwise faces
// in front, and no alpha to coverage. Its fields are phrased so: a mask
// names the bits or channels left out, not those kept. render ships no
// presets.
type State struct {
	// ColorTargets is the blend and write mask of each colour target of the
	// pass drawn into, by its index; those past the pass's last are unused.
	ColorTargets [maxColorTargetCount]ColorTargetState
	// Depth is the depth test, write and bias, and Stencil the stencil test,
	// both used only in a pass with a depth-stencil target of a format that
	// has the aspect.
	Depth   DepthState
	Stencil StencilState
	// CullMode is which faces are culled, and FrontFace which winding faces
	// the front.
	CullMode  CullMode
	FrontFace FrontFace
	// AlphaToCoverage turns the alpha of the first colour target into the
	// coverage of a multisampled pass.
	AlphaToCoverage bool
}

// makeKeyState answers s as a pass of formats draws it, in one form for every
// state that draws the same, so they share a pipeline: the colour targets the
// pass lacks, and with no depth-stencil target the depth and stencil state,
// zero; each other value that is its field's default spelt zero; and a blend
// that blends as none does spelt as none.
func (s State) makeKeyState(formats passFormat) State {
	for index := range s.ColorTargets {
		target := &s.ColorTargets[index]
		if index >= formats.colorCount {
			*target = ColorTargetState{}
			continue
		}
		target.Blend = BlendState{Color: target.Blend.Color.makeKeyComponent(), Alpha: target.Blend.Alpha.makeKeyComponent()}
		target.UnwrittenChannels &= ColorWriteFlagAll
	}
	if formats.depthStencilFormat == TextureFormatUndefined {
		s.Depth = DepthState{}
		s.Stencil = StencilState{}
		return s
	}
	s.Depth.Compare = spellDefaultZero(s.Depth.Compare, CompareFunctionAlways)
	s.Stencil.Front = s.Stencil.Front.makeKeyFace()
	s.Stencil.Back = s.Stencil.Back.makeKeyFace()
	return s
}

// makeColorTargetStates answers gfx's state of each colour target of a pass
// of formats, its blending and write mask s's for its index.
func (s State) makeColorTargetStates(formats []TextureFormat) []gfx.ColorTargetState {
	if len(formats) == 0 {
		return nil
	}
	targets := make([]gfx.ColorTargetState, len(formats))
	for index, format := range formats {
		target := s.ColorTargets[index]
		targets[index] = gfx.ColorTargetState{
			Format:    gfx.TextureFormat(format),
			Blend:     target.Blend.makeBlendState(),
			WriteMask: gfx.ColorWriteFlags(ColorWriteFlagAll &^ target.UnwrittenChannels),
		}
	}
	return targets
}

// makeDepthStencilState answers gfx's depth and stencil state of a pass whose
// depth-stencil target is of format, none for no format: s's tests, writes
// and bias, each zero value WebGPU's default.
func (s State) makeDepthStencilState(format TextureFormat) gfx.DepthStencilState {
	if format == TextureFormatUndefined {
		return gfx.DepthStencilState{}
	}
	return gfx.DepthStencilState{
		Format:              gfx.TextureFormat(format),
		DepthWriteEnabled:   s.Depth.Write,
		DepthCompare:        s.Depth.Compare.makeCompareFunction(),
		StencilFront:        s.Stencil.Front.makeStencilFaceState(),
		StencilBack:         s.Stencil.Back.makeStencilFaceState(),
		StencilReadMask:     ^s.Stencil.UnreadBits,
		StencilWriteMask:    ^s.Stencil.UnwrittenBits,
		DepthBias:           s.Depth.Bias,
		DepthBiasSlopeScale: s.Depth.BiasSlopeScale,
		DepthBiasClamp:      s.Depth.BiasClamp,
	}
}

// ColorTargetState is how a draw writes one colour target: its blending, a
// zero value blending none, and the channels it leaves unwritten, zero
// writing every one.
type ColorTargetState struct {
	Blend             BlendState
	UnwrittenChannels ColorWriteFlags
}

// BlendState is how a colour target blends what a draw writes into what it
// holds, its colour and its alpha each by a component. A zero BlendState
// blends none; one that is not zero blends, a zero component of it keeping
// WebGPU's defaults: the source's factor one, the destination's zero, and
// adding them.
type BlendState struct {
	Color, Alpha BlendComponent
}

// makeBlendState answers gfx's blending of b: none for a zero b, and each
// component's zero values WebGPU's defaults otherwise.
func (b BlendState) makeBlendState() gfx.BlendState {
	if b == (BlendState{}) {
		return gfx.BlendState{}
	}
	return gfx.BlendState{Color: b.Color.makeBlendComponent(), Alpha: b.Alpha.makeBlendComponent()}
}

// BlendComponent is how one part of a colour, its colour channels or its
// alpha, blends: the source's factor, a zero value meaning one, and the
// destination's, a zero value meaning zero, combined by Operation, a zero
// value adding them.
type BlendComponent struct {
	SourceFactor, DestinationFactor BlendFactor
	Operation                       BlendOperation
}

// makeKeyComponent answers c with each value that is its default spelt zero.
func (c BlendComponent) makeKeyComponent() BlendComponent {
	return BlendComponent{
		SourceFactor:      spellDefaultZero(c.SourceFactor, BlendFactorOne),
		DestinationFactor: spellDefaultZero(c.DestinationFactor, BlendFactorZero),
		Operation:         spellDefaultZero(c.Operation, BlendOperationAdd),
	}
}

// makeBlendComponent answers gfx's blending of c, its zero values WebGPU's
// defaults.
func (c BlendComponent) makeBlendComponent() gfx.BlendComponent {
	return gfx.BlendComponent{
		SrcFactor: cmp.Or(gfx.BlendFactor(c.SourceFactor), gfx.BlendFactorOne),
		DstFactor: cmp.Or(gfx.BlendFactor(c.DestinationFactor), gfx.BlendFactorZero),
		Operation: cmp.Or(gfx.BlendOperation(c.Operation), gfx.BlendOperationAdd),
	}
}

// DepthState is a draw's depth test, Compare a zero value passing always,
// whether it writes the depth of what passes, and its bias, as WebGPU's
// depthBias, depthBiasSlopeScale and depthBiasClamp, zero biasing none.
type DepthState struct {
	Compare        CompareFunction
	Write          bool
	Bias           int32
	BiasSlopeScale float32
	BiasClamp      float32
}

// StencilState is a draw's stencil test: each face's test and operations,
// and the bits left out of what the test reads and of what it writes, zero
// reading and writing every bit.
type StencilState struct {
	Front, Back   StencilFaceState
	UnreadBits    uint32
	UnwrittenBits uint32
}

// StencilFaceState is the stencil test of one face: its comparison, a zero
// value passing always, and what it does to the stencil value when the
// stencil test fails, when the depth test fails, and when both pass, a zero
// operation keeping it.
type StencilFaceState struct {
	Compare                                          CompareFunction
	FailOperation, DepthFailOperation, PassOperation StencilOperation
}

// makeKeyFace answers f with each value that is its default spelt zero.
func (f StencilFaceState) makeKeyFace() StencilFaceState {
	return StencilFaceState{
		Compare:            spellDefaultZero(f.Compare, CompareFunctionAlways),
		FailOperation:      spellDefaultZero(f.FailOperation, StencilOperationKeep),
		DepthFailOperation: spellDefaultZero(f.DepthFailOperation, StencilOperationKeep),
		PassOperation:      spellDefaultZero(f.PassOperation, StencilOperationKeep),
	}
}

// makeStencilFaceState answers gfx's stencil test of f, its zero values
// WebGPU's defaults.
func (f StencilFaceState) makeStencilFaceState() gfx.StencilFaceState {
	return gfx.StencilFaceState{
		Compare:     f.Compare.makeCompareFunction(),
		FailOp:      f.FailOperation.makeStencilOperation(),
		DepthFailOp: f.DepthFailOperation.makeStencilOperation(),
		PassOp:      f.PassOperation.makeStencilOperation(),
	}
}

// BlendFactor scales a colour or alpha in blending, each factor gfx's of the
// same name with its words in full, Src as Source and Dst as Destination. A
// zero factor is its component's default: one for the source, zero for the
// destination.
type BlendFactor uint32

const (
	BlendFactorZero                     = BlendFactor(gfx.BlendFactorZero)
	BlendFactorOne                      = BlendFactor(gfx.BlendFactorOne)
	BlendFactorSource                   = BlendFactor(gfx.BlendFactorSrc)
	BlendFactorOneMinusSource           = BlendFactor(gfx.BlendFactorOneMinusSrc)
	BlendFactorSourceAlpha              = BlendFactor(gfx.BlendFactorSrcAlpha)
	BlendFactorOneMinusSourceAlpha      = BlendFactor(gfx.BlendFactorOneMinusSrcAlpha)
	BlendFactorDestination              = BlendFactor(gfx.BlendFactorDst)
	BlendFactorOneMinusDestination      = BlendFactor(gfx.BlendFactorOneMinusDst)
	BlendFactorDestinationAlpha         = BlendFactor(gfx.BlendFactorDstAlpha)
	BlendFactorOneMinusDestinationAlpha = BlendFactor(gfx.BlendFactorOneMinusDstAlpha)
	BlendFactorSourceAlphaSaturated     = BlendFactor(gfx.BlendFactorSrcAlphaSaturated)
	BlendFactorConstant                 = BlendFactor(gfx.BlendFactorConstant)
	BlendFactorOneMinusConstant         = BlendFactor(gfx.BlendFactorOneMinusConstant)
)

// BlendOperation combines the scaled source and destination in blending,
// each operation gfx's of the same name. A zero operation adds them.
type BlendOperation uint32

const (
	BlendOperationAdd             = BlendOperation(gfx.BlendOperationAdd)
	BlendOperationSubtract        = BlendOperation(gfx.BlendOperationSubtract)
	BlendOperationReverseSubtract = BlendOperation(gfx.BlendOperationReverseSubtract)
	BlendOperationMin             = BlendOperation(gfx.BlendOperationMin)
	BlendOperationMax             = BlendOperation(gfx.BlendOperationMax)
)

// ColorWriteFlags is a set of a colour target's channels, each flag gfx's of
// the same name.
type ColorWriteFlags uint32

const (
	ColorWriteFlagRed   = ColorWriteFlags(gfx.ColorWriteFlagRed)
	ColorWriteFlagGreen = ColorWriteFlags(gfx.ColorWriteFlagGreen)
	ColorWriteFlagBlue  = ColorWriteFlags(gfx.ColorWriteFlagBlue)
	ColorWriteFlagAlpha = ColorWriteFlags(gfx.ColorWriteFlagAlpha)
	ColorWriteFlagAll   = ColorWriteFlags(gfx.ColorWriteFlagAll)
)

// CompareFunction is how a depth or stencil test compares a draw's value
// with the one held, each function gfx's of the same name. A zero function
// passes always.
type CompareFunction uint32

const (
	CompareFunctionNever        = CompareFunction(gfx.CompareFunctionNever)
	CompareFunctionLess         = CompareFunction(gfx.CompareFunctionLess)
	CompareFunctionEqual        = CompareFunction(gfx.CompareFunctionEqual)
	CompareFunctionLessEqual    = CompareFunction(gfx.CompareFunctionLessEqual)
	CompareFunctionGreater      = CompareFunction(gfx.CompareFunctionGreater)
	CompareFunctionNotEqual     = CompareFunction(gfx.CompareFunctionNotEqual)
	CompareFunctionGreaterEqual = CompareFunction(gfx.CompareFunctionGreaterEqual)
	CompareFunctionAlways       = CompareFunction(gfx.CompareFunctionAlways)
)

// makeCompareFunction answers gfx's function of f, passing always for zero.
func (f CompareFunction) makeCompareFunction() gfx.CompareFunction {
	return cmp.Or(gfx.CompareFunction(f), gfx.CompareFunctionAlways)
}

// StencilOperation is what a stencil test does to the stencil value held,
// each operation gfx's of the same name. A zero operation keeps it.
type StencilOperation uint32

const (
	StencilOperationKeep           = StencilOperation(gfx.StencilOperationKeep)
	StencilOperationZero           = StencilOperation(gfx.StencilOperationZero)
	StencilOperationReplace        = StencilOperation(gfx.StencilOperationReplace)
	StencilOperationInvert         = StencilOperation(gfx.StencilOperationInvert)
	StencilOperationIncrementClamp = StencilOperation(gfx.StencilOperationIncrementClamp)
	StencilOperationDecrementClamp = StencilOperation(gfx.StencilOperationDecrementClamp)
	StencilOperationIncrementWrap  = StencilOperation(gfx.StencilOperationIncrementWrap)
	StencilOperationDecrementWrap  = StencilOperation(gfx.StencilOperationDecrementWrap)
)

// makeStencilOperation answers gfx's operation of o, keeping for zero.
func (o StencilOperation) makeStencilOperation() gfx.StencilOperation {
	return cmp.Or(gfx.StencilOperation(o), gfx.StencilOperationKeep)
}

// CullMode is which faces of triangles are culled, each mode gfx's of the
// same name. Its zero value culls none.
type CullMode uint32

const (
	CullModeNone  = CullMode(gfx.CullModeNone)
	CullModeFront = CullMode(gfx.CullModeFront)
	CullModeBack  = CullMode(gfx.CullModeBack)
)

// FrontFace is the winding of a triangle facing the front, each gfx's of the
// same name with its words in full, CCW as CounterClockwise and CW as
// Clockwise. Its zero value is counter-clockwise.
type FrontFace uint32

const (
	FrontFaceCounterClockwise = FrontFace(gfx.FrontFaceCCW)
	FrontFaceClockwise        = FrontFace(gfx.FrontFaceCW)
)

// spellDefaultZero answers value, or zero when it is defaultValue.
func spellDefaultZero[T comparable](value, defaultValue T) T {
	var zero T
	if value == defaultValue {
		return zero
	}
	return value
}
