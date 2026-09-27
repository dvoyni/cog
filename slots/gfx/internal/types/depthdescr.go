package types

// DepthDescr names a pass's depth attachment. Texture, Width and Height belong
// to a DepthKindTexture; DepthDescrAuto has no size of its own and takes the colour
// target's.
type DepthDescr struct {
	Kind          DepthKind
	Texture       TextureID
	Width, Height int
}

// DepthDescrAuto uses the backend's own depth texture for the target's size. Every
// DepthDescrAuto pass at a given size shares one texture, so a pass that means to
// start from a clean depth buffer must clear depth or it inherits whatever the
// previous pass at that size left behind.
func DepthDescrAuto() DepthDescr { return DepthDescr{Kind: DepthKindAuto} }

// DepthDescrNone declares a pass with no depth attachment.
func DepthDescrNone() DepthDescr { return DepthDescr{Kind: DepthKindNone} }

// DepthDescrTarget renders depth into a texture, which must be FormatDepth32F and
// Renderable.
func DepthDescrTarget(texture TextureDescr) DepthDescr {
	return DepthDescr{
		Kind: DepthKindTexture, Texture: texture.Params.ID,
		Width: texture.Params.Width, Height: texture.Params.Height,
	}
}

// EqualsTo compares where a depth attachment points, not how big it is, for
// the reason TargetDescr.EqualsTo gives.
func (d DepthDescr) EqualsTo(other DepthDescr) bool {
	return d.Kind == other.Kind && d.Texture == other.Texture
}
