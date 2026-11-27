package sfx

import "github.com/dvoyni/cog/libs/m"

// ListenerOptions is "become this" to SetListener: an absent field is
// unchanged, so a job that moves the listener and one that turns it do not
// overwrite each other, and a 2D game sets its rotation once and only moves
// after.
type ListenerOptions struct {
	// Position is where the listener stands, in the game's units.
	Position m.Maybe[m.Vec3]
	// Orientation is which way the listener faces. The axes are right-handed,
	// forward -Z, up +Y and right +X, so an unrotated listener is W3C's
	// default listener. The zero m.Quat is read as no rotation.
	//
	// A 2D game, Y down, rotates its listener once, with
	// m.QuatRotationX(-math.Pi / 2): forward is then up the screen, up out of
	// it and right screen-right. Leaving it out is not a degraded pan but a
	// broken one: panning projects the listener's up away before taking a
	// bearing, the Z=0 plane contains +Y, and every sprite with positive X is
	// hard right whatever its Y.
	Orientation m.Maybe[m.Quat]
}

// merge answers o with every field next states replacing o's own.
func (o ListenerOptions) merge(next ListenerOptions) ListenerOptions {
	if next.Position.Present() {
		o.Position = next.Position
	}
	if next.Orientation.Present() {
		o.Orientation = next.Orientation
	}
	return o
}
