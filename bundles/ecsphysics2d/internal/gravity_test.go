package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
)

// Gravity is cp's term in cp's BodyUpdateVelocity, read out of the Constants
// Resource the plugin registers. These tests reach it the way an app does: a
// System of the app's own takes ecs.Write[*Constants] and writes it.

// gravityOnUpdate is the app's gravity System, ordered before Integrate so the
// value it writes is the one this tick's Solve reads.
type gravityOnUpdate kernel.Subscription[app.UpdateEvent]

// constantsCmd reads the Constants back from outside a tick.
type constantsCmd kernel.Command[constantsRequest, constantsResponse]

type constantsRequest struct{}

type constantsResponse struct{ Constants ecsphysics2d.Constants }

// weigher is an app that owns gravity: its System writes Gravity at the top of
// every tick. It is composed only into the tests that need it, so every other
// test's frame keeps the lock picture it had.
//
// gravity is written only between ticks, and PublishEvent(…).Wait() is the
// happens-before edge that makes that safe, as with game.push.
type weigher struct{ gravity m.Vec2d }

func (*weigher) Name() kernel.PluginName { return "physicstestweigher" }

func (*weigher) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsphysics2d.Name}
}

func (w *weigher) Register(registrar *kernel.Registrar, _ any) error {
	registerConstantsCmd(registrar)
	registrar.Subscribe[gravityOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
		constants *ecs.Write[*ecsphysics2d.Constants],
	) {
		constants.Get().Gravity = w.gravity
	})).Before[ecsphysics2d.IntegrateOnUpdate]()
	return nil
}

// newGravityHarness is the harness with a weigher composed beside the game,
// whose gravity starts at g.
func newGravityHarness(t testing.TB, config any, g m.Vec2d) (*harness, *weigher) {
	t.Helper()
	w := &weigher{gravity: g}
	return newHarnessWithPlugins(t, config, 1024, w), w
}

// The plugin registers Constants itself, with gravity 0, so an app that never
// writes it has a top-down world and pays nothing.
func TestTheConstantsAreRegisteredWithNoGravity(t *testing.T) {
	h := newHarnessWithPlugins(t, nil, 64, &readsConstants{})

	got := h.kernel.ExecuteCommand[constantsCmd](constantsRequest{})

	if got.Constants != (ecsphysics2d.Constants{}) {
		t.Errorf("the Constants start at %+v, want gravity 0", got.Constants)
	}
}

// readsConstants registers only the read-back command, so the test of the
// defaults composes no System that writes them.
type readsConstants struct{}

func (*readsConstants) Name() kernel.PluginName { return "physicstestreadsconstants" }

func (*readsConstants) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsphysics2d.Name}
}

func (*readsConstants) Register(registrar *kernel.Registrar, _ any) error {
	registerConstantsCmd(registrar)
	return nil
}

func registerConstantsCmd(registrar *kernel.Registrar) {
	registrar.HandleCommand[constantsCmd](ecs.ToExecute[constantsRequest, constantsResponse](registrar, func(
		_ constantsRequest,
		constants *ecs.Read[*ecsphysics2d.Constants],
		answer *ecs.Resp[constantsResponse],
	) {
		answer.Set(constantsResponse{Constants: *constants.Get()})
	}))
}

// The free fall both gravity tests run: a 2 kg Body with a moment of 8 thrown
// at (1.5, 3) m/s turning at 0.7 rad/s, damped at 1.5 /s for moving and for
// turning alike, which is cp's one global damping of e^−1.5. Two seconds at
// that rate leaves 5% of the throw, so the horizon is well past where damping
// decides the answer, and the Body is 94% of the way to its terminal speed.
var (
	fallGravity = m.Vec2d{X: 0.4, Y: -9.81}
	fallStart   = ecsphysics2d.Position{Current: m.Vec2d{X: 1, Y: 2}}
	fallThrow   = ecsphysics2d.Velocity{Linear: m.Vec2d{X: 1.5, Y: 3}, Angular: 0.7}
)

const (
	fallMass   = 2.0
	fallMoment = 8.0
	fallRate   = 1.5
	fallTicks  = 120
)

// cpFall is cp's trajectory for that Body, out of a throwaway harness importing
// github.com/jakecoffman/cp/v2 v2.4.0: a Space with SetGravity(0.4, −9.81) and
// SetDamping(e^−1.5), stepped at 1/60 s. Each row is the tick it was taken
// after, then where the Body stood and how fast it was going when Step
// returned.
var cpFall = []struct {
	tick int
	cpStep
}{
	{1, cpStep{1.0249999999999999, 2.0499999999999998, 0.011666666666666665, 1.4696315347091655, 2.7624297360849979, 0.68271693841983283}},
	{10, cpStep{1.2286603377694236, 2.3330613964379321, 0.10452200631261406, 1.2279280353571709, 0.87160108931886526, 0.54516054814998316}},
	{20, cpStep{1.4166956646733, 2.3483163294787572, 0.18592382667707213, 1.0160381762415607, -0.78599764900792701, 0.42457146179884292}},
	{30, cpStep{1.5730922013028041, 2.1160633399607622, 0.24931962812034997, 0.85101818803743479, -2.0769368444350049, 0.33065658691870969}},
	{40, cpStep{1.7048484232911951, 1.6910509865375813, 0.29869232792781392, 0.72250049200162747, -3.082321300731182, 0.25751560882000896}},
	{50, cpStep{1.8174147489419614, 1.1159174895606963, 0.3371438252002163, 0.62241080969040796, -3.8653155025824626, 0.20055335780213246}},
	{60, cpStep{1.9150359682979265, 0.4238695284286117, 0.36708988138623122, 0.54446088672906223, -4.475112000124609, 0.15619111210390033}},
	{70, cpStep{2.0010179271684154, -0.35923150893991584, 0.39041189339379995, 0.48375342568641011, -4.9500219899246325, 0.12164176041531109}},
	{80, cpStep{2.0779352208584432, -1.2132447533824455, 0.40857509460809516, 0.43647440748811567, -5.3198822618693242, 0.094734698265628442}},
	{90, cpStep{2.1477929462076482, -2.1224844802235294, 0.42272060993687177, 0.39965347109243682, -5.6079297312868519, 0.073779457193304657}},
	{100, cpStep{2.2121526742048729, -3.074734634802887, 0.43373714835187149, 0.37097729699405974, -5.8322613260309595, 0.057459499036728819}},
	{110, cpStep{2.2722305575590518, -4.0604813441850478, 0.44231683709620956, 0.34864427015075172, -6.0069709476853275, 0.044749502844695026}},
	{120, cpStep{2.3289737369522361, -5.0723151966778257, 0.44899870540880893, 0.33125129135682879, -6.1430349378398592, 0.034850947857504519}},
}

// spawnFall puts the free-fall Body into h.
func spawnFall(t *testing.T, h *harness) ecs.Entity {
	t.Helper()
	return h.spawn(t, spawnRequest{
		Kind:     kindDynamic,
		Place:    fallStart,
		Velocity: fallThrow,
		Body:     dynamic(t, fallMass, fallMoment, fallRate, fallRate),
	})
}

// wantFall runs the fall for fallTicks and compares every frozen row. With no
// Contact there is no bias, so position and velocity compare tick for tick:
// both engines integrate positions first and velocities after, in the one step.
func wantFall(t *testing.T, h *harness, body ecs.Entity) {
	t.Helper()
	const tolerance = 1e-9
	row := 0
	for k := 1; k <= fallTicks; k++ {
		h.frame(t)
		if row >= len(cpFall) || cpFall[row].tick != k {
			continue
		}
		got, want := h.read(t, body), cpFall[row].cpStep
		row++
		if math.Abs(got.Velocity.Linear.X-want.vx) > tolerance ||
			math.Abs(got.Velocity.Linear.Y-want.vy) > tolerance ||
			math.Abs(got.Velocity.Angular-want.angular) > tolerance {
			t.Errorf("tick %d velocity is (%.17g, %.17g) w %.17g, want cp's (%.17g, %.17g) w %.17g",
				k, got.Velocity.Linear.X, got.Velocity.Linear.Y, got.Velocity.Angular,
				want.vx, want.vy, want.angular)
		}
		if math.Abs(got.Place.Current.X-want.x) > tolerance ||
			math.Abs(got.Place.Current.Y-want.y) > tolerance ||
			math.Abs(got.Place.Angle-want.angle) > tolerance {
			t.Errorf("tick %d the Body is at (%.17g, %.17g) a %.17g, want cp's (%.17g, %.17g) a %.17g",
				k, got.Place.Current.X, got.Place.Current.Y, got.Place.Angle, want.x, want.y, want.angle)
		}
	}
	if row != len(cpFall) {
		t.Fatalf("compared %d of cp's %d rows", row, len(cpFall))
	}
}

// A Body falling freely under Gravity falls exactly as cp's BodyUpdateVelocity
// makes it fall, with the same gravity, damping and step, over a horizon long
// enough for damping to decide the answer. The angular velocity only decays:
// gravity has no angular term.
func TestABodyFallsUnderGravityExactlyAsChipmunkDoes(t *testing.T) {
	h, _ := newGravityHarness(t, nil, fallGravity)
	body := spawnFall(t, h)

	wantFall(t, h, body)
}

// Gravity 0 with m·g written into Force every tick is the same fall, so an app
// that keeps writing its own gravity as a Force is unaffected by the term.
func TestGravityWrittenAsAForceFallsTheSameAsGravity(t *testing.T) {
	h := newHarnessWith(t, nil, 64)
	body := spawnFall(t, h)
	h.game.push = fallGravity.MulS(fallMass)

	wantFall(t, h, body)
}

// Gravity is cp's term in the velocity integrator, which only Dynamic bodies
// reach: a Kinematic body keeps the Velocity the app gave it and a Static body
// has none to change.
func TestGravityMovesNeitherAKinematicNorAStaticBody(t *testing.T) {
	h, _ := newGravityHarness(t, nil, fallGravity)
	kinematic := h.spawn(t, spawnRequest{
		Kind:     kindKinematic,
		Place:    ecsphysics2d.Position{Current: m.Vec2d{X: 3}},
		Velocity: ecsphysics2d.Velocity{Linear: m.Vec2d{X: 2}},
	})
	static := h.spawn(t, spawnRequest{
		Kind:  kindStatic,
		Place: ecsphysics2d.Position{Current: m.Vec2d{X: -3, Y: 1}},
	})

	h.frames(t, 30)

	k := h.read(t, kinematic)
	if k.Velocity.Linear != (m.Vec2d{X: 2}) || k.Velocity.Angular != 0 {
		t.Errorf("the Kinematic body's Velocity is %+v after 30 ticks of gravity, want (2, 0) unchanged",
			k.Velocity)
	}
	if want := 3 + 30*2*tick; !near(k.Place.Current.X, want) || k.Place.Current.Y != 0 {
		t.Errorf("the Kinematic body is at %v, want (%v, 0): its own Velocity and nothing else",
			k.Place.Current, want)
	}
	if s := h.read(t, static); s.Place.Current != (m.Vec2d{X: -3, Y: 1}) {
		t.Errorf("the Static body is at %v after 30 ticks of gravity, want (-3, 1)", s.Place.Current)
	}
}

// A System writing a new Gravity between ticks changes the fall on the very
// next tick: Solve reads Constants every tick, so there is nothing to refresh
// and nothing cached from the tick before. Undamped, so the change is the whole
// of the difference.
func TestANewGravityChangesTheFallOnTheNextTick(t *testing.T) {
	h, w := newGravityHarness(t, nil, m.Vec2d{Y: -9.81})
	body := h.spawn(t, spawnRequest{Kind: kindDynamic, Body: dynamic(t, 2, 8, 0, 0)})

	h.frames(t, 10)
	before := h.read(t, body)
	if want := -9.81 * 10 * tick; !near(before.Velocity.Linear.Y, want) {
		t.Fatalf("after 10 ticks at g = 9.81 the Body falls at %v, want %v", before.Velocity.Linear.Y, want)
	}

	w.gravity = m.Vec2d{X: 5, Y: 1.62}
	h.frame(t)

	after := h.read(t, body)
	want := before.Velocity.Linear.Add(m.Vec2d{X: 5, Y: 1.62}.MulS(tick))
	if !near(after.Velocity.Linear.X, want.X) || !near(after.Velocity.Linear.Y, want.Y) {
		t.Errorf("the tick after Gravity changed the Body moves at %v, want %v", after.Velocity.Linear, want)
	}
	got := h.kernel.ExecuteCommand[constantsCmd](constantsRequest{})
	if got.Constants.Gravity != (m.Vec2d{X: 5, Y: 1.62}) {
		t.Errorf("the Constants hold %v, want the gravity the System wrote", got.Constants.Gravity)
	}
}
