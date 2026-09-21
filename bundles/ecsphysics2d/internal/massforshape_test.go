package internal

import (
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/libs/m"
)

// A polygon drawn off-centre and spun by a Torque turns about its centroid only
// when it was built through NewDynamicForShape. The same outline built by hand,
// with the Moment computed correctly about its centroid but the Shape left
// where it was drawn, turns about Position instead — its local origin — and its
// centroid swings round a circle as it spins. Nothing fails; it only wobbles,
// which is why recentring is the constructor's job.
func TestAnOffCentrePolygonTurnsAboutItsCentroidOnlyWhenBuiltThroughTheConstructor(t *testing.T) {
	const (
		density = 1.0
		torque  = 8.0
		ticks   = 60
	)
	// A hexagon of six vertices, drawn about (2, 1) rather than about the
	// origin, so its Shape is a Poly carrying a Polygon.
	hexagon := []m.Vec2d{
		{X: 3, Y: 1}, {X: 2.5, Y: 1.866}, {X: 1.5, Y: 1.866},
		{X: 1, Y: 1}, {X: 1.5, Y: 0.134}, {X: 2.5, Y: 0.134},
	}
	drawn, drawnPolygon, err := ecsphysics2d.NewPolygonShape(hexagon, 0)
	if err != nil {
		t.Fatalf("NewPolygonShape: %v", err)
	}
	centroid, _ := ecsphysics2d.CentroidForPoly(hexagon)

	// The hand-built Body: the right mass and the right Moment about the
	// centroid, and the Shape left where it was drawn.
	mass := density * ecsphysics2d.AreaForPoly(hexagon, 0)
	handBuilt := dynamic(t, mass, ecsphysics2d.MomentForPoly(mass, hexagon, centroid.Negate(), 0), 0, 0)

	built, shape, polygon, returned, err := ecsphysics2d.NewDynamicForShape(drawn, drawnPolygon, density, 0, 0)
	if err != nil {
		t.Fatalf("NewDynamicForShape: %v", err)
	}
	if math.Abs(built.Moment()-handBuilt.Moment()) > 1e-9 || math.Abs(built.Mass()-handBuilt.Mass()) > 1e-9 {
		t.Fatalf("the two Bodies differ in more than their pivot: mass %v and %v, Moment %v and %v",
			built.Mass(), handBuilt.Mass(), built.Moment(), handBuilt.Moment())
	}

	if returned.Distance(centroid) > 1e-9 {
		t.Fatalf("the constructor returned the centroid %v, the outline's is %v", returned, centroid)
	}

	// Two origins far enough apart that the hexagons never touch. The
	// constructor's Body is placed at its origin plus the returned centroid,
	// which is the one line its doc shows.
	handOrigin, builtOrigin := m.Vec2d{}, m.Vec2d{X: 50}
	builtAt := builtOrigin.Add(returned)

	h := newHarness(t)
	hand := h.spawn(t, spawnRequest{
		Kind: kindPolygonBody, Place: ecsphysics2d.Position{Current: handOrigin, Previous: handOrigin},
		Body: handBuilt, Shape: drawn, Polygon: drawnPolygon,
	})
	byConstructor := h.spawn(t, spawnRequest{
		Kind: kindPolygonBody, Place: ecsphysics2d.Position{Current: builtAt, Previous: builtAt},
		Body: built, Shape: shape, Polygon: polygon,
	})
	h.game.torque = torque

	// Where each Body's centroid is in the world: its Position plus its own
	// local centroid turned by its Angle.
	handLocal, _ := ecsphysics2d.CentroidForPoly(ecsphysics2d.PolygonVerts(nil, drawn, drawnPolygon))
	builtLocal, _ := ecsphysics2d.CentroidForPoly(ecsphysics2d.PolygonVerts(nil, shape, polygon))
	worldCentroid := func(e ecs.Entity, local m.Vec2d) (m.Vec2d, float64) {
		place := h.read(t, e).Place
		return place.Current.Add(local.Rotate(m.ForAngle(place.Angle))), place.Angle
	}

	var handWobble, builtWobble float64
	for range ticks {
		h.frames(t, 1)
		handAt, handAngle := worldCentroid(hand, handLocal)
		builtCentroid, builtAngle := worldCentroid(byConstructor, builtLocal)
		if math.Abs(handAngle-builtAngle) > 1e-9 {
			t.Fatalf("the two Bodies turned by %v and %v rad under the same Torque", handAngle, builtAngle)
		}
		handWobble = max(handWobble, handAt.Distance(handOrigin.Add(centroid)))
		builtWobble = max(builtWobble, builtCentroid.Distance(builtAt))
	}

	angle := h.read(t, byConstructor).Place.Angle
	t.Logf("after %.2f rad the hand-built centroid strayed %.6f m, the constructor's %.3g m", angle, handWobble, builtWobble)
	if angle < 2 {
		t.Fatalf("the Bodies turned only %v rad, too little to show a wobble", angle)
	}
	if builtWobble > 1e-9 {
		t.Errorf("the constructor's Body moved its centroid %v m while it spun, want it turning in place", builtWobble)
	}
	if want := centroid.Length(); handWobble < want {
		t.Errorf("the hand-built Body's centroid strayed only %v m, want at least %v: it turns about its origin",
			handWobble, want)
	}
}
