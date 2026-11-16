package ecs

import "github.com/dvoyni/cog/kernel"

// ObservationResetSystem is the system that ends world W's observation
// window, registered with RegisterTask1(ecs.ObservationResetSystem[Game])
// and scheduled where the frame needs it: first, last, after the network
// send, after the renderer has read its deltas. It takes the barrier. Every
// observed store's shadows take their rows' values and its removed rows are
// dropped; the window's despawned indices, and reservations never spawned,
// go back to the free list.
//
// Its first run turns observation on, and every row present then counts as
// added in the first window. A world it never runs in observes nothing and
// keeps nothing for observers: a despawned index is reused at once.
func ObservationResetSystem[W WorldID](k *kernel.Kernel, _ kernel.NoArg, world World[W]) kernel.NoResult {
	world.world.resetObservation()
	return kernel.NoResult{}
}

// ShrinkSystem is the system that gives back the memory world W holds past
// what is in use, registered with RegisterTask1(ecs.ShrinkSystem[Game]) and
// scheduled where the game can afford regrowing, such as after a level
// unloads. It takes the barrier, and does what World.Shrink does.
func ShrinkSystem[W WorldID](k *kernel.Kernel, _ kernel.NoArg, world World[W]) kernel.NoResult {
	world.Shrink()
	return kernel.NoResult{}
}
