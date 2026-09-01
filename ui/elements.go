package ui

import (
	"github.com/dvoyni/cog/canvas"
	"github.com/dvoyni/cog/gfx"
	"github.com/dvoyni/cog/m"
)

type SpriteFit uint8

const (
	SpriteStretch SpriteFit = iota
	SpriteContain
	SpriteCover
)

type SpriteParams struct {
	Path     string
	Scale    float32
	Tint     m.Color
	Filter   gfx.FilterMode
	Fit      SpriteFit
	Frame    canvas.SpriteFrame
	Rotation float32
}

type VisualStates[T any] map[VisualState]T

type InteractiveSpriteParams struct {
	Default SpriteParams
	Path    VisualStates[string]
	Tint    VisualStates[m.Color]
}

type Sprite9SlicedParams struct {
	Path     string
	Insets   canvas.SpriteFrame
	Scale    float32
	Tint     m.Color
	Filter   gfx.FilterMode
	NoCenter bool
}

type InteractiveSprite9SlicedParams struct {
	Default Sprite9SlicedParams
	Path    VisualStates[string]
	Tint    VisualStates[m.Color]
}

type Insets struct {
	Top, Right, Bottom, Left float32
}

type Sprite9SliceImages struct {
	Center                   string
	Top, Right, Bottom, Left string
	TopLeft, TopRight        string
	BottomRight, BottomLeft  string
}

type Sprite9SliceTiledParams struct {
	Images Sprite9SliceImages
	Insets Insets
	Scale  float32
	Tint   m.Color
	Filter gfx.FilterMode
}

type InteractiveSprite9SliceTiledParams struct {
	Default Sprite9SliceTiledParams
	Images  VisualStates[Sprite9SliceImages]
	Tint    VisualStates[m.Color]
}

type TextAlignment uint8

const (
	TextAlignStart TextAlignment = iota
	TextAlignCenter
	TextAlignEnd
)

// Font identifies a font face for UI text: a resource path and a logical pixel
// size. Text is measured and drawn through the canvas Lookup, which parses inline
// ${path} icons in every UI string.
type Font struct {
	Path string
	Size int
}

type TextParams struct {
	Font         Font
	Text         string
	Color        m.Color
	Alignment    TextAlignment
	WordWrapping bool
	WrapWidth    float32
}

type InteractiveTextParams struct {
	Default TextParams
	Colors  VisualStates[m.Color]
}

type ColorParams struct {
	Color m.Color
}

type InteractiveColorParams struct {
	Default m.Color
	Colors  VisualStates[m.Color]
}

type spriteVisual struct{}
type sprite9SlicedVisual struct{}
type sprite9SliceTiledVisual struct{}
type textVisual struct{}
type colorVisual struct{}
type interactiveSpriteVisual struct{}
type interactiveSprite9SlicedVisual struct{}
type interactiveSprite9SliceTiledVisual struct{}
type interactiveTextVisual struct{}
type interactiveColorVisual struct{}

var (
	sharedSpriteVisual                       spriteVisual
	sharedSprite9SlicedVisual                sprite9SlicedVisual
	sharedSprite9SliceTiledVisual            sprite9SliceTiledVisual
	sharedTextVisual                         textVisual
	sharedColorVisual                        colorVisual
	sharedInteractiveSpriteVisual            interactiveSpriteVisual
	sharedInteractiveSprite9SlicedVisual     interactiveSprite9SlicedVisual
	sharedInteractiveSprite9SliceTiledVisual interactiveSprite9SliceTiledVisual
	sharedInteractiveTextVisual              interactiveTextVisual
	sharedInteractiveColorVisual             interactiveColorVisual
)

type packedVisualStates[T any] [16]opt[T]

type interactiveSpritePayload struct {
	defaultValue SpriteParams
	path         packedVisualStates[string]
	tint         packedVisualStates[m.Color]
}

type interactiveSprite9SlicedPayload struct {
	defaultValue Sprite9SlicedParams
	path         packedVisualStates[string]
	tint         packedVisualStates[m.Color]
}

type interactiveSprite9SliceTiledPayload struct {
	defaultValue Sprite9SliceTiledParams
	images       packedVisualStates[Sprite9SliceImages]
	tint         packedVisualStates[m.Color]
}

type interactiveTextPayload struct {
	defaultValue TextParams
	colors       packedVisualStates[m.Color]
}

type interactiveColorPayload struct {
	defaultValue m.Color
	colors       packedVisualStates[m.Color]
}

func Sprite(params SpriteParams) (ParamVisual[SpriteParams], SpriteParams) {
	return sharedSpriteVisual, params
}

func Image(params SpriteParams) Element {
	return NewElement().Visual(Sprite(params)).PreserveAspectRatio()
}

func InteractiveSprite(params InteractiveSpriteParams) (ParamVisual[interactiveSpritePayload], interactiveSpritePayload) {
	return sharedInteractiveSpriteVisual, interactiveSpritePayload{
		defaultValue: params.Default,
		path:         packVisualStates(params.Path),
		tint:         packVisualStates(params.Tint),
	}
}

func InteractiveImage(params InteractiveSpriteParams) Element {
	return NewElement().Visual(InteractiveSprite(params)).PreserveAspectRatio()
}

func (interactiveSpriteVisual) DefaultSize(lookup canvas.LookupAccess, params interactiveSpritePayload) m.Vec2 {
	return sharedSpriteVisual.DefaultSize(lookup, params.defaultValue)
}

func (interactiveSpriteVisual) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params interactiveSpritePayload) {
	value := params.defaultValue
	value.Path = params.path.value(state.VisualState, value.Path)
	value.Tint = params.tint.value(state.VisualState, value.Tint)
	sharedSpriteVisual.Draw(lookup, queue, state, value)
}

func (spriteVisual) DefaultSize(lookup canvas.LookupAccess, params SpriteParams) m.Vec2 {
	scale := defaultScale(params.Scale)
	size := lookup.SpriteSize(params.Path)
	return m.Vec2{X: size.X * scale, Y: size.Y * scale}
}

func (spriteVisual) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params SpriteParams) {
	if queue == nil {
		return
	}
	bounds := state.Rect
	frame := params.Frame
	intrinsic := lookup.SpriteSize(params.Path)
	source := intrinsic
	if source.X > 0 && source.Y > 0 {
		switch params.Fit {
		case SpriteContain:
			bounds = containRect(bounds, source)
		case SpriteCover:
			frame = coverFrame(frame, source, bounds)
		}
	}
	queue.Sprite(state.Layer, params.Path, canvas.SpriteTransform{
		Position: m.Vec2{
			X: bounds.X + bounds.Width*0.5,
			Y: bounds.Y + bounds.Height*0.5,
		},
		Size:     m.Vec2{X: bounds.Width, Y: bounds.Height},
		Rotation: params.Rotation, Origin: m.Vec2{X: 0.5, Y: 0.5}, Frame: frame, Filter: params.Filter,
	}, nil, gfx.ColorParam("tint", defaultTint(params.Tint)))
}

func Sprite9Sliced(params Sprite9SlicedParams) (ParamVisual[Sprite9SlicedParams], Sprite9SlicedParams) {
	return sharedSprite9SlicedVisual, params
}

func Image9Sliced(params Sprite9SlicedParams) Element {
	return NewElement().Visual(Sprite9Sliced(params))
}

func InteractiveSprite9Sliced(params InteractiveSprite9SlicedParams) (ParamVisual[interactiveSprite9SlicedPayload], interactiveSprite9SlicedPayload) {
	return sharedInteractiveSprite9SlicedVisual, interactiveSprite9SlicedPayload{
		defaultValue: params.Default,
		path:         packVisualStates(params.Path),
		tint:         packVisualStates(params.Tint),
	}
}

func InteractiveImage9Sliced(params InteractiveSprite9SlicedParams) Element {
	return NewElement().Visual(InteractiveSprite9Sliced(params))
}

func (interactiveSprite9SlicedVisual) DefaultSize(lookup canvas.LookupAccess, params interactiveSprite9SlicedPayload) m.Vec2 {
	return sharedSprite9SlicedVisual.DefaultSize(lookup, params.defaultValue)
}

func (interactiveSprite9SlicedVisual) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params interactiveSprite9SlicedPayload) {
	value := params.defaultValue
	value.Path = params.path.value(state.VisualState, value.Path)
	value.Tint = params.tint.value(state.VisualState, value.Tint)
	sharedSprite9SlicedVisual.Draw(lookup, queue, state, value)
}

func (sprite9SlicedVisual) DefaultSize(_ canvas.LookupAccess, params Sprite9SlicedParams) m.Vec2 {
	scale := defaultScale(params.Scale)
	return m.Vec2{
		X: float32(params.Insets.Left+params.Insets.Right) * scale,
		Y: float32(params.Insets.Top+params.Insets.Bottom) * scale,
	}
}

func (sprite9SlicedVisual) Draw(_ canvas.LookupAccess, queue *canvas.OpQueue, state State, params Sprite9SlicedParams) {
	if queue == nil {
		return
	}
	bounds := state.Rect
	queue.Sprite(state.Layer, params.Path, canvas.SpriteTransform{
		Position:  m.Vec2{X: bounds.X, Y: bounds.Y},
		Size:      m.Vec2{X: bounds.Width, Y: bounds.Height},
		NineSlice: params.Insets, NineSliceScale: defaultScale(params.Scale), NineSliceNoCenter: params.NoCenter,
		Filter: params.Filter,
	}, nil, gfx.ColorParam("tint", defaultTint(params.Tint)))
}

func Sprite9SliceTiled(params Sprite9SliceTiledParams) (ParamVisual[Sprite9SliceTiledParams], Sprite9SliceTiledParams) {
	return sharedSprite9SliceTiledVisual, params
}

func Image9SliceTiled(params Sprite9SliceTiledParams) Element {
	return NewElement().Visual(Sprite9SliceTiled(params))
}

func InteractiveSprite9SliceTiled(params InteractiveSprite9SliceTiledParams) (ParamVisual[interactiveSprite9SliceTiledPayload], interactiveSprite9SliceTiledPayload) {
	return sharedInteractiveSprite9SliceTiledVisual, interactiveSprite9SliceTiledPayload{
		defaultValue: params.Default,
		images:       packVisualStates(params.Images),
		tint:         packVisualStates(params.Tint),
	}
}

func InteractiveImage9SliceTiled(params InteractiveSprite9SliceTiledParams) Element {
	return NewElement().Visual(InteractiveSprite9SliceTiled(params))
}

func (interactiveSprite9SliceTiledVisual) DefaultSize(lookup canvas.LookupAccess, params interactiveSprite9SliceTiledPayload) m.Vec2 {
	return sharedSprite9SliceTiledVisual.DefaultSize(lookup, params.defaultValue)
}

func (interactiveSprite9SliceTiledVisual) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params interactiveSprite9SliceTiledPayload) {
	value := params.defaultValue
	value.Images = params.images.value(state.VisualState, value.Images)
	value.Tint = params.tint.value(state.VisualState, value.Tint)
	sharedSprite9SliceTiledVisual.Draw(lookup, queue, state, value)
}

func (sprite9SliceTiledVisual) DefaultSize(lookup canvas.LookupAccess, params Sprite9SliceTiledParams) m.Vec2 {
	scale := defaultScale(params.Scale)
	insets := resolvedSprite9SliceTiledInsets(lookup, params)
	return m.Vec2{
		X: (insets.Left + insets.Right) * scale,
		Y: (insets.Top + insets.Bottom) * scale,
	}
}

func (sprite9SliceTiledVisual) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params Sprite9SliceTiledParams) {
	if queue == nil {
		return
	}
	bounds := state.Rect
	scale := defaultScale(params.Scale)
	insets := resolvedSprite9SliceTiledInsets(lookup, params)
	x := splitAxis(bounds.Width, insets.Left*scale, insets.Right*scale)
	y := splitAxis(bounds.Height, insets.Top*scale, insets.Bottom*scale)
	paths := [3][3]string{
		{params.Images.TopLeft, params.Images.Top, params.Images.TopRight},
		{params.Images.Left, params.Images.Center, params.Images.Right},
		{params.Images.BottomLeft, params.Images.Bottom, params.Images.BottomRight},
	}
	for row := 0; row < 3; row++ {
		for column := 0; column < 3; column++ {
			path := paths[row][column]
			if row == 1 && column == 1 && path == "" {
				continue
			}
			width := x[column+1] - x[column]
			height := y[row+1] - y[row]
			if width <= 0 || height <= 0 {
				continue
			}
			queue.Sprite(state.Layer, path, canvas.SpriteTransform{
				Position: m.Vec2{X: bounds.X + x[column], Y: bounds.Y + y[row]},
				Size:     m.Vec2{X: width, Y: height},
				Scale:    scale, TileX: column == 1, TileY: row == 1, Filter: params.Filter,
			}, nil, gfx.ColorParam("tint", defaultTint(params.Tint)))
		}
	}
}

func resolvedSprite9SliceTiledInsets(lookup canvas.LookupAccess, params Sprite9SliceTiledParams) Insets {
	topLeft := m.Vec2{}
	if (params.Insets.Top == 0 || params.Insets.Left == 0) && params.Images.TopLeft != "" {
		topLeft = lookup.SpriteSize(params.Images.TopLeft)
	}
	bottomRight := m.Vec2{}
	if (params.Insets.Right == 0 || params.Insets.Bottom == 0) && params.Images.BottomRight != "" {
		bottomRight = lookup.SpriteSize(params.Images.BottomRight)
	}
	return Insets{
		Top:    resolvedInset(params.Insets.Top, topLeft.Y),
		Right:  resolvedInset(params.Insets.Right, bottomRight.X),
		Bottom: resolvedInset(params.Insets.Bottom, bottomRight.Y),
		Left:   resolvedInset(params.Insets.Left, topLeft.X),
	}
}

func resolvedInset(value, calculated float32) float32 {
	if value < 0 {
		return 0
	}
	if value == 0 {
		return calculated
	}
	return value
}

func Text(params TextParams) (ParamVisual[TextParams], TextParams) {
	return sharedTextVisual, params
}

func Label(params TextParams) Element {
	return NewElement().Visual(Text(params))
}

func InteractiveText(params InteractiveTextParams) (ParamVisual[interactiveTextPayload], interactiveTextPayload) {
	return sharedInteractiveTextVisual, interactiveTextPayload{
		defaultValue: params.Default,
		colors:       packVisualStates(params.Colors),
	}
}

func InteractiveLabel(params InteractiveTextParams) Element {
	return NewElement().Visual(InteractiveText(params))
}

func (interactiveTextVisual) DefaultSize(lookup canvas.LookupAccess, params interactiveTextPayload) m.Vec2 {
	return sharedTextVisual.DefaultSize(lookup, params.defaultValue)
}

func (interactiveTextVisual) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params interactiveTextPayload) {
	value := params.defaultValue
	value.Color = params.colors.value(state.VisualState, value.Color)
	sharedTextVisual.Draw(lookup, queue, state, value)
}

func Color(params ColorParams) (ParamVisual[ColorParams], ColorParams) {
	return sharedColorVisual, params
}

func ColorPanel(params ColorParams) Element {
	return NewElement().Visual(Color(params))
}

func InteractiveColor(params InteractiveColorParams) (ParamVisual[interactiveColorPayload], interactiveColorPayload) {
	return sharedInteractiveColorVisual, interactiveColorPayload{
		defaultValue: params.Default,
		colors:       packVisualStates(params.Colors),
	}
}

func InteractiveColorPanel(params InteractiveColorParams) Element {
	return NewElement().Visual(InteractiveColor(params))
}

func (interactiveColorVisual) DefaultSize(canvas.LookupAccess, interactiveColorPayload) m.Vec2 {
	return m.Vec2{}
}

func (interactiveColorVisual) Draw(_ canvas.LookupAccess, queue *canvas.OpQueue, state State, params interactiveColorPayload) {
	if queue == nil {
		return
	}
	queue.FillRect(state.Layer, state.Rect, params.colors.value(state.VisualState, params.defaultValue))
}

func (colorVisual) DefaultSize(canvas.LookupAccess, ColorParams) m.Vec2 { return m.Vec2{} }

func (colorVisual) Draw(_ canvas.LookupAccess, queue *canvas.OpQueue, state State, params ColorParams) {
	if queue == nil {
		return
	}
	queue.FillRect(state.Layer, state.Rect, params.Color)
}

func (textVisual) DefaultSize(lookup canvas.LookupAccess, params TextParams) m.Vec2 {
	if params.Font.Path == "" || params.Font.Size <= 0 {
		return m.Vec2{}
	}
	size := lookup.MeasureTextSize(params.Font.Path, params.Font.Size, params.Text)
	if params.WordWrapping && params.WrapWidth > 0 {
		size = lookup.MeasureWrappedTextSize(params.Font.Path, params.Font.Size, params.Text, params.WrapWidth)
	}
	return size
}

func (textVisual) Draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, state State, params TextParams) {
	if queue == nil || params.Font.Path == "" || params.Font.Size <= 0 {
		return
	}
	bounds := state.Rect
	measured := lookup.MeasureTextSize(params.Font.Path, params.Font.Size, params.Text)
	if params.WordWrapping && bounds.Width > 0 {
		measured = lookup.MeasureWrappedTextSize(params.Font.Path, params.Font.Size, params.Text, bounds.Width)
	}
	position := m.Vec2{X: bounds.X, Y: bounds.Y + (bounds.Height-measured.Y)/2}
	align := canvas.AlignLeft
	switch params.Alignment {
	case TextAlignCenter:
		position.X = bounds.X + bounds.Width/2
		align = canvas.AlignCenter
	case TextAlignEnd:
		position.X = bounds.X + bounds.Width
		align = canvas.AlignRight
	}
	queue.Text(state.Layer, params.Font.Path, params.Text, canvas.TextDraw{
		Position: position, Size: float32(params.Font.Size), Color: params.Color, Align: align,
		WordWrapping: params.WordWrapping, WrapWidth: bounds.Width,
	})
}

func packVisualStates[T any](values VisualStates[T]) packedVisualStates[T] {
	var packed packedVisualStates[T]
	for mask, value := range values {
		for index := range packed {
			if mask&(VisualState(1)<<index) != 0 {
				packed[index] = opt[T]{v: value, set: true}
			}
		}
	}
	return packed
}

func (values *packedVisualStates[T]) value(state VisualState, fallback T) T {
	for index := len(values) - 1; index >= 0; index-- {
		if state&(VisualState(1)<<index) != 0 && values[index].set {
			return values[index].v
		}
	}
	return fallback
}

func defaultScale(scale float32) float32 {
	if scale == 0 {
		return 1
	}
	return scale
}

func defaultTint(color m.Color) m.Color {
	if color == (m.Color{}) {
		return m.Color{R: 1, G: 1, B: 1, A: 1}
	}
	return color
}

func splitAxis(length, leading, trailing float32) [4]float32 {
	leading = min(max(leading, 0), length)
	trailing = min(max(trailing, 0), max(length-leading, 0))
	return [4]float32{0, leading, length - trailing, length}
}

func containRect(bounds Rect, source m.Vec2) Rect {
	if bounds.Width <= 0 || bounds.Height <= 0 || source.X <= 0 || source.Y <= 0 {
		return bounds
	}
	scale := min(bounds.Width/source.X, bounds.Height/source.Y)
	width, height := source.X*scale, source.Y*scale
	return Rect{
		X: bounds.X + (bounds.Width-width)/2, Y: bounds.Y + (bounds.Height-height)/2,
		Width: width, Height: height,
	}
}

func coverFrame(frame canvas.SpriteFrame, source m.Vec2, bounds Rect) canvas.SpriteFrame {
	width := source.X - float32(frame.Left+frame.Right)
	height := source.Y - float32(frame.Top+frame.Bottom)
	if width <= 0 || height <= 0 || bounds.Width <= 0 || bounds.Height <= 0 {
		return frame
	}
	sourceRatio := width / height
	targetRatio := bounds.Width / bounds.Height
	if sourceRatio > targetRatio {
		crop := int((width - height*targetRatio) / 2)
		frame.Left += crop
		frame.Right += crop
	} else if sourceRatio < targetRatio {
		crop := int((height - width/targetRatio) / 2)
		frame.Top += crop
		frame.Bottom += crop
	}
	return frame
}
