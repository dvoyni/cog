package internal

import "github.com/dvoyni/cog/libs/m"

// ListenerParams is "become this" to SetListener: an absent field is unchanged,
// the same rule Params follows, so a game that only ever moves its Listener
// never restates a rotation it did not choose.
type ListenerParams struct {
	// Position is where the Listener stands, in the game's own units.
	Position m.Maybe[m.Vec3]
	// Orientation is which way it faces. The axes are right-handed, forward
	// -Z, up +Y, right +X, so an unrotated Listener is the W3C default
	// Listener and a speaker and a spotlight on one Transform point the same
	// way.
	//
	// A 2D game must rotate its Listener - QuatRotationX(-Pi/2) - and leaving
	// it out is not a degraded pan but a broken one: panning projects the up
	// axis away before taking a bearing, the Z=0 plane contains +Y, so every
	// source with positive X would be hard right whatever its Y.
	Orientation m.Maybe[m.Quat]
}

// merge returns p with every field next states replacing p's own, which is what
// "coalesced within a tick, last value wins" means when two Systems each set
// half of the Listener.
func (p ListenerParams) merge(next ListenerParams) ListenerParams {
	if value, ok := next.Position.Get(); ok {
		p.Position = m.Some(value)
	}
	if value, ok := next.Orientation.Get(); ok {
		p.Orientation = m.Some(value)
	}
	return p
}

// Listener is where the game is heard from, as of the last flush. There is one
// per Engine, and it sits at the origin with no rotation before any call, so a
// game that never says where it listens from still hears its Positional Voices.
//
// It is a resource of its own for the reason the resources are several rather
// than one: a System asking where the Listener is never contends with the
// Systems recording operations, and it is asked of the resources rather than of
// the Queue it is recorded on. Buses got the same treatment for the same
// reason, one question earlier.
//
// It is read-only to a caller, like the other views: the Listener is moved by
// recording SetListener on the Queue, and the flush applies it.
//
// sound never reads a camera. A game, or ecsaudio, copies a camera's Transform
// across; it is one value copy, since m.Transform carries Position and Rotation
// and Scale means nothing here.
//
// Access it only while a handler holds its declared resource lock.
type Listener struct {
	position    m.Vec3
	orientation m.Quat
	// front and up are the orientation resolved to axes, recomputed when it
	// moves rather than once per Voice per tick. They are what the W3C azimuth
	// algorithm is handed, which is why they are kept as a pair of vectors
	// rather than a matrix.
	front, up m.Vec3
}

// NewListener builds the Listener at the origin, unrotated: facing -Z with +Y
// up, which is the W3C default Listener exactly.
func NewListener() *Listener {
	listener := &Listener{orientation: m.Quat{W: 1}}
	listener.reface()
	return listener
}

// Position reports where the Listener stands, so a game reads back what it set.
//
// It answers as of the last flush: a SetListener recorded in this tick is
// visible in the next one, which is true of every operation a game records.
func (l *Listener) Position() m.Vec3 { return l.position }

// Orientation reports which way the Listener faces, as of the last flush.
func (l *Listener) Orientation() m.Quat { return l.orientation }

// apply installs the tick's coalesced Listener, last value having won at the
// moment each field was recorded.
func (l *Listener) apply(set *ListenerParams) {
	if position, ok := set.Position.Get(); ok {
		l.position = position
	}
	if orientation, ok := set.Orientation.Get(); ok && orientation != l.orientation {
		l.orientation = orientation
		l.reface()
	}
}

// reface resolves the orientation to the forward and up axes the equations
// read.
//
// The zero Quat is (0,0,0,0), which is not a rotation at all, so it is read as
// the identity - m.Transform.rotation's rule, and the reason a game that
// declares a ListenerParams and fills in only its Position is not left facing
// nowhere.
func (l *Listener) reface() {
	rotation := l.orientation
	if rotation == (m.Quat{}) {
		rotation = m.Quat{W: 1}
	}
	l.front = rotation.Rotate(m.Vec3{Z: -1})
	l.up = rotation.Rotate(m.Vec3{Y: 1})
}
