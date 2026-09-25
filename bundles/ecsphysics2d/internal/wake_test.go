package internal

import (
	"math"
	"reflect"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
)

// A sleeping crate a Kinematic paddle touches in the tick its Island wakes is
// solved as one Body, exactly as the same crate awake would be. Detect finds
// the paddle's Contact with the crate where it sleeps, in the sleepers' grid;
// the sleep System then wakes the Island and brings its quiet Contact with the
// floor back. The failing sequence is two solver rows for the one crate, one
// per Contact: each row is solved against its own Contact alone and both write
// back to the one Velocity, so one Contact's Impulse is lost for the tick —
// the push, or the carry, or the floor's.

// gatherer reads, between ticks, which Entities the last Solve gathered into
// its dense rows. The rows are Solve's private scratch and nothing public
// names them, so it reads them by reflection: they are the one place a Body
// solved twice over shows as itself rather than as a wrong velocity.
type gatherer struct{}

func (*gatherer) Name() kernel.PluginName { return "physicstestgatherer" }

func (*gatherer) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, Name}
}

type gatheredCmd kernel.Command[gatheredRequest, gatheredResponse]

type gatheredRequest struct{}

type gatheredResponse struct{ Rows []ecs.Entity }

func (*gatherer) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[gatheredCmd](ecs.ToExecute[gatheredRequest, gatheredResponse](registrar, func(
		_ gatheredRequest,
		contacts *ecs.Read[*Contacts],
		answer *ecs.Resp[gatheredResponse],
	) {
		rows := reflect.ValueOf(contacts.Get()).Elem().FieldByName("solver").FieldByName("rows")
		var gathered []ecs.Entity
		// Row 0 is the immovable one every Static party shares.
		for i := 1; i < rows.Len(); i++ {
			gathered = append(gathered, ecs.Entity(rows.Index(i).FieldByName("entity").Uint()))
		}
		answer.Set(gatheredResponse{Rows: gathered})
	}))
	return nil
}

// gatheredOnce fails for every Entity the last Solve gathered into more than
// one row.
func gatheredOnce(t testing.TB, h *harness) {
	t.Helper()
	rows := h.kernel.ExecuteCommand[gatheredCmd](gatheredRequest{}).Rows
	seen := map[ecs.Entity]int{}
	for _, e := range rows {
		seen[e]++
	}
	for e, n := range seen {
		if n > 1 {
			t.Errorf("Solve gathered %v into %d rows, want one", e, n)
		}
	}
}

// wakeScene is crates resting on the floor, side by side at the x given, and
// a Kinematic paddle, a box, that arrives once they have settled.
type wakeScene struct {
	crates             []float64
	paddleAt, paddleBy m.Vec2d
	// pinned pins each crate to the next with a Joint as long as the gap.
	pinned bool
}

// wokenPaddle is the paddle's Shape: the crate's size, so that it meets the
// crate face to face, and lifted clear of the floor.
func wokenPaddle() Shape { return NewBoxShape(0.5, 0.5, 0) }

// run builds the scene with sleeping on or off, lets it stand for ticks, or
// until every crate sleeps when ticks is 0, and answers how many ticks it
// stood, then brings the paddle in.
func (s wakeScene) run(t *testing.T, sleep bool, ticks int) (h *harness, crates []ecs.Entity, k ecs.Entity, stood int) {
	t.Helper()
	config := Sleep{}
	if sleep {
		config = Sleep{Time: napTime}
	}
	h = newHarnessWithPlugins(t, nil, 1024, &weigher{gravity: napGravity}, &napper{}, &gatherer{})
	h.setSleep(t, config)
	h.spawn(t, spawnRequest{Kind: kindShapedStatic, Shape: pileFloor()})
	for _, x := range s.crates {
		crates = append(crates, spawnCrates(t, h, x, 0, 1)...)
	}
	for i := 1; s.pinned && i < len(crates); i++ {
		h.spawn(t, spawnRequest{
			Kind:  kindJoint,
			Joint: NewPinJoint(crates[i-1], crates[i], m.Vec2d{}, m.Vec2d{}, s.crates[i-1]-s.crates[i]),
		})
	}
	if ticks == 0 {
		stood = settle(t, h, crates, 300)
	} else {
		h.frames(t, ticks)
		stood = ticks
	}
	k = paddle(t, h, wokenPaddle(), s.paddleAt, s.paddleBy.MulS(1/tick))
	return h, crates, k, stood
}

// check runs the scene asleep and awake and compares every crate, tick by
// tick, for the tick the paddle arrives and the one after: the woken crates
// must move as the awake ones do, and no Body may be gathered twice. It
// answers the woken scene for the case's own claims.
func (s wakeScene) check(t *testing.T) (*harness, []ecs.Entity, ecs.Entity) {
	t.Helper()
	asleep, woken, kw, stood := s.run(t, true, 0)
	awake, reference, ka, _ := s.run(t, false, stood)
	for step := 1; step <= 2; step++ {
		asleep.frame(t)
		awake.frame(t)
		gatheredOnce(t, asleep)
		gatheredOnce(t, awake)
		if got, want := asleep.read(t, kw).Place.Current, awake.read(t, ka).Place.Current; got != want {
			t.Fatalf("tick %d: the paddle stands at %v asleep and %v awake", step, got, want)
		}
		for i := range woken {
			got, want := asleep.read(t, woken[i]), awake.read(t, reference[i])
			if d := got.Place.Current.Distance(want.Place.Current); d > 1e-3 {
				t.Errorf("tick %d: crate %d stands at %v woken and %v awake", step, i, got.Place.Current, want.Place.Current)
			}
			if d := got.Velocity.Linear.Distance(want.Velocity.Linear); d > 1e-2 {
				t.Errorf("tick %d: crate %d moves at %v woken and %v awake", step, i, got.Velocity.Linear, want.Velocity.Linear)
			}
		}
	}
	return asleep, woken, kw
}

// The slow push, a discrete touch: the paddle goes 0.1 m a tick, and the tick
// it arrives it overlaps the sleeping crate by 0.05 m. Solved twice over, the
// crate keeps the floor's row's velocity, 0, and the paddle overlaps it the
// next tick; solved once, it is pushed off at the paddle's −6 m/s.
func TestASlowPaddleTouchingASleepingCratePushesItAsAnAwakeOne(t *testing.T) {
	wakeScene{
		crates:   []float64{0},
		paddleAt: m.Vec2d{X: 0.55, Y: 0.3}, paddleBy: m.Vec2d{X: -0.1},
	}.check(t)
}

// The first-Hit carry: the paddle goes 5 m a tick, so its path meets the
// sleeping crate at T = 0.5 and carries it to x = −2.5, flush with the
// paddle's front face. Solved twice over, the crate keeps the floor's row's
// velocity, 0, and stays there while the paddle's next tick sweeps from −2 to
// −7: passed through. Solved once, it leaves the paddle's path as the awake
// one does, thrown up and over it, turned by the floor's Friction.
func TestAFastPaddleCarryingASleepingCrateLeavesItsPath(t *testing.T) {
	h, crates, k := wakeScene{
		crates:   []float64{0},
		paddleAt: m.Vec2d{X: 3, Y: 0.3}, paddleBy: m.Vec2d{X: -5},
	}.check(t)
	// The band the paddle swept on the second tick, against the crate's reach
	// from its centre at any angle.
	paddle, crate := h.read(t, k).Place, h.read(t, crates[0]).Place.Current
	reach := 0.25 * math.Sqrt2
	inFront := crate.X+reach < paddle.Current.X-0.25
	above := crate.Y-reach > paddle.Current.Y+0.25
	if !inFront && !above {
		t.Errorf("the crate stands at %v, in the band the paddle swept from %v to %v",
			crate, paddle.Previous, paddle.Current)
	}
}

// The carry past the first Hit: a second sleeping crate stands behind the
// first on the paddle's path, and is carried from a Hit the path pass held.
// Solved twice over, it loses the floor's Impulse instead of the paddle's.
func TestAFastPaddleCarryingTwoSleepingCratesSolvesEachOnce(t *testing.T) {
	wakeScene{
		crates:   []float64{0, -1.5},
		paddleAt: m.Vec2d{X: 3, Y: 0.3}, paddleBy: m.Vec2d{X: -5},
	}.check(t)
}

// A Joint reaches the rows by Entity, not by slot: Solve seeds its table from
// the rows the Contacts made. Pinned to a second crate, the pushed crate's two
// rows would leave the Joint pulling on one of them; solved once, the pair is
// pushed together as the awake pair is.
func TestASlowPaddleTouchingAPinnedSleepingCratePushesThePairAsAnAwakeOne(t *testing.T) {
	wakeScene{
		crates:   []float64{0, -1},
		paddleAt: m.Vec2d{X: 0.55, Y: 0.3}, paddleBy: m.Vec2d{X: -0.1},
		pinned: true,
	}.check(t)
}
