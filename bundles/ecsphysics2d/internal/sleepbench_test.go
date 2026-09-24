package internal

import (
	"fmt"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"

	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// What sleeping costs and what it saves, measured over settled piles under the
// plugin's own gravity: stacks of four crates side by side on one floor, each
// stack an Island of its own. The allocation-line tests are assertions; the
// benchmarks report, and wall-clock is asserted nowhere.

// pileStack is how many crates a stack holds, and pileSpacing how far apart the
// stacks stand, which is far enough that no two stacks touch.
const (
	pileStack   = 4
	pileSpacing = 1.0
)

// populatePiles stands n crates in stacks of pileStack on one floor long
// enough for all of them, and answers the crates.
func populatePiles(t testing.TB, h *harness, n int) []ecs.Entity {
	t.Helper()
	if n == 0 {
		return nil
	}
	stacks := (n + pileStack - 1) / pileStack
	floor := NewBoxShapeFor(NewBB(-1, -1, float64(stacks)*pileSpacing+1, 0), 0)
	floor.Friction = 0.7
	h.spawn(t, spawnRequest{Kind: kindShapedStatic, Shape: floor})
	var crates []ecs.Entity
	for i := range stacks {
		count := min(pileStack, n-i*pileStack)
		crates = append(crates, spawnCrates(t, h, float64(i)*pileSpacing, 0, count)...)
	}
	return crates
}

// kicker is an app System that keeps Islands falling asleep and waking: each
// tick it kicks the sleeping crates of one stack upwards, a different stack
// every tick, so that at any moment some stacks are waking, some settling and
// some asleep. It counts the Bodies it kicked.
type kicker struct {
	stacks int
	tick   int
	kicks  int
}

type kickQuery struct {
	Place    Position
	Velocity *Velocity
	_        ecs.With[Sleeping]
}

type kickOnUpdate kernel.Subscription[app.UpdateEvent]

func (*kicker) Name() kernel.PluginName { return "physicstestkicker" }

func (*kicker) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, Name}
}

func (k *kicker) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[kickOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[kickQuery]) {
		k.tick++
		if k.stacks == 0 {
			return
		}
		column := float64(k.tick%k.stacks) * pileSpacing
		for _, it := range q.All() {
			if it.Place.Current.X > column-0.3 && it.Place.Current.X < column+0.3 {
				it.Velocity.Linear.Y += 0.5
				k.kicks++
			}
		}
	})).Before[IntegrateOnUpdate]()
	return nil
}

// newPileHarness is a harness under the plugin's gravity with sleeping set to
// sleep, and with kicker composed beside it when one is given.
func newPileHarness(t testing.TB, n int, sleep Sleep, kick *kicker) *harness {
	t.Helper()
	plugins := []kernel.Plugin{&weigher{gravity: napGravity}, &napper{}}
	if kick != nil {
		plugins = append(plugins, kick)
	}
	h := newHarnessWithPlugins(t, nil, uint32(2*max(n, 1)), plugins...)
	h.setSleep(t, sleep)
	return h
}

// sleepingCount is how many of crates sleep.
func sleepingCount(t testing.TB, h *harness, crates []ecs.Entity) int {
	t.Helper()
	return len(crates) - awakeCount(t, h, crates)
}

// TestTheSleepingStepSitsOnTheEnginesAllocationLine is the allocation line
// with sleeping on, twice: over settled piles all asleep, where the step walks
// the sleepers and compares them and nothing else, and over the same piles
// churning — a stack kicked awake every tick while others settle and fall
// asleep again — which puts the Island build, the Tag added and removed, the
// sleepers' grid moved in and out, and the quiet Contacts handed back inside
// the measurement. The empty engine carries the same Systems, so its line
// includes their dispatch.
func TestTheSleepingStepSitsOnTheEnginesAllocationLine(t *testing.T) {
	if raceEnabled {
		t.Skip("allocation counts are not meaningful under -race")
	}
	const ticks = 4_000

	measure := func(n int, churn bool) (float64, int, int) {
		var kick *kicker
		if churn {
			kick = &kicker{}
		}
		h := newPileHarness(t, n, Sleep{Time: napTime}, kick)
		crates := populatePiles(t, h, n)
		// Settled first, and then — churning — kicked for long enough that
		// every buffer the churn fills has grown to what it needs.
		h.frames(t, 200)
		if churn {
			kick.stacks = (n + pileStack - 1) / pileStack
			h.frames(t, 600)
		}
		asleep := sleepingCount(t, h, crates)
		kicked := 0
		if churn {
			kicked = kick.kicks
		}
		mallocs := allocationsDuring(func() {
			for range ticks {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
		if churn {
			kicked = kick.kicks - kicked
		}
		return float64(mallocs) / ticks, asleep, kicked
	}

	empty, _, _ := measure(0, false)
	// The churn's own line carries the kicker's System, one more subscription.
	emptyChurn, _, _ := measure(0, true)
	small, smallAsleep, _ := measure(256, false)
	large, largeAsleep, _ := measure(1024, false)
	churnSmall, churnSmallAsleep, smallKicks := measure(256, true)
	churnLarge, churnLargeAsleep, largeKicks := measure(1024, true)
	t.Logf("objects a step with sleeping on: %.3f with no Bodies; settled, %.3f at N=256 (%d asleep) "+
		"and %.3f at N=1024 (%d asleep); churning, %.3f with no Bodies, %.3f at N=256 "+
		"(%d asleep when it began, %d kicks) and %.3f at N=1024 (%d asleep when it began, %d kicks)",
		empty, small, smallAsleep, large, largeAsleep, emptyChurn,
		churnSmall, churnSmallAsleep, smallKicks, churnLarge, churnLargeAsleep, largeKicks)

	if smallAsleep != 256 || largeAsleep != 1024 {
		t.Fatal("the settled piles are not all asleep, so the measurement is not of a sleeping step")
	}
	if smallKicks == 0 || largeKicks == 0 || churnSmallAsleep == 0 || churnLargeAsleep == 0 {
		t.Fatal("the churning piles did not both sleep and wake, so the measurement is not of a churn")
	}
	for _, got := range []struct {
		name         string
		value, empty float64
	}{
		{"settled at N=256", small, empty}, {"settled at N=1024", large, empty},
		{"churning at N=256", churnSmall, emptyChurn}, {"churning at N=1024", churnLarge, emptyChurn},
	} {
		if got.value-got.empty > 0.05 {
			t.Errorf("the sleeping step %s costs %.3f objects a tick against %.3f with no Bodies",
				got.name, got.value, got.empty)
		}
	}
}

// BenchmarkTheSettledPile is one tick over settled piles under the plugin's
// gravity, with sleeping off and on. Off, every resting Contact is detected and
// solved every tick; on, the piles are asleep, and the tick walks the sleepers
// and compares them.
func BenchmarkTheSettledPile(b *testing.B) {
	for _, n := range []int{256, 1024} {
		for _, on := range []bool{false, true} {
			name := fmt.Sprintf("N=%d/sleeping=off", n)
			sleep := Sleep{}
			if on {
				name = fmt.Sprintf("N=%d/sleeping=on", n)
				sleep = Sleep{Time: napTime}
			}
			b.Run(name, func(b *testing.B) {
				h := newPileHarness(b, n, sleep, nil)
				crates := populatePiles(b, h, n)
				h.frames(b, 200)
				asleep := sleepingCount(b, h, crates)
				if on && asleep != n {
					b.Fatalf("%d of %d crates asleep after 200 ticks", asleep, n)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
				}
			})
		}
	}
}

// BenchmarkTheIslandBuild is the reference scene of BenchmarkTheStep — Bodies
// under a Force, Kinematic ones, Contacts that never settle — with sleeping off
// and with sleeping on at a Time nothing ever reaches: what the Island build
// costs a tick on which nothing sleeps.
func BenchmarkTheIslandBuild(b *testing.B) {
	for _, n := range []int{256, 1024} {
		for _, on := range []bool{false, true} {
			name := fmt.Sprintf("N=%d/sleeping=off", n)
			sleep := Sleep{}
			if on {
				name = fmt.Sprintf("N=%d/sleeping=on", n)
				sleep = Sleep{Time: 1e9}
			}
			b.Run(name, func(b *testing.B) {
				h := newHarnessWithPlugins(b, nil, uint32(2*n), &napper{})
				h.setSleep(b, sleep)
				populate(b, h, n)
				h.game.push = m.Vec2d{X: 10}
				h.frames(b, 100)

				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
				}
			})
		}
	}
}
