package canvas

import (
	"math"
	"unsafe"

	"github.com/dvoyni/cog/libs/m"
)

// SDFMaterial is canvas's SDF material, which draws the distance from an
// entry's edge through a Profile, its argument, as a colour: a sprite's read
// from its image, a distance field such as DistanceField bakes, and a
// shape's from its rectangle and radius, its quad grown by the profile's
// reach. It draws the band alone, so a halo is two entries: the field drawn
// with it, then the sprite over it. The band's colour is the profile's
// times the entry's tint, white for a sprite's zero tint and transparent
// for a shape's, so a shape drawn with it gives a tint, m.White to draw the
// profile's colour as it is. Its zero Profile draws nothing. canvas holds
// it, so it is never released and its parameters are canvas's.
var SDFMaterial = Material{index: 1}

// Profile is the SDF material's argument: a band of the distance from an
// entry's edge, positive outside it, and the colour it draws. Its distances
// are in field texels: a sprite's distance field's own texels, or a shape's
// own space, the space of its Rect and Width.
type Profile struct {
	// Inner is how far inside the edge the band reaches, and Outer how far
	// outside it.
	Inner, Outer float32
	// Plateau is how far outside the edge the band stays solid, falling off
	// beyond it to Outer along (1 - t) to the power Falloff, t running from
	// 0 at Plateau to 1 at Outer. A Falloff of zero is a hard edge at
	// Outer.
	Plateau, Falloff float32
	// Color is linear and premultiplied: one of zero alpha adds to what the
	// target holds, as a glow does.
	Color m.Color
	// Offset moves the band in world space, the space an entry's transform
	// maps into, so every entry's falls the same way, mirrored or not. A
	// sprite's band moves within its field, which holds only its spread
	// beyond the art.
	Offset m.Vec2
	// Spread is the Spread a sprite's DistanceField was baked with, zero
	// meaning 8, as DistanceField's does. A shape ignores it.
	Spread float32
	_      float32
}

// insideAll is the Inner of a profile reaching all the way inside the edge,
// so its band lies under the whole entry, which drawn over it leaves no seam
// at its anti-aliased edge.
const insideAll = math.MaxFloat32

// Outline answers a profile of a hard band width wide outside the edge, in
// color, lying under the whole entry.
func Outline(width float32, color m.Color) Profile {
	return Profile{Inner: insideAll, Outer: width, Plateau: width, Color: color.Premultiply()}
}

// Halo answers a profile solid out to plateau outside the edge, then falling
// off to reach, in color, lying under the whole entry.
func Halo(plateau, reach float32, color m.Color) Profile {
	return Profile{Inner: insideAll, Outer: reach, Plateau: plateau, Falloff: 2, Color: color.Premultiply()}
}

// Glow answers a profile falling off from the edge to reach outside it, in
// color, added to what the target holds, lying under the whole entry.
func Glow(reach float32, color m.Color) Profile {
	glow := color.Premultiply()
	glow.A = 0
	return Profile{Inner: insideAll, Outer: reach, Falloff: 1, Color: glow}
}

// DropShadow answers a profile of the entry's shape in color, offset in world
// space, its edge falling off across softness centred on the entry's edge.
func DropShadow(offset m.Vec2, softness float32, color m.Color) Profile {
	return Profile{Inner: insideAll, Outer: softness / 2, Plateau: -softness / 2, Falloff: 1, Color: color.Premultiply(), Offset: offset}
}

// profileFromBytes answers the Profile of argument, the bytes an entry gives
// SDFMaterial, the zero Profile where it gives none.
func profileFromBytes(argument []byte) Profile {
	var profile Profile
	copy(castToBytes(unsafe.Slice(&profile, 1)), argument)
	return profile
}

// sdfMaterialDescriptor is what canvas makes SDFMaterial of.
var sdfMaterialDescriptor = MaterialDescriptor{
	Label:              "canvas SDF",
	Shader:             sdfShaderPath,
	VertexEntryPoint:   sdfVertexEntryPoint,
	FragmentEntryPoint: sdfFragmentEntryPoint,
	Argument:           ArgumentOf[Profile](),
}
