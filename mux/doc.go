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
// A Path names one property of a model through its struct fields, arrays,
// slices and maps, compiled once by MakePath, by reflection. Each [] in it is
// a hole, which every read and override fills with an int64, and a literal
// index leaves none; a path with its holes filled is an address. Get reads
// the model's own value at an address, allocating nothing, or answers false
// for an index outside its array or slice, or a key its map lacks. The game
// moves no element of a slice while an override addresses it, freeing
// elements rather than closing the gaps:
//
//	position := mux.MakePath[Game, Vec2](configurator, "Squads[].Members[].Position")
//	value, ok := position.Get(model.Get(), squad, member)
//
// A map's integer key is its own hole. Any other key is interned in the
// model's timeline by the update that adds its element, and a view looks its
// index up without writing:
//
//	health := mux.MakePath[Game, int32](configurator, "UnitsByName[].Health")
//
//	// An update adds a unit, and interns its name:
//	model.Get().UnitsByName["ada"] = Unit{Health: 30}
//	mux.ElevateTimeline(timeline).Intern(UnitName("ada"))
//
//	// A view looks the name's index up, and reads through it:
//	ada, found := timeline.Get().Index(UnitName("ada"))
//	value, ok := health.Get(model.Get(), ada)
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
// A view reads each property's current value: the blend of the active
// overrides by weight, or the model's own when none is. Each weighs nothing
// at its start and end, so a tween over a running one takes over with no
// jump:
//
//	shown, ok := timeline.Get().Current(zoom, model.Get())
//
// A hold keeps a property at one value over a span. It calls no
// interpolator to make its value, so it holds a property of any type, such
// as a bool, where overlapping overrides show the one that weighs most. End
// answers when an address is next free, so an update can queue after
// whatever plays there, and Cancel removes every override on an address:
//
//	unitPosition := mux.MakePath[Game, Vec2](configurator, "Units[].Position")
//	unitVisible := mux.MakePath[Game, bool](configurator, "Units[].Visible")
//
//	// A unit just added stays hidden for a second, until its entrance:
//	writeTimeline.Hold(unitVisible, writeTimeline.Now(), false, time.Second, unit)
//
//	// A unit's next walk queues after the one playing:
//	writeTimeline.Tween(unitPosition, writeTimeline.End(unitPosition, unit), from, to, time.Second, mux.EasingLinear, unit)
//
//	// A unit's plan changed, so nothing of the old one shows:
//	writeTimeline.Cancel(unitPosition, unit)
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
