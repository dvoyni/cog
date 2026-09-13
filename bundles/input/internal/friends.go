package internal

// The friend functions: what the input root and inputimpl read from, or do to,
// a public type's unexported state. Only the root and inputimpl can import this
// package, so these are not public API.

// ChangeDown reads Change.down for the input root and inputimpl.
func ChangeDown(v *Change) bool { return v.down }

// ChangeDx reads Change.dx for the input root and inputimpl.
func ChangeDx(v *Change) float64 { return v.dx }

// ChangeDy reads Change.dy for the input root and inputimpl.
func ChangeDy(v *Change) float64 { return v.dy }

// ChangeKey reads Change.key for the input root and inputimpl.
func ChangeKey(v *Change) Key { return v.key }

// ChangeKindOf reads Change.kind for the input root and inputimpl.
func ChangeKindOf(v *Change) changeKind { return v.kind }

// ChangeMods reads Change.mods for the input root and inputimpl.
func ChangeMods(v *Change) Mods { return v.mods }

// ChangeModsRef points at Change.mods for the input root and inputimpl.
func ChangeModsRef(v *Change) *Mods { return &v.mods }

// ChangePos reads Change.pos for the input root and inputimpl.
func ChangePos(v *Change) Pos { return v.pos }

// ChangeRune reads Change.r for the input root and inputimpl.
func ChangeRune(v *Change) rune { return v.r }

// StateAdvance calls State.advance for the input root and inputimpl.
func StateAdvance(v *State) { v.advance() }

// StateApply calls State.apply for the input root and inputimpl.
func StateApply(v *State, a0 Change) { v.apply(a0) }

// StateHeld calls State.held for the input root and inputimpl.
func StateHeld(v *State) []Key { return v.held() }

// StateModifiers calls State.modifiers for the input root and inputimpl.
func StateModifiers(v *State) Mods { return v.modifiers() }
