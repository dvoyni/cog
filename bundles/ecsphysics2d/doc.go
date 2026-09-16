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
// The query surface, in two layers, both exported because a replacement solver
// lives in another package and is built from exactly these. The pair primitives
// ProbeShape, Penetration and ClosestPoint are free functions over values. The
// world queries Probe, ProbeAll and Overlap are methods on StaticIndex and
// BodyIndex, two Resources whose locks stay apart. Nothing on either layer
// takes a duration, a velocity, a func value or an interface, and nothing on
// either allocates.
//
// The Shape value itself, in its circle and segment kinds. The Polygon kinds,
// the hulling constructor and the GJK path four of the six pair kinds go
// through arrive with the Polygon pipeline, and until then a query against one
// reports no Hit.
package ecsphysics2d
