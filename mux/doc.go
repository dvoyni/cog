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
// ElevateTimeline. The timeline's time is its own: only its Step task moves
// it, by the delta it is given, so it is deterministic under any clock. The
// game schedules Step once per rendered frame, before its views.
//
// See docs/specs/mux.md.
package mux
