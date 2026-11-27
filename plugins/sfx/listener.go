package sfx

import "github.com/dvoyni/cog/libs/m"

// listener is what Listener names: where the game is heard from.
type listener struct {
	position    m.Vec3
	orientation m.Quat
}

// makeListener answers the listener at the origin, unrotated: facing -Z with
// +Y up, W3C's default listener.
func makeListener() listener {
	return listener{orientation: m.Quat{W: 1}}
}

// Position answers where the listener stands, as of the last flush: a
// SetListener recorded before a flush is visible after it.
func (l listener) Position() m.Vec3 { return l.position }

// Orientation answers which way the listener faces, as of the last flush.
func (l listener) Orientation() m.Quat { return l.orientation }

// apply moves the listener to every field options states.
func (l *listener) apply(options ListenerOptions) {
	l.position = options.Position.Or(l.position)
	l.orientation = options.Orientation.Or(l.orientation)
}

// resolveAxes answers the forward and up the listener's orientation turns
// -Z and +Y to, which the equations read. The zero m.Quat is no rotation at
// all, so it is read as the identity, and a listener given a zero
// orientation is not left facing nowhere.
func (l listener) resolveAxes() (front, up m.Vec3) {
	rotation := l.orientation
	if rotation == (m.Quat{}) {
		rotation = m.Quat{W: 1}
	}
	return rotation.Rotate(m.Vec3{Z: -1}), rotation.Rotate(m.Vec3{Y: 1})
}
