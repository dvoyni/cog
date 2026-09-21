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
// with the four Systems the step is chained on app.UpdateEvent in cp's own
// order — IntegrateOnUpdate, IndexOnUpdate, DetectOnUpdate, SolveOnUpdate.
// Integrate moves every Body with a Velocity, Index keeps the two indices
// current, Detect writes the tick's Contacts, and Solve drives them apart.
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
// and stays zero; the Shape field that would feed it is not built yet. Joints
// arrive with theirs.
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
// serves pressure plates and area damage. Every moving circle Sensor is Probed
// once a tick, from Position.Previous to Current, with no opt-in flag: its
// entries are its Entity plus a Hit, sitting together in the list and ordered
// by T, so the app takes the first and stops at a wall. Box and segment Sensors
// are tested discretely, a Static Sensor is never Probed, and two Sensors that
// find each other keep the smaller T. T and Depth mean one thing on every
// entry: a Probed Sensor carries the Probe's T and a Depth of 0, except one
// that started inside something, which reports T = 0 with the overlap at the
// start, and everything else reports T = 1 with the overlap where the tick
// ended.
//
// Two holes are accepted rather than fixed. A Sensor's path is a chord and not
// the polyline it flew, so a sharply curving one can clip a corner; and two
// moving Sensors are tested against each other's end positions rather than
// their relative motion, so two with a radius crossing within one tick can miss
// each other.
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
// # A Body's kind is said by its Components
//
// Dynamic present is a Dynamic body; a Velocity with no Dynamic is Kinematic;
// the Static Tag is Static and carries no Velocity. There is no Kind field, no
// Kinematic Tag and no FixedRotation Tag, and nothing compares a mass against
// an infinity to find out.
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
// seconds, radians; Force in newtons and Torque in newton-metres; Damping and
// the solver's bias as rates in 1/s. Integration is exponential in the Damping
// rates, which is exact and stable at any step.
package ecsphysics2d
