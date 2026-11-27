package sfx

import (
	"slices"

	"github.com/dvoyni/cog/libs/m"
)

// busVolumes is what BusVolumes names: one linear volume per bus, indexed by
// a resolved bus. It holds one slice the flush writes in place.
type busVolumes struct{ volumes []float32 }

// makeBusVolumes answers the volumes of busCount buses, every one at unity,
// which is what makes a game that declares no bus audible.
func makeBusVolumes(busCount int) busVolumes {
	return busVolumes{volumes: slices.Repeat([]float32{1}, busCount)}
}

// Volume answers bus's linear volume, 1 being unity, as of the last flush, so
// a settings screen draws its slider where the player left it. A SetBus
// recorded before a flush is visible after it. A bus outside
// Config.MaxBuses answers Master's.
func (b busVolumes) Volume(bus Bus) float32 { return b.volumes[bus.resolve(len(b.volumes))] }

// apply installs the volumes a flush's SetBus calls coalesced to, one per
// resolved bus, a bus no SetBus named keeping its own.
func (b busVolumes) apply(sets []m.Maybe[float32]) {
	for index, set := range sets {
		if volume, ok := set.Get(); ok {
			b.volumes[index] = volume
		}
	}
}
