// Package internal is the physics plugin itself: the Components it registers,
// the five Systems the step is, and the settings resolved at registration.
//
// Everything with an algorithm in it is a port of Chipmunk, taken from
// github.com/jakecoffman/cp/v2 v2.4.0 (MIT, Copyright (c) 2017 Jake Coffman) and
// checked against Chipmunk2D f2f3d66 (MIT, Copyright (c) 2007-2015 Scott Lembcke
// and Howling Moon Software). A departure from cp is a finding, stated where it
// is made, and only four reasons are acceptable: the ECS layout, zero
// allocations, per-second units, or a defect in cp.
//
// Only composition roots and tests reach this package, through
// ecsphysics2dplugin; everything else reaches the plugin through its root.
package internal
