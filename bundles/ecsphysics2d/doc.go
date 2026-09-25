// Package ecsphysics2d gives an app 2D rigid-body physics on a plane.
//
// It is a port of Chipmunk, not a design of its own: cp's algorithms are
// transliterated and what changes is the data layout, from cp's pointer graph
// to cog's Components, Resources and Systems. Like cp, it computes in float64.
// The specification the implementation is judged against is
// docs/specs/ecsphysics2d.md.
//
// A departure from cp is a finding, stated where it is made, and only four
// reasons are acceptable: the ECS layout in place of a pointer graph, zero
// allocations, per-second units, or a defect in cp.
//
// # Licences
//
// The port takes from two MIT sources and keeps both notices:
//
//	github.com/jakecoffman/cp v2.4.0
//	Copyright (c) 2017 Jake Coffman
//
//	Chipmunk2D f2f3d66
//	Copyright (c) 2007-2015 Scott Lembcke and Howling Moon Software
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//
// Box2D v3 and neguse/gox2d were read as references only and nothing is taken
// from them.
//
// # What is here so far
//
// The value types everything else is written over: Transform and BB in
// types.go, and the moment and area helpers in utils.go. The vector is m.Vec2d
// and lives in libs/m, because every Component and query exposes it to gameplay
// code and a physics-owned vector would make gameplay import physics for vector
// maths.
//
// And a Body that moves: Position, Velocity, Force, Dynamic and the Static Tag,
// with the five Systems the step is chained on app.UpdateEvent in cp's own
// order — IntegrateOnUpdate, IndexOnUpdate, DetectOnUpdate, SleepOnUpdate,
// SolveOnUpdate. Integrate moves every Body with a Velocity, Index keeps the two
// indices current, Detect writes the tick's Contacts, the sleep System puts
// idle Islands to sleep and wakes disturbed ones, and Solve drives the Contacts
// apart.
//
// # The step closes for circles
//
// Contacts is the tick's Contact list, one entry per touching pair, marked
// Began, Continuing or Ended and always on. Detect finds circle-against-circle
// and circle-against-segment through the two indices as cp's closed forms, and
// Solve runs cp's impulse solver over what a filter left: the dense solved
// list, the gather through the BodyIndex slot table, PreStep, the velocity
// integration, the warm start, the Iterations passes, and the de-penetration
// bias applied as a position delta before it returns.
//
// Solve is indivisible, and both sides force it. PreStep computes the bounce
// from the velocity before integration, which is what stops gravity-fed jitter
// from eating Restitution, and the warm start must follow the damping, or the
// cached Impulse is damped away before it does anything.
//
// Friction and Restitution are plain Shape fields, combined into the Contact
// entry by Detect as the plain products u = ua·ub and e = ea·eb and writable by
// a filter for one tick. Both default to zero, as cp's do, which is why sliding
// along a wall at the defaults is exactly v ← v − (v·n)·n and is the solver's
// own behaviour rather than a rule of its own. Surface velocity is on the entry
// too, zero from Detect and written by a filter, as the next section shows.
// Joints arrive with theirs.
//
// # Conveyors
//
// A Shape has no surface velocity. A belt is a Component of the app's own on
// the belt's Entity, holding the velocity its surface moves at, and a filter
// System ordered After[DetectOnUpdate]().Before[SolveOnUpdate]() walks
// Contacts and, for each Contact with a belt on either side, writes
// SurfaceVelocity: A's surface velocity less B's, a party with no belt counting
// as zero, with its normal component removed.
//
//	surface := beltOf(entry.A).Sub(beltOf(entry.B))
//	entry.SurfaceVelocity = surface.Sub(entry.Normal.MulS(surface.Dot(entry.Normal)))
//
// That is cp's surface_vr in this port's A/B convention: cp's b − a, the port's
// A playing cp's b. The normal component must go because Solve adds the entry
// to the pair's relative velocity before splitting it into normal and tangent,
// so what is left along the Normal would push the pair apart or pull it
// together; only the tangent carries.
//
// The carrying is friction, bounded by the same Coulomb clamp, so a belt with
// no Friction carries nothing. The entry's Friction is the product of the two
// Shapes', so the belt's Shape needs a Friction, and so does whatever rides on
// it, or the filter writes the entry's Friction itself.
//
// A scene with belts pays for them in its own System and a scene without pays
// nothing; the filter reads the app's Component through ecs.Get and takes the
// Contacts write every filter takes. The recipe's proof, in
// internal/material_test.go, is
// TestAFilterWritesASurfaceVelocityAndTheContactCarriesTheBodyAlongIt.
//
// # Which pairs collide, and what keeps a point from tunnelling
//
// A pair collides when a.CollisionBits & b.CollidesWith and b.CollisionBits &
// a.CollidesWith are both non-zero, which is cp's ShapeFilter.Reject without
// cp's Group, and it is decided before any Shape test. There are 32 groups, one
// per bit of a uint32. Zero in either field means nothing collides, as in cp,
// and every constructor sets both fields to every bit, so a Shape built the
// normal way collides with everything — while a Shape written as a bare literal
// collides with nothing, which is a hazard the package documents rather than
// guards. Querying as a Shape collides is passing that Shape's two fields, so a
// Shape that collides with nothing is invisible to queries too; queries do not
// skip Sensors, which departs from cp, because Overlap has to be able to find
// one and the groups already say the skip when an app wants it.
//
// The plugin has no collision configuration at all: no matrix, no rule list and
// nothing to change at runtime. A Body changes what it collides with by writing
// its own Shape, which Index picks up on the next tick; a Static body is
// replaced instead. A rule that depends on the pair rather than the categories
// — owner exclusion, a line-of-sight gate — stays in the app's own filter
// System, so there are two filtering mechanisms by design.
//
// The package has no projectile concept either. A projectile is an ordinary
// Dynamic body with a circle Shape marked a Sensor, and the same mechanism
// serves pressure plates and area damage. Every moving Sensor, whatever its
// Shape, is Probed once a tick, from Position.Previous to Current, with no
// opt-in flag: its entries are its Entity plus a Hit, sitting together in the
// list and ordered by T, so the app takes the first and stops at a wall. A
// Shape that is not a circle is held at its end angle. Two moving Sensors meet
// along their relative motion, as one entry with one T, and a Static Sensor is
// never Probed; a fast solid Body reports every Sensor that did not move which
// its path crosses, a Dynamic one up to where it stopped and a Kinematic one,
// never stopped, all along its path. T and Depth mean one thing on every
// entry: a Probed Sensor carries the Probe's T and a Depth of 0, except one
// that started inside something, which reports T = 0 with the overlap at the
// start, and everything else reports T = 1 with the overlap where the tick
// ended — except around a solid Body that was stopped short.
//
// A solid Body does not tunnel either. One that moves at least its own
// thinnest width in a tick is Probed along its path, its Shape held at its end
// angle, and stopped where it first meets a Body it collides with, of any
// kind, that it was not already touching when the tick began; a Sensor never
// stops it. There is no opt-in and no Component to add. The stopping Contact is
// an ordinary one carrying the T it was stopped at, one point and a Depth of 0,
// and Solve moves a Dynamic body back to that point before it solves, so a
// reacting System sees it there; the Body's other Contacts that tick were found
// where it stopped and carry the same T. A filter that drops or ignores the
// stopping Contact means no stop, which is how a one-way platform lets a fast
// Body through. Two Bodies that both moved that fast meet along their relative
// motion, head-on or crossing at an angle: the pair is one stopping Contact with
// one T, and each Dynamic side moves back to it unless something on its own
// path stopped it sooner. A Kinematic body is never stopped and never pushed:
// every Dynamic body it meets on its path, not just the first, is carried
// along with it by the movement it has left after T, so a fast paddle hits
// each ball instead of passing through it. The Contact of a carry past the
// first, and the entry of a Sensor crossed past it, are written by Solve, the
// only System that can tell a Kinematic mover from a Dynamic one, so a filter
// never sees them and cannot drop them; a reacting System does
// (continuous-collision.md).
//
// One hole is accepted rather than fixed. A Sensor's path is a chord and not
// the polyline it flew, so a sharply curving one can clip a corner, as a solid
// Body's can.
//
// The plugin never moves a Sensor back and never stops one. Snap-back is the
// app's write — Position.Current = Previous, which is from.Lerp(to, T) — and so
// are reflecting, exploding and expiring it. An app that teleports a Sensor
// sets Previous = Current, as render interpolation already requires; a Sensor
// with no Velocity is one Integrate never moves, so its Previous is the app's
// to keep.
//
// A filter marks an entry two ways. Dropped takes the pair out of this tick's
// solution, so a dropped Began entry disappears, a dropped Continuing one
// becomes Ended and an Ended one cannot be dropped. Ignored is cp's arb.Ignore
// and runs until the pair comes apart, which is what a one-way platform needs;
// Solve skips an ignored entry as it skips a Sensor's, and the ignore ends when
// the pair misses one tick.
//
// The query surface, in two layers, both exported because a replacement solver
// lives in another package and is built from exactly these. The pair primitives
// ProbeShape, Penetration and ClosestPoint are free functions over values. The
// world queries Probe, ProbeAll and Overlap are methods on StaticIndex and
// BodyIndex, two Resources whose locks stay apart. Nothing on either layer
// takes a duration, a velocity, a func value or an interface, and nothing on
// either allocates — save a pair primitive handed a Polygon of more than
// thirty-two vertices, which builds it one world-cache run; the step's own path
// reads the index's slab and never does.
//
// The Shape value itself, in all five of its kinds: a circle, a segment with
// its neighbours' tangents, and the three polygon kinds. NewPolygonShape is the
// one way to a polygon and it always hulls, refusing what the hull changed
// rather than silently collapsing a concave outline into its hull as cp does; a
// Shape of more than four vertices carries them in a Polygon Component beside
// it. Four of the six pair kinds go through GJK, whose expanding hull is two
// stack buffers where cp allocates one a recursion.
//
// # A Force written this tick moves the Body next tick
//
// Positions integrate first, which is cp's order and not Box2D's. The Force an
// app's System writes this tick is turned into velocity by Solve at the end of
// the same tick, and that velocity is spent by the next tick's Integrate. Only
// Force pays this: a Velocity written directly is immediate.
//
// # Gravity is a Constant
//
// Constants is the Resource of the physics values that hold for the whole
// world rather than for one Body, and Gravity, in m/s², is the one there is.
// The plugin registers it next to Contacts at its own defaults, gravity 0, so a
// top-down world is the default and an app that never writes it pays nothing.
// Solve reads it once a tick and adds it in cp's velocity integrator beside
// Force·invMass, to Dynamic bodies only, so writing m·g into Force instead is
// the same fall. An app that wants gravity writes Constants.Gravity from a
// System of its own through ecs.Write[*Constants], once from app.InitEvent or
// whenever it changes; that System runs in series with Solve, which is the
// price the app chose. Constants are not Config, which is fixed when physics
// starts.
//
// # Sleeping
//
// Bodies that have been idle long enough stop being integrated, indexed,
// detected and solved, and wake when something disturbs them. It is cp's
// ProcessComponents and it is off by default: the Sleep Resource the plugin
// registers has a Time of 0, and a world whose app never writes it steps
// exactly as it would without sleeping at all.
//
// An app turns it on by writing Sleep from a System of its own, once from
// app.InitEvent as a rule:
//
//	sleep.Get().Time = 0.5 // seconds every Body of an Island must be idle
//
// A Dynamic body is idle when v·v·m + w²·i is at most m·IdleSpeed², cp's
// kinetic energy with no ½; an IdleSpeed of 0 falls back to one tick of the
// Constants' Gravity, so a world with no gravity and no IdleSpeed never sleeps.
// The Bodies joined by touching or by Joints are an Island, which falls asleep
// and wakes as one; a Static or Kinematic body joins none, and a Kinematic one
// keeps what it touches awake.
//
// A sleeping Body carries the Sleeping Tag, which only the plugin writes.
// Everything that wakes one is on Sleeping: a touch, a Position, Velocity or
// Force written, a changed gravity, a support removed, sleeping turned off, and
// WakeCmd for the rest. It is still found by every query on the Body index, and
// its Contacts go quiet while it sleeps and come back Continuing when it wakes.
//
// An app filter that wants cp's order — PreSolve before the Islands are built,
// so a Contact it drops neither joins nor wakes one — orders itself
// Before[SleepOnUpdate] as well as Before[SolveOnUpdate].
//
// # A Body's kind is said by its Components
//
// Dynamic present is a Dynamic body; a Velocity with no Dynamic is Kinematic;
// the Static Tag is Static and carries no Velocity. There is no Kind field, no
// Kinematic Tag and no FixedRotation Tag, and nothing compares a mass against
// an infinity to find out.
//
// A new Dynamic body starts at NewDynamicForShape, which gives it the mass and
// the Moment of inertia its Shape has at a density and moves the Shape so its
// centroid is at Position, which is the centre of gravity; the app then places
// the Body at the old origin plus the centroid it returns. NewDynamic is for the Body
// whose mass and Moment the app already has, and it leaves the Shape where it
// is: an off-centre Shape then turns about Position, which is right only for a
// Body meant to.
//
// # Memory is given back when the app asks and never before
//
// The Contact buffers, the two grids, the world-cache slab and the solver's
// scratch keep their high-water capacity, so a steady scene allocates nothing. ShrinkCmd is what
// gives that capacity back, with a Keep option per area, and it is the only
// thing in the package that allocates on purpose. Shrinking on a heuristic was
// rejected — it would put an allocation into the tick after every lull — and so
// were buffers that never gave memory back.
//
// # Units
//
// SI throughout, and per second rather than per tick: metres, kilograms,
// seconds, radians; Force in newtons and Torque in newton-metres; Gravity in
// metres per second squared; Damping and
// the solver's bias as rates in 1/s. Integration is exponential in the Damping
// rates, which is exact and stable at any step.
package ecsphysics2d
