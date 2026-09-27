package types

type TargetKind uint8

// The screen is the zero value: a recorder that defaults a pass leaves Target
// alone, and the screen is what it means by that. A colourless pass has to say
// so explicitly, because "no colour attachment" is a deliberate choice a
// depth-only prepass makes, never an omission.
const (
	TargetScreen TargetKind = iota
	TargetNone
	TargetTexture
)

// TargetDescr names a pass's colour attachment. Texture, Mip and Layer say
// where a TargetTexture renders; Width and Height are its texel dimensions,
// which a recorder resolving a projection reads the aspect from. The screen
// has none: its swapchain view is per-frame and sized on the render thread.
type TargetDescr struct {
	Kind          TargetKind
	Texture       TextureID
	Mip, Layer    int
	Width, Height int
}

// TargetDescrScreen is the frame's screen attachment. It stays a sentinel the
// recorder cannot resolve: the swapchain view is per-frame and known only on
// the render thread.
func TargetDescrScreen() TargetDescr { return TargetDescr{Kind: TargetScreen} }

// TargetDescrTexture renders into one mip level of one layer of a texture, which
// must have been allocated Renderable.
func TargetDescrTexture(texture TextureDescr, mip, layer int) TargetDescr {
	return TargetDescr{
		Kind: TargetTexture, Texture: texture.Params.ID, Mip: mip, Layer: layer,
		Width: texture.Params.Width, Height: texture.Params.Height,
	}
}

// TargetDescrNone declares a pass with no colour attachment, such as a depth-only
// prepass.
func TargetDescrNone() TargetDescr { return TargetDescr{Kind: TargetNone} }

// EqualsTo compares where a target points, not how big it is. The dimensions ride
// along only so a recorder can read the aspect back out; two descriptors naming
// one texture are one attachment whatever they claim about its size.
func (t TargetDescr) EqualsTo(other TargetDescr) bool {
	return t.Kind == other.Kind && t.Texture == other.Texture &&
		t.Mip == other.Mip && t.Layer == other.Layer
}
