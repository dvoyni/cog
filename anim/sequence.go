package anim

import "github.com/dvoyni/cog/m"

// Sequence produces a value of type T for eased progress in [0, 1]. Any type
// may implement it; a track stores the sequence value it was added with, so a
// sequence may carry whatever payload the reader needs at draw time.
type Sequence[T any] interface {
	At(progress float32) T
}

// Lerp is a Sequence that mixes between two values. Embed it in a named struct
// to give a track its own slot type and payload fields:
//
//	type MoveSeq struct {
//		anim.Lerp[float32]
//		From, To TileId
//	}
//
//	tl.Add(unitId, MoveSeq{Lerp: anim.LerpFloat(0, 1), From: a, To: b}, anim.Over(0.4))
type Lerp[T any] struct {
	From T
	To   T
	// Mix returns the value the given amount of the way from from to to. It
	// must be set; the Lerp* constructors supply the m package's mixes.
	Mix func(from T, to T, amount float32) T
}

// At mixes From and To by progress.
func (l Lerp[T]) At(progress float32) T {
	return l.Mix(l.From, l.To, progress)
}

// LerpFloat mixes two scalars linearly.
func LerpFloat(from float32, to float32) Lerp[float32] {
	return Lerp[float32]{From: from, To: to, Mix: m.Lerp}
}

// LerpAngle mixes two radian angles along the shortest arc.
func LerpAngle(from float32, to float32) Lerp[float32] {
	return Lerp[float32]{From: from, To: to, Mix: m.LerpAngle}
}

// LerpVec2 mixes two vectors componentwise.
func LerpVec2(from m.Vec2, to m.Vec2) Lerp[m.Vec2] {
	return Lerp[m.Vec2]{From: from, To: to, Mix: m.Vec2.Lerp}
}

// LerpVec3 mixes two vectors componentwise.
func LerpVec3(from m.Vec3, to m.Vec3) Lerp[m.Vec3] {
	return Lerp[m.Vec3]{From: from, To: to, Mix: m.Vec3.Lerp}
}

// LerpColor mixes two colors componentwise.
func LerpColor(from m.Color, to m.Color) Lerp[m.Color] {
	return Lerp[m.Color]{From: from, To: to, Mix: m.Color.Lerp}
}
