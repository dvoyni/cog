package internal

import (
	"errors"
	"sync"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
)

// Everything here runs against a real kernel.Engine with the real ecs and the
// real app plugin composed beside physics, and reads back what the Components
// hold after a tick. The plugin is judged by what a Body does over the ticks it
// is published, not by what its Systems look like.

// tick is the fixed step every test publishes at, which is the rate every
// number quoted in the spec is quoted at.
const tick = 1.0 / 60

// The four Component sets a Body is spawned from. Which set an Entity carries
// is the whole of what says which kind of Body it is: Dynamic present is
// Dynamic, a Velocity with no Dynamic is Kinematic, and the Static Tag is
// Static and carries no Velocity at all.
type (
	dynamicBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Force    ecsphysics2d.Force
		Body     ecsphysics2d.Dynamic
	}
	// forcelessBody is the one trap in the Component set: a Dynamic body with
	// no Force falls out of the velocity integrator's Query and never moves.
	forcelessBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Body     ecsphysics2d.Dynamic
	}
	kinematicBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
	}
	staticBody struct {
		Place  ecsphysics2d.Position
		Marker ecsphysics2d.Static
	}
	// shapedBody is a Dynamic Body carrying a Shape, which is the whole of what
	// puts it in BodyIndex: Index rebuilds that index from these every tick.
	shapedBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Force    ecsphysics2d.Force
		Body     ecsphysics2d.Dynamic
		Shape    ecsphysics2d.Shape
	}
	// shapedStatic is static geometry as an app spawns it: the Shape, the
	// Position and the Tag in one Spawn, so the Shape hook and the Position
	// reach Index together.
	shapedStatic struct {
		Place  ecsphysics2d.Position
		Marker ecsphysics2d.Static
		Shape  ecsphysics2d.Shape
	}
	// placelessStatic is the trap the drain states rather than checks: a Static
	// with a Shape and no Position at all. Its hook is drained once, finds no
	// Position, and the Entity never enters the index.
	placelessStatic struct {
		Marker ecsphysics2d.Static
		Shape  ecsphysics2d.Shape
	}
	// polygonBody and polygonStatic are the two sets a Shape of kind ShapePoly
	// is spawned from: the Polygon Component rides on the same Entity, which is
	// where Index reads the vertices from. An inline kind may be spawned from
	// them too, carrying the zero Polygon, which is harmless.
	polygonBody struct {
		Place    ecsphysics2d.Position
		Velocity ecsphysics2d.Velocity
		Force    ecsphysics2d.Force
		Body     ecsphysics2d.Dynamic
		Shape    ecsphysics2d.Shape
		Polygon  ecsphysics2d.Polygon
	}
	polygonStatic struct {
		Place   ecsphysics2d.Position
		Marker  ecsphysics2d.Static
		Shape   ecsphysics2d.Shape
		Polygon ecsphysics2d.Polygon
	}
	// jointEntity is a Joint as an app spawns one: an Entity of its own
	// carrying nothing but the Joint, which holds its two Bodies by Reference.
	jointEntity struct {
		Joint ecsphysics2d.Joint
	}
)

// bodyKind names which of the sets a spawn carries.
type bodyKind int

const (
	kindDynamic bodyKind = iota
	kindForceless
	kindKinematic
	kindStatic
	kindShapedBody
	kindShapedStatic
	kindPlacelessStatic
	kindPolygonBody
	kindPolygonStatic
	kindJoint
)

// spawnCmd creates Bodies. It is a System registered as a command, which is how
// a test reaches a structural change from outside a tick.
type spawnCmd kernel.Command[spawnRequest, spawnResponse]

type spawnRequest struct {
	Kind     bodyKind
	Count    int
	Place    ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
	Body     ecsphysics2d.Dynamic
	Shape    ecsphysics2d.Shape
	Polygon  ecsphysics2d.Polygon
	Joint    ecsphysics2d.Joint
}

type spawnResponse struct{ First ecs.Entity }

func spawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[spawnRequest, spawnResponse]) {
	return ecs.ToExecute[spawnRequest, spawnResponse](registrar, func(
		request spawnRequest,
		dynamics *ecs.Spawn[dynamicBody],
		forceless *ecs.Spawn[forcelessBody],
		kinematics *ecs.Spawn[kinematicBody],
		statics *ecs.Spawn[staticBody],
		shaped *ecs.Spawn[shapedBody],
		shapedStatics *ecs.Spawn[shapedStatic],
		placeless *ecs.Spawn[placelessStatic],
		polygons *ecs.Spawn[polygonBody],
		polygonStatics *ecs.Spawn[polygonStatic],
		joints *ecs.Spawn[jointEntity],
		answer *ecs.Resp[spawnResponse],
	) {
		var first ecs.Entity
		for i := range max(request.Count, 1) {
			var e ecs.Entity
			switch request.Kind {
			case kindDynamic:
				e = dynamics.New(dynamicBody{Place: request.Place, Velocity: request.Velocity, Body: request.Body})
			case kindForceless:
				e = forceless.New(forcelessBody{Place: request.Place, Velocity: request.Velocity, Body: request.Body})
			case kindKinematic:
				e = kinematics.New(kinematicBody{Place: request.Place, Velocity: request.Velocity})
			case kindStatic:
				e = statics.New(staticBody{Place: request.Place})
			case kindShapedBody:
				e = shaped.New(shapedBody{
					Place: request.Place, Velocity: request.Velocity,
					Body: request.Body, Shape: request.Shape,
				})
			case kindShapedStatic:
				e = shapedStatics.New(shapedStatic{Place: request.Place, Shape: request.Shape})
			case kindPlacelessStatic:
				e = placeless.New(placelessStatic{Shape: request.Shape})
			case kindPolygonBody:
				e = polygons.New(polygonBody{
					Place: request.Place, Velocity: request.Velocity,
					Body: request.Body, Shape: request.Shape, Polygon: request.Polygon,
				})
			case kindPolygonStatic:
				e = polygonStatics.New(polygonStatic{
					Place: request.Place, Shape: request.Shape, Polygon: request.Polygon,
				})
			case kindJoint:
				e = joints.New(jointEntity{Joint: request.Joint})
			}
			if i == 0 {
				first = e
			}
		}
		answer.Set(spawnResponse{First: first})
	})
}

// readCmd reads one Body's three Components back, which is how a test sees what
// a tick did without holding a lock of its own across one.
type readCmd kernel.Command[readRequest, readResponse]

type readRequest struct{ Entity ecs.Entity }

type readResponse struct {
	Place    ecsphysics2d.Position
	Velocity ecsphysics2d.Velocity
	Force    ecsphysics2d.Force
	HasForce bool
}

func readCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[readRequest, readResponse]) {
	return ecs.ToExecute[readRequest, readResponse](registrar, func(
		request readRequest,
		places *ecs.Get[ecsphysics2d.Position],
		velocities *ecs.Get[ecsphysics2d.Velocity],
		forces *ecs.Get[ecsphysics2d.Force],
		answer *ecs.Resp[readResponse],
	) {
		var reply readResponse
		reply.Place, _ = places.Of(request.Entity)
		reply.Velocity, _ = velocities.Of(request.Entity)
		reply.Force, reply.HasForce = forces.Of(request.Entity)
		answer.Set(reply)
	})
}

// shapeCmd gives an Entity a Shape or takes one away after it has been spawned,
// which is how a test reaches the Hook records the drain reads without spawning
// a whole Entity for each.
type shapeCmd kernel.Command[shapeRequest, shapeResponse]

type shapeRequest struct {
	Entity ecs.Entity
	Shape  ecsphysics2d.Shape
	// Drop takes the Shape away instead of writing one.
	Drop bool
}

type shapeResponse struct{ Dropped bool }

func shapeCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[shapeRequest, shapeResponse]) {
	return ecs.ToExecute[shapeRequest, shapeResponse](registrar, func(
		request shapeRequest,
		shapes *ecs.Set[ecsphysics2d.Shape],
		drop *ecs.Remove[ecsphysics2d.Shape],
		answer *ecs.Resp[shapeResponse],
	) {
		if request.Drop {
			answer.Set(shapeResponse{Dropped: drop.From(request.Entity)})
			return
		}
		shapes.UpdateFor(request.Entity, request.Shape)
		answer.Set(shapeResponse{})
	})
}

// placeCmd writes one Entity's Position from outside a tick, which is what an
// app does to teleport a Body — and, for a Static, what the spec says does not
// move it, because a Static is world-cached at insert and never again.
type placeCmd kernel.Command[placeRequest, placeResponse]

type placeRequest struct {
	Entity ecs.Entity
	Place  ecsphysics2d.Position
}

type placeResponse struct{}

func placeCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[placeRequest, placeResponse]) {
	return ecs.ToExecute[placeRequest, placeResponse](registrar, func(
		request placeRequest,
		places *ecs.Set[ecsphysics2d.Position],
		answer *ecs.Resp[placeResponse],
	) {
		places.UpdateFor(request.Entity, request.Place)
		answer.Set(placeResponse{})
	})
}

// indexCmd asks the two indices what they hold. It reaches them the way any
// app System does — ecs.Read of each Resource, separately — which is also what
// shows the two locks really are apart.
type indexCmd kernel.Command[indexRequest, indexResponse]

type indexRequest struct {
	// At and Radius are the circle both indices are Overlapped with.
	At     m.Vec2d
	Radius float64
}

type indexResponse struct {
	StaticLen, BodyLen int
	Statics, Bodies    []ecs.Entity
}

func indexCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[indexRequest, indexResponse]) {
	return ecs.ToExecute[indexRequest, indexResponse](registrar, func(
		request indexRequest,
		statics *ecs.Read[*ecsphysics2d.StaticIndex],
		bodies *ecs.Read[*ecsphysics2d.BodyIndex],
		answer *ecs.Resp[indexResponse],
	) {
		static, body := statics.Get(), bodies.Get()
		probe := ecsphysics2d.NewCircleShape(request.Radius, m.Vec2d{})
		all := ecsphysics2d.CollisionBitsAll
		answer.Set(indexResponse{
			StaticLen: static.Len(),
			BodyLen:   body.Len(),
			Statics:   static.Overlap(nil, probe, request.At, 0, nil, all, all, ecs.NoEntity),
			Bodies:    body.Overlap(nil, probe, request.At, 0, nil, all, all, ecs.NoEntity),
		})
	})
}

// contactsCmd reads the tick's Contact list back, which is how a test sees what
// Detect wrote and what Solve did to it. The list persists until the next
// Detect, so reading it between ticks is what an app's own System ordered
// before Integrate would legally see.
type contactsCmd kernel.Command[contactsRequest, contactsResponse]

type contactsRequest struct{}

type contactsResponse struct {
	Contacts []ecsphysics2d.Contact
}

func contactsCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[contactsRequest, contactsResponse]) {
	return ecs.ToExecute[contactsRequest, contactsResponse](registrar, func(
		_ contactsRequest,
		contacts *ecs.Read[*ecsphysics2d.Contacts],
		answer *ecs.Resp[contactsResponse],
	) {
		answer.Set(contactsResponse{
			Contacts: append([]ecsphysics2d.Contact(nil), contacts.Get().All()...),
		})
	})
}

// despawnCmd retires an Entity, which is what makes an Ended entry name a Body
// no Store holds any more.
type despawnCmd kernel.Command[despawnRequest, despawnResponse]

type despawnRequest struct{ Entity ecs.Entity }

type despawnResponse struct{}

func despawnCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[despawnRequest, despawnResponse]) {
	return ecs.ToExecute[despawnRequest, despawnResponse](registrar, func(
		request despawnRequest,
		entities *ecs.WriteableEntities,
		answer *ecs.Resp[despawnResponse],
	) {
		entities.Despawn(request.Entity)
		answer.Set(despawnResponse{})
	})
}

// jointCmd reads one Joint back and, when asked, replaces it: the Impulse the
// last tick delivered and the ratchet's Angle are what a test looks at, and
// replacing one is how a test changes a motor's rate between ticks.
type jointCmd kernel.Command[jointRequest, jointResponse]

type jointRequest struct {
	Entity ecs.Entity
	Joint  ecsphysics2d.Joint
	// Replace writes Joint over whatever the Entity holds.
	Replace bool
}

type jointResponse struct {
	Joint ecsphysics2d.Joint
	Found bool
}

func jointCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[jointRequest, jointResponse]) {
	return ecs.ToExecute[jointRequest, jointResponse](registrar, func(
		request jointRequest,
		joints *ecs.Set[ecsphysics2d.Joint],
		answer *ecs.Resp[jointResponse],
	) {
		if request.Replace {
			joints.UpdateFor(request.Entity, request.Joint)
		}
		joint, found := joints.Of(request.Entity)
		answer.Set(jointResponse{Joint: joint, Found: found})
	})
}

// jointedCmd asks the JointedPairs Resource what Index built, which is how a
// test sees the set Detect checks without going through a Contact.
type jointedCmd kernel.Command[jointedRequest, jointedResponse]

type jointedRequest struct{ A, B ecs.Entity }

type jointedResponse struct {
	Len int
	Has bool
}

func jointedCmdImpl(registrar *kernel.Registrar) func() (kernel.Lock, kernel.Execute[jointedRequest, jointedResponse]) {
	return ecs.ToExecute[jointedRequest, jointedResponse](registrar, func(
		request jointedRequest,
		pairs *ecs.Read[*ecsphysics2d.JointedPairs],
		answer *ecs.Resp[jointedResponse],
	) {
		set := pairs.Get()
		answer.Set(jointedResponse{Len: set.Len(), Has: set.Has(request.A, request.B)})
	})
}

// filterOnUpdate is the app's filter System — cp's Begin and PreSolve — ordered
// into the one gap the specification puts it in, and before the sleep System,
// which is cp's own order: PreSolve runs before ProcessComponents. Left
// unordered against it, the two write the Contact list in whichever order the
// scheduler meets them, and a tick on which the sleep System is the one kept
// waiting costs the kernel's dispatch a map for its wide lock set — an
// allocation the allocation-line tests would see as noise.
type filterOnUpdate kernel.Subscription[app.UpdateEvent]

// pushQuery is the gameplay write that Force exists for: a System of the app's,
// ordered Before[IntegrateOnUpdate], adding this tick's Force to every Body
// that can take one.
type pushQuery struct {
	Force *ecsphysics2d.Force
}

// pushOnUpdate is the game's own ordering identity for that System.
type pushOnUpdate kernel.Subscription[app.UpdateEvent]

// The probe Systems the chain is read off. Each is sandwiched between two of
// the plugin's identities, so the order they run in is the order the four they
// sit between run in: Integrate < first < Index < second < Detect < third <
// Solve < fourth. The sleep System runs inside the third gap, between Detect
// and Solve, and the recorder there is unordered against it.
type (
	afterIntegrateOnUpdate kernel.Subscription[app.UpdateEvent]
	afterIndexOnUpdate     kernel.Subscription[app.UpdateEvent]
	afterDetectOnUpdate    kernel.Subscription[app.UpdateEvent]
	afterSolveOnUpdate     kernel.Subscription[app.UpdateEvent]
)

// game stands in for the app: it spawns Bodies, writes Force, reads Components
// back, and records the order the plugin's Systems ran in.
//
// push is written only between ticks, and kernel.PublishEvent(…).Wait() is the
// happens-before edge that makes that safe: no test writes it while a tick that
// reads it is in flight.
type game struct {
	push m.Vec2d
	// torque is the angular half of the same write.
	torque float64

	// filter is the app's filter System's body, written between ticks like push
	// and run over every entry the tick found. A nil one marks nothing.
	filter func(entry *ecsphysics2d.Contact)

	mu    sync.Mutex
	order []string
}

func (g *game) Name() kernel.PluginName { return "physicstestgame" }

func (g *game) Dependencies() []kernel.PluginName {
	return []kernel.PluginName{ecs.Name, ecsphysics2d.Name}
}

func (g *game) Register(registrar *kernel.Registrar, _ any) error {
	registrar.HandleCommand[spawnCmd](spawnCmdImpl(registrar))
	registrar.HandleCommand[readCmd](readCmdImpl(registrar))
	registrar.HandleCommand[shapeCmd](shapeCmdImpl(registrar))
	registrar.HandleCommand[placeCmd](placeCmdImpl(registrar))
	registrar.HandleCommand[indexCmd](indexCmdImpl(registrar))
	registrar.HandleCommand[contactsCmd](contactsCmdImpl(registrar))
	registrar.HandleCommand[despawnCmd](despawnCmdImpl(registrar))
	registrar.HandleCommand[jointCmd](jointCmdImpl(registrar))
	registrar.HandleCommand[jointedCmd](jointedCmdImpl(registrar))
	registrar.Subscribe[pushOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[pushQuery]) {
		for _, it := range q.All() {
			it.Force.Force = it.Force.Force.Add(g.push)
			it.Force.Torque += g.torque
		}
	})).Before[ecsphysics2d.IntegrateOnUpdate]()

	registrar.Subscribe[filterOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
		contacts *ecs.Write[*ecsphysics2d.Contacts],
	) {
		if g.filter == nil {
			return
		}
		list := contacts.Get().All()
		for i := range list {
			g.filter(&list[i])
		}
	})).After[ecsphysics2d.DetectOnUpdate]().Before[ecsphysics2d.SleepOnUpdate]()

	g.probe(registrar)
	return nil
}

// probe registers the four recorders, each ordered into one gap of the chain.
func (g *game) probe(registrar *kernel.Registrar) {
	record := func(name string) func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
		return func() (kernel.Lock, kernel.Observe[app.UpdateEvent]) {
			return nil, func(kernel.Kernel, app.UpdateEvent) {
				g.mu.Lock()
				defer g.mu.Unlock()
				g.order = append(g.order, name)
			}
		}
	}
	registrar.Subscribe[afterIntegrateOnUpdate](record("integrate")).
		After[ecsphysics2d.IntegrateOnUpdate]().Before[ecsphysics2d.IndexOnUpdate]()
	registrar.Subscribe[afterIndexOnUpdate](record("index")).
		After[ecsphysics2d.IndexOnUpdate]().Before[ecsphysics2d.DetectOnUpdate]()
	registrar.Subscribe[afterDetectOnUpdate](record("detect")).
		After[ecsphysics2d.DetectOnUpdate]().Before[ecsphysics2d.SolveOnUpdate]()
	registrar.Subscribe[afterSolveOnUpdate](record("solve")).
		After[ecsphysics2d.SolveOnUpdate]()
}

// ran is the order the recorders saw, as a fresh copy.
func (g *game) ran() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.order...)
}

// harness is physics composed with app, ecs and the game, running in a real
// engine.
type harness struct {
	kernel kernel.Executioner
	game   *game
	plugin *plugin
}

func newHarness(t testing.TB) *harness { return newHarnessWith(t, nil, 1024) }

// newHarnessWith composes the whole engine: app because the step is
// app.UpdateEvent's, ecs because the Components live in its Stores, physics
// because it is what is being tested, and the game standing in for the app.
func newHarnessWith(t testing.TB, config any, ids uint32) *harness {
	t.Helper()
	return newHarnessWithPlugins(t, config, ids)
}

// newHarnessWithPlugins is newHarnessWith with more of the app's plugins
// composed beside the game, for the tests that need a System of their own in
// the frame and must not put it in every other test's.
func newHarnessWithPlugins(t testing.TB, config any, ids uint32, extra ...kernel.Plugin) *harness {
	t.Helper()
	configs := map[kernel.PluginName]any{ecs.Name: ecs.Config{PrewarmEntities: ids}}
	if config != nil {
		configs[ecsphysics2d.Name] = config
	}
	physics, world := New().(*plugin), &game{}
	var failure error
	engine := kernel.New(configs).
		Handler(func(err error) error { failure = err; return nil }).
		WithPlugins(append([]kernel.Plugin{appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), physics, world}, extra...)...)
	stopped := make(chan struct{})
	t.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	<-engine.Ready()
	if failure != nil {
		t.Fatalf("composing the engine: %v", failure)
	}
	k := engine.Executioner()
	k.PublishEvent(app.InitEvent{}).Wait()
	return &harness{kernel: k, game: world, plugin: physics}
}

// frame publishes one real app.UpdateEvent and waits for it: publish, acquire
// every declared lock, run every System, wait.
func (h *harness) frame(t testing.TB) {
	t.Helper()
	h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
}

func (h *harness) frames(t testing.TB, n int) {
	t.Helper()
	for range n {
		h.frame(t)
	}
}

func (h *harness) spawn(t testing.TB, request spawnRequest) ecs.Entity {
	t.Helper()
	response := h.kernel.ExecuteCommand[spawnCmd](request)
	return response.First
}

func (h *harness) read(t testing.TB, e ecs.Entity) readResponse {
	t.Helper()
	response := h.kernel.ExecuteCommand[readCmd](readRequest{Entity: e})
	return response
}

// setShape writes an Entity's Shape from outside a tick, and dropShape takes it
// away. Each is one act the drain sees as a Hook record on its next run.
func (h *harness) setShape(t testing.TB, e ecs.Entity, shape ecsphysics2d.Shape) {
	t.Helper()
	h.kernel.ExecuteCommand[shapeCmd](shapeRequest{Entity: e, Shape: shape})
}

func (h *harness) dropShape(t testing.TB, e ecs.Entity) {
	t.Helper()
	response := h.kernel.ExecuteCommand[shapeCmd](shapeRequest{Entity: e, Drop: true})
	if !response.Dropped {
		t.Fatalf("%v had no Shape to take away", e)
	}
}

// place writes an Entity's Position from outside a tick.
func (h *harness) place(t testing.TB, e ecs.Entity, at m.Vec2d) {
	t.Helper()
	h.kernel.ExecuteCommand[placeCmd](placeRequest{
		Entity: e, Place: ecsphysics2d.Position{Current: at},
	})
}

// indexed is what both indices hold, and which Entities each finds under a
// circle of that radius at that point.
func (h *harness) indexed(t testing.TB, at m.Vec2d, radius float64) indexResponse {
	t.Helper()
	response := h.kernel.ExecuteCommand[indexCmd](indexRequest{At: at, Radius: radius})
	return response
}

// contacts is the tick's Contact list as the app sees it.
func (h *harness) contacts(t testing.TB) []ecsphysics2d.Contact {
	t.Helper()
	response := h.kernel.ExecuteCommand[contactsCmd](contactsRequest{})
	return response.Contacts
}

// despawn retires an Entity from outside a tick.
func (h *harness) despawn(t testing.TB, e ecs.Entity) {
	t.Helper()
	h.kernel.ExecuteCommand[despawnCmd](despawnRequest{Entity: e})
}

// joint reads one Joint back as it stands now.
func (h *harness) joint(t testing.TB, e ecs.Entity) jointResponse {
	t.Helper()
	response := h.kernel.ExecuteCommand[jointCmd](jointRequest{Entity: e})
	return response
}

// setJoint replaces one Joint from outside a tick.
func (h *harness) setJoint(t testing.TB, e ecs.Entity, joint ecsphysics2d.Joint) {
	t.Helper()
	h.kernel.ExecuteCommand[jointCmd](jointRequest{
		Entity: e, Joint: joint, Replace: true,
	})
}

// jointedPairs is what Index built out of the Joint walk.
func (h *harness) jointedPairs(t testing.TB, a, b ecs.Entity) jointedResponse {
	t.Helper()
	response := h.kernel.ExecuteCommand[jointedCmd](jointedRequest{A: a, B: b})
	return response
}

// circle is the Shape every index test spawns with: the smallest thing that
// carries a place and a radius, since Polygons are a later ticket.
func circle(radius float64) ecsphysics2d.Shape {
	return ecsphysics2d.NewCircleShape(radius, m.Vec2d{})
}

// dynamic is the Dynamic a test spawns with, built through the constructor so
// that every test runs against a Body an app could have built.
func dynamic(t testing.TB, mass, moment, damping, angularDamping float64) ecsphysics2d.Dynamic {
	t.Helper()
	body, err := ecsphysics2d.NewDynamic(mass, moment, damping, angularDamping)
	if err != nil {
		t.Fatalf("NewDynamic(%v, %v, %v, %v): %v", mass, moment, damping, angularDamping, err)
	}
	return body
}

// moverQuery is a game System's write of a Body's Position, which is what an
// app does to place or teleport one. Locking that Store is what makes the
// coupling check bite.
type moverQuery struct {
	Place *ecsphysics2d.Position
}

type moverOnUpdate kernel.Subscription[app.UpdateEvent]

// mover is a game that names the plugin's Components through its root alone,
// and declares whichever dependencies the test hands it.
type mover struct{ deps []kernel.PluginName }

func (*mover) Name() kernel.PluginName             { return "physicstestmover" }
func (p *mover) Dependencies() []kernel.PluginName { return p.deps }

func (*mover) Register(registrar *kernel.Registrar, _ any) error {
	registrar.Subscribe[moverOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(q *ecs.Query[moverQuery]) {
		for _, it := range q.All() {
			it.Place.Current = it.Place.Current.Add(m.Vec2d{X: 1})
		}
	})).Before[ecsphysics2d.IntegrateOnUpdate]()
	return nil
}

// composeWithMover builds physics' engine with a mover declaring deps beside
// it, and returns every error composition reported.
func composeWithMover(deps []kernel.PluginName) error {
	var failure error
	kernel.New(map[kernel.PluginName]any{}).
		Handler(func(err error) error { failure = errors.Join(failure, err); return nil }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(), New(), &mover{deps: deps})
	return failure
}

func near(got, want float64) bool { return abs(got-want) <= 1e-12 }

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
