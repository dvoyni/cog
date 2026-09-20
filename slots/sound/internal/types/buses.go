package types

import "github.com/dvoyni/cog/libs/m"

// MaxBuses is how many Buses exist, Master among them. It is a fixed maximum
// rather than a configured one: a Bus is a handful of constants a game
// declares - music, effects, dialogue, UI - and a table of 32 is smaller than
// the bookkeeping a growable one would need.
const MaxBuses = 32

// resolve answers which Bus a named one actually is.
//
// An out-of-range Bus is Master rather than an error, so a mis-declared
// constant does not silence a game, and it is Master everywhere a Bus is named
// - a Play, a SetBus, a StopBus, a read - because one rule cannot disagree with
// itself. A game whose constant is wrong is wrong about it consistently: its
// sounds play on Master and its slider moves Master, which is what it asked
// for, one Bus over.
func (b Bus) resolve() Bus {
	if b < 0 || b >= MaxBuses {
		return Master
	}
	return b
}

// Buses is every Bus's volume as of the last flush. It is a resource of its own
// so that the settings screen reading a slider's position never contends with
// the Systems recording operations, which is why sound's resources are several
// rather than one.
//
// It is read-only to a caller, like the other views: a volume is changed by
// recording SetBus on the Queue, and the flush applies it. There is no Mute
// beside the volume - a game persists its settings anyway, and a shadow copy
// inside sound invites "muted by whom".
//
// Access it only while a handler holds its declared resource lock.
type Buses struct {
	// volumes is one linear volume per Bus, indexed by a resolved Bus.
	volumes [MaxBuses]float32
}

// NewBuses builds the table with every Bus at unity, which is what makes a game
// that declares no Bus at all audible and gives it a working master slider.
func NewBuses() *Buses {
	buses := &Buses{}
	for i := range buses.volumes {
		buses.volumes[i] = 1
	}
	return buses
}

// Volume reports a Bus's volume, so a settings screen draws its slider where
// the player left it.
//
// It answers as of the last flush: a SetBus recorded in this tick is visible in
// the next one, which is true of every operation a game records.
func (b *Buses) Volume(bus Bus) float32 { return b.volumes[bus.resolve()] }

// apply installs the tick's coalesced volumes, last value having won at the
// moment each was recorded. It walks the whole table rather than a list of what
// moved because the table is 32 entries and a list is an allocation.
func (b *Buses) apply(sets *[MaxBuses]m.Maybe[float32]) {
	for i := range sets {
		if volume, ok := sets[i].Get(); ok {
			b.volumes[i] = volume
		}
	}
}
