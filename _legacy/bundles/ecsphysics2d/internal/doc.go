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
// It declares everything the root aliases - the Components, Transform and BB,
// the Contacts and indices, the commands and errors - beside the solver that
// works on them, so a System and the arithmetic it drives share one package and
// every Component field. The vector is m.Vec2d and lives in libs/m, because
// every Component and query exposes it to gameplay code; Transform and BB are
// physics-only and live here. There is no internal/types: nothing here is
// plain data another package needs apart from the logic.
//
// Only composition roots and tests reach this package, through
// ecsphysics2dplugin; everything else reaches the plugin through its root,
// which aliases what is declared here and which this package never imports.
package internal
