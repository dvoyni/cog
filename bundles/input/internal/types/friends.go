package types

// The friend functions: what input's internal/ reads from, or does to, a
// public type's unexported state. Only packages under bundles/input can import
// this package, so these are not public API.

// ChangeDown reads Change.down for input's internal/.
func ChangeDown(v *Change) bool { return v.down }

// ChangeDx reads Change.dx for input's internal/.
func ChangeDx(v *Change) float64 { return v.dx }

// ChangeDy reads Change.dy for input's internal/.
func ChangeDy(v *Change) float64 { return v.dy }

// ChangeKey reads Change.key for input's internal/.
func ChangeKey(v *Change) Key { return v.key }

// ChangeKindOf reads Change.kind for input's internal/.
func ChangeKindOf(v *Change) changeKind { return v.kind }

// ChangeMods reads Change.mods for input's internal/.
func ChangeMods(v *Change) Mods { return v.mods }

// ChangeModsRef points at Change.mods for input's internal/.
func ChangeModsRef(v *Change) *Mods { return &v.mods }

// ChangePos reads Change.pos for input's internal/.
func ChangePos(v *Change) Pos { return v.pos }

// ChangeRune reads Change.r for input's internal/.
func ChangeRune(v *Change) rune { return v.r }

// ChangeText reads Change.text for input's internal/.
func ChangeText(v *Change) string { return v.text }

// StateAdvance calls State.advance for input's internal/.
func StateAdvance(v *State) { v.advance() }

// StateApply calls State.apply for input's internal/.
func StateApply(v *State, a0 Change) { v.apply(a0) }

// StateHeld calls State.held for input's internal/.
func StateHeld(v *State) []Key { return v.held() }

// StateModifiers calls State.modifiers for input's internal/.
func StateModifiers(v *State) Mods { return v.modifiers() }
