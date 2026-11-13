// Package mux keeps a game's true state apart from what is shown of it: the
// model jumps to each new state at once, and timed overrides make what is
// drawn catch up.
//
// A model is a plain struct of the game's data, registered with RegisterModel
// from the Init of a plugin listed after mux's. Each registered model is
// installed as the resource *M, beside its Timeline[M, D] and its
// Tasks[M, D]. D, an [N]int64 the game declares, is the model's depth:
//
//	type GameDepth [12]int64
//
//	mux.RegisterModel[Game, GameDepth](configurator)
//
// A Path names one property of a model through its struct fields, compiled
// once by MakePath, by reflection. Get reads the model's own value through
// it, allocating nothing:
//
//	position := mux.MakePath[Game, Vec2](configurator, "Hero.Position")
//	value, ok := position.Get(model.Get())
//
// A view reads the timeline under kernel.Read[mux.Timeline[M, D]], and a job
// holding kernel.Write[mux.Timeline[M, D]] reaches its writing side through
// ElevateTimeline. An update changes the model at once, and tweens the
// property it changed from the value shown to the model's new one:
//
//	zoom := mux.MakePath[Game, float32](configurator, "Camera.Zoom")
//
//	model.Get().Camera.Zoom = 2
//	writeTimeline := mux.ElevateTimeline(timeline)
//	writeTimeline.Tween(zoom, writeTimeline.Now(), 1, 2, time.Second, mux.EasingLinear)
//
// A view reads each property's current value: the active tween's, or the
// model's own when none is:
//
//	shown, ok := timeline.Get().Current(zoom, model.Get())
//
// A tween eases by an Easing, a curve named by its number: EasingLinear or
// one of Robert Penner's curves mux registers below 256, or a game's own,
// registered from 256 with RegisterEasing. It moves through the interpolator
// of its property's type: every float and integer kind has one built in, and
// a game registers one for its own types with RegisterInterpolator:
//
//	mux.RegisterInterpolator(configurator, func(a, b Vec2, t float32, ease func(a, b, t float32) float32) Vec2 {
//		return Vec2{X: ease(a.X, b.X, t), Y: ease(a.Y, b.Y, t)}
//	})
//
// The timeline's time is its own: only its Step task moves it, by the delta
// it is given, so it is deterministic under any clock. Step samples every
// active override at the new time, which Current then reads. The game
// schedules Step once per rendered frame, before its views.
//
// See docs/specs/mux.md.
package mux
