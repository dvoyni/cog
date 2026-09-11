// Package proto wires the ecs prototype onto a real kernel engine and drives it
// with a real app.UpdateEvent tick. That is the whole point of cog#243: the
// any-boxed resource cell, the real lock acquisition and the reflective call
// are exactly where an allocation would hide, and none of them exist in a
// standalone microbenchmark.
package proto

import (
	"github.com/dvoyni/cog/app"
	"github.com/dvoyni/cog/kernel"

	"protoecs/ecs"
)

// Components. Every one is pointer-free, transitively, as cog#237 requires.

type Body struct{ Px, Py, Vx, Vy float64 } // 32B, written
type Collider struct {
	R    float64
	Mask uint64
}                                      // 16B, read
type Health struct{ Cur, Max float32 } // 8B, read
type Frozen struct{}                   // a Tag: a fieldless Component

// Queries. A field's pointer-ness is its access mode (cog#238).

type MoveQ struct {
	*Body
	Collider
}

type DamageQ struct {
	*Health
	Collider
}

type ThawQ struct {
	*Body
	Collider
	Health
}

type UnfrozenQ struct {
	*Body
	Collider
	_ ecs.Without[Frozen]
}

type ProjectileBundle struct {
	Body     Body
	Collider Collider
	Health   Health
}

var (
	sinkF float64
	sinkI int
)

var last struct {
	bodies   *ecs.Store[Body]
	entities *ecs.Entities
}

// Systems are plain Go funcs. None returns anything: cog#234 found
// reflect.Value.Call allocates only for a callee that returns a value.

func moveSystem(q *ecs.Query[MoveQ], ev app.UpdateEvent) {
	dt := ev.Dt
	acc := 0.0
	for _, it := range q.All() {
		it.Px += it.Vx * dt
		it.Py += it.Vy * dt
		acc += it.R
	}
	sinkF = acc
}

func damageSystem(q *ecs.Query[DamageQ], ev app.UpdateEvent) {
	dt := float32(ev.Dt)
	acc := 0.0
	for _, it := range q.All() {
		it.Cur -= dt
		if it.Cur < 0 {
			it.Cur = it.Max
		}
		acc += it.R
	}
	sinkF = acc
}

func thawSystem(q *ecs.Query[ThawQ], ev app.UpdateEvent) {
	dt := ev.Dt
	acc := 0.0
	for _, it := range q.All() {
		it.Px += it.Vx * dt
		acc += it.R + float64(it.Cur)
	}
	sinkF = acc
}

func filteredSystem(q *ecs.Query[UnfrozenQ], ev app.UpdateEvent) {
	dt := ev.Dt
	n := 0
	for _, it := range q.All() {
		it.Px += it.Vx * dt
		n++
	}
	sinkI = n
}

// churnSystem is the structural change nox's real workload never holds still
// without: one projectile spawned and one despawned every tick.
func churnSystem(sp *ecs.Spawn[ProjectileBundle], w *ecs.WriteableEntities, _ app.UpdateEvent) {
	e := sp.New(ProjectileBundle{
		Body:     Body{Vx: 1, Vy: 1},
		Collider: Collider{R: 0.5, Mask: 1},
		Health:   Health{Cur: 1, Max: 1},
	})
	if prev != 0 {
		w.Despawn(prev)
	}
	prev = e
}

// prev is the projectile spawned last tick, despawned this one, so the entity
// count is steady and the free list is exercised every frame.
var prev ecs.Entity

// Subscription identities. Each logical subscription needs its own defined type.

type (
	MoveSub     kernel.Subscription[app.UpdateEvent]
	DamageSub   kernel.Subscription[app.UpdateEvent]
	ThawSub     kernel.Subscription[app.UpdateEvent]
	FilteredSub kernel.Subscription[app.UpdateEvent]
	ChurnSub    kernel.Subscription[app.UpdateEvent]
)

// mode selects which Systems the plugin registers, so one plugin serves every
// benchmark shape.
type mode uint8

const (
	modeReflected     mode = iota // one System, call through reflect.Value.Call
	modeBaked                     // one System, call baked by a generic builder
	modeThree                     // one System over three Components
	modeFiltered                  // one System with a Without filter
	modeStructural                // move plus a spawn/despawn every tick
	modeTwoDisjoint               // two Systems whose write sets do not overlap
	modeNone                      // no subscriptions at all: the floor of PublishEvent
	modeBare                      // the same work as a hand-written kernel subscription
	modeHybrid                    // ToHandler's machinery, ToHandler1's call
	modeNoopReflected             // a System that does nothing, called reflectively
	modeNoopBaked                 // the same System, called directly
)

// idleSystem touches neither its Query nor the event, so a frame through it
// measures the builder and nothing else.
func idleSystem(q *ecs.Query[MoveQ], ev app.UpdateEvent) { sinkI++ }

type protoPlugin struct {
	entities int
	mode     mode
}

func (protoPlugin) Name() kernel.PluginName           { return "proto" }
func (protoPlugin) Dependencies() []kernel.PluginName { return nil }

func (p protoPlugin) Register(r *kernel.Registrar, _ any) error {
	// Room for the seeded entities plus whatever churn spawns before the free
	// list starts recycling.
	ids := uint32(p.entities + 1024)
	en := ecs.NewEntities(ids)
	r.InitResource[*ecs.Entities](en)

	bodies := ecs.RegisterComponent[Body](r, en, ids)
	// Kept so the correctness tests can read the world back after a tick. A
	// real plugin would reach it through a Query like everything else.
	last.bodies, last.entities = bodies, en
	colliders := ecs.RegisterComponent[Collider](r, en, ids)
	healths := ecs.RegisterComponent[Health](r, en, ids)
	frozen := ecs.RegisterComponent[Frozen](r, en, ids)

	for i := range p.entities {
		e := en.Alloc()
		bodies.Add(e, Body{Px: float64(i), Py: float64(i), Vx: 1, Vy: 1})
		colliders.Add(e, Collider{R: 0.5, Mask: 1})
		healths.Add(e, Health{Cur: 10, Max: 10})
		if i%10 == 0 {
			frozen.Add(e, Frozen{})
		}
	}

	switch p.mode {
	case modeReflected:
		r.Subscribe[MoveSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, moveSystem))
	case modeBaked:
		r.Subscribe[MoveSub, app.UpdateEvent](ecs.ToHandler1(en, moveSystem))
	case modeThree:
		r.Subscribe[ThawSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, thawSystem))
	case modeFiltered:
		r.Subscribe[FilteredSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, filteredSystem))
	case modeStructural:
		r.Subscribe[MoveSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, moveSystem))
		r.Subscribe[ChurnSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, churnSystem))
	case modeTwoDisjoint:
		// moveSystem writes Body; damageSystem writes Health. Neither names the
		// other's write, so the scheduler may run them concurrently.
		r.Subscribe[MoveSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, moveSystem))
		r.Subscribe[DamageSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, damageSystem))
	case modeNone:
		// Nothing subscribes, so a publication completes immediately. Whatever
		// this costs is the engine's price for a tick, before any ECS exists.
	case modeBare:
		r.Subscribe[MoveSub, app.UpdateEvent](bareMove(bodies, colliders))
	case modeHybrid:
		r.Subscribe[MoveSub, app.UpdateEvent](ecs.ToHandlerHybrid(en, moveSystem))
	case modeNoopReflected:
		r.Subscribe[MoveSub, app.UpdateEvent](ecs.ToHandler[app.UpdateEvent](en, idleSystem))
	case modeNoopBaked:
		r.Subscribe[MoveSub, app.UpdateEvent](ecs.ToHandler1(en, idleSystem))
	}
	return nil
}

// bareMove is moveSystem written by hand against the Stores directly: the same
// arithmetic over the same memory, through an ordinary kernel subscription with
// no Query, no reflection and no handler builder. It is the baseline that says
// which allocations belong to the ECS and which the engine would charge anyway.
func bareMove(
	bodies *ecs.Store[Body], colliders *ecs.Store[Collider],
) func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
	return func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		var wb kernel.Write[*ecs.Store[Body]]
		var rc kernel.Read[*ecs.Store[Collider]]
		var re kernel.Read[*ecs.Entities]
		return func(a kernel.ResourceAccess) {
				re = a.GetRead[*ecs.Entities]()
				wb = a.GetWrite[*ecs.Store[Body]]()
				rc = a.GetRead[*ecs.Store[Collider]]()
			}, func(_ kernel.Kernel, ev app.UpdateEvent) error {
				_ = re.Get()
				b, c := wb.Get(), rc.Get()
				dt := ev.Dt
				acc := 0.0
				for i := b.Len() - 1; i >= 0; i-- {
					e := b.OwnerAt(i)
					p, ok := b.Get(e)
					if !ok {
						continue
					}
					col, ok := c.Get(e)
					if !ok {
						continue
					}
					p.Px += p.Vx * dt
					p.Py += p.Vy * dt
					acc += col.R
				}
				sinkF = acc
				return nil
			}
	}
}
