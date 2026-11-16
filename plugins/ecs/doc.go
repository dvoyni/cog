// Package ecs is Cog's entity component system on the kernel.
//
// A world, named by its world ID, holds entities whose data lives in plain,
// pointer-free components, one store per component type. Each store is a
// kernel resource, so it is the lock unit: systems touching different
// components run in parallel, and a system taking World, the write on the
// world, runs alone in it.
//
// A system is a plain function of one form, the kernel, the job's arg, up to
// eight system parameters, and the job's result, registered as a kernel task
// with the kernel's RegisterTask0 to RegisterTask8:
//
//	func move(k *kernel.Kernel, t Tick, q ecs.Query2[Game, *Position, Velocity]) kernel.NoResult {
//		for _, row := range q.All() {
//			pos, vel := row.Get()
//			pos.X += vel.X * t.Dt
//		}
//		return kernel.NoResult{}
//	}
//
//	c.move = configurator.RegisterTask1(move)
//
// Each parameter declares its locks at registration. Once the containers
// have grown, nothing on the hot path allocates.
//
// See docs/design.md.
package ecs
