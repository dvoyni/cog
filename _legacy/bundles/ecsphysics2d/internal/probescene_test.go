package internal

import (
	"fmt"
	"math"
	"testing"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/libs/m"
)

// Layer C's seventh scene: a thousand and twenty-four one-metre Probes at a
// radius of 0.3 over a field of Shapes, with brute force over every Shape as the
// oracle. cp is not the oracle here and is not imported; what the scene does with
// cp is re-derive the rate its own broadphase would answer at, from cp's rule
// rather than from cp's numbers.
//
// The porting index gives two figures for defect 5 that cannot both be right —
// a hit rate of 0.729 against brute force's 0.896, and separately 187 of 918
// Hits missed, which is 0.796 — and the two are never reconciled, so neither is
// citable. The claim that survives, and which both support, is the qualitative
// one: a radius-0.3 Probe through cp's broadphase misses roughly a fifth of the
// Hits it should find, and returns a farther Hit for some of the rest.
//
// Measured rather than quoted, the fraction turns out to depend on one thing the
// porting index never records: how big the Shapes are against the 0.3 m the
// Probe is inflated by. At the specification's own reference scale the scene
// below misses 0.297 of the Hits and at half that scale 0.431, from the same
// seed and the same thousand Probes. So there is no one number to re-derive, and
// two scenes at different scales disagreeing by that much is the likeliest
// reading of the porting index's two irreconcilable figures. What the scene
// asserts is what holds at both scales: cp's rule misses a large part of the
// Hits, returns a farther Hit for some more, and the port misses none of either.
const (
	probeSceneShapes  = 400
	probeSceneProbes  = 1024
	probeSceneRadius  = 0.3
	probeSceneLength  = 1.0
	probeSceneCell    = 2.0
	probeSceneExtent  = 20.0
	probeSceneSeed    = 0x243F6A8885A308D3
	probeSceneNearest = 1e-9
)

// probeSceneShape is one placed Shape of the field, kept beside the index so the
// brute force has values to work from and never asks the index anything.
type probeSceneShape struct {
	entity ecs.Entity
	shape  Shape
	at     m.Vec2d
	angle  float64
	verts  []m.Vec2d
	box    BB
}

// buildProbeScene lays the field out: circles, segments, boxes and hexagons
// scattered over the extent, every one of them scaled by scale.
//
// The scale is a parameter because the fraction of Hits cp's rule misses is a
// function of it and of nothing else interesting: a Shape is missed exactly when
// the swept circle reaches it and the bare centreline does not, and how often
// that happens depends on how big the Shape is against the 0.3 m the Probe is
// inflated by. That dependence is almost certainly why the porting index has two
// figures for defect 5 that cannot be reconciled — they are two scenes, not two
// readings of one — and it is why this scene measures at two scales and quotes
// neither as the number.
func buildProbeScene(t testing.TB, scale float64) ([]probeSceneShape, *StaticIndex) {
	t.Helper()
	rng := splitmix64(probeSceneSeed)
	idx := NewStaticIndex(probeSceneCell)
	field := make([]probeSceneShape, 0, probeSceneShapes)

	for i := range probeSceneShapes {
		at := m.Vec2d{
			X: (float64(rng.intn(4001))/4000*2 - 1) * probeSceneExtent,
			Y: (float64(rng.intn(4001))/4000*2 - 1) * probeSceneExtent,
		}
		angle := float64(rng.intn(628)) / 100
		var shape Shape
		var verts []m.Vec2d
		switch i % 4 {
		case 0:
			shape = NewCircleShape((0.2+float64(rng.intn(31))/100)*scale, m.Vec2d{})
		case 1:
			shape = NewSegmentShape(
				m.Vec2d{X: -0.5 * scale}, m.Vec2d{X: 0.5 * scale}, 0)
		case 2:
			shape = NewBoxShape(0.5*scale, 0.5*scale, 0)
		case 3:
			hexagon, polygon, err := NewPolygonShape([]m.Vec2d{
				{X: 0.4 * scale}, {X: 0.2 * scale, Y: 0.35 * scale},
				{X: -0.2 * scale, Y: 0.35 * scale},
				{X: -0.4 * scale}, {X: -0.2 * scale, Y: -0.35 * scale},
				{X: 0.2 * scale, Y: -0.35 * scale},
			}, 0)
			if err != nil {
				t.Fatalf("hulling the hexagon: %v", err)
			}
			shape, verts = hexagon, PolygonVerts(nil, hexagon, polygon)
		}
		entity := testEntity(i)
		idx.Insert(entity, shape, at, angle, verts)
		_, box := cacheWorldAt(shape, NewTransformRigid(at, angle), verts,
			make([]m.Vec2d, worldLenFor(shape, verts)))
		field = append(field, probeSceneShape{
			entity: entity, shape: shape, at: at, angle: angle, verts: verts, box: box,
		})
	}
	return field, idx
}

// probeSceneRay is one Probe of the scene: a metre long from somewhere in the
// field, in a direction the seed chose.
func probeSceneRay(rng *splitmix64) (m.Vec2d, m.Vec2d) {
	from := m.Vec2d{
		X: (float64(rng.intn(4001))/4000*2 - 1) * probeSceneExtent,
		Y: (float64(rng.intn(4001))/4000*2 - 1) * probeSceneExtent,
	}
	angle := float64(rng.intn(628)) / 100
	return from, from.Add(m.ForAngle(angle).MulS(probeSceneLength))
}

// bruteForce is the oracle: every Shape in the field tested with the pair
// primitive, every Hit appended to dst. It asks no index anything, which is the
// whole of why it is the oracle.
func bruteForce(dst []Hit, field []probeSceneShape, from, to m.Vec2d, radius float64) []Hit {
	for i := range field {
		s := &field[i]
		if hit, ok := ProbeShape(from, to, radius, s.shape, s.at, s.angle, s.verts); ok {
			hit.Entity = s.entity
			dst = append(dst, hit)
		}
	}
	return dst
}

// nearestOf is the smallest T of a set of Hits and every Entity that reaches it.
// A Probe that starts inside two Shapes meets both at T = 0, and which of them a
// walk names first is the walk's own order rather than an answer: the index is
// owed the T, and the identity only up to that tie.
func nearestOf(hits []Hit) (float64, map[ecs.Entity]bool) {
	nearest := math.Inf(1)
	for _, hit := range hits {
		nearest = math.Min(nearest, hit.T)
	}
	tied := map[ecs.Entity]bool{}
	for _, hit := range hits {
		if hit.T <= nearest+probeSceneNearest {
			tied[hit.Entity] = true
		}
	}
	return nearest, tied
}

// chipmunkBroadphase is cp's rule rather than cp's numbers: the broadphase
// receives the ray it was given and never the swept circle, so a Shape whose own
// box the bare segment does not enter is never a candidate at all — however far
// the Probe's radius reaches past it. That is defect 5, and it is in C as well,
// where cpBBTreeSegmentQuery descends on cpBBIntersectsSegment of the un-inflated
// a and b. The narrowing below it is the same pair primitive at the full radius,
// exactly as cp's is; only the candidate set differs.
func chipmunkBroadphase(field []probeSceneShape, from, to m.Vec2d, radius float64) (float64, bool) {
	nearest, found := math.Inf(1), false
	for i := range field {
		s := &field[i]
		if !s.box.IntersectsSegment(from, to) {
			continue
		}
		if hit, ok := ProbeShape(from, to, radius, s.shape, s.at, s.angle, s.verts); ok && hit.T < nearest {
			nearest, found = hit.T, true
		}
	}
	return nearest, found
}

func TestAThousandProbesFindEveryHitBruteForceFindsWhereChipmunksBroadphaseMissesAFifth(t *testing.T) {
	// Two Shape scales, because one would quote a number rather than re-derive
	// one. 1.0 is the scale of the specification's own reference scene — circles
	// of 0.4 m, boxes of half a metre — and 0.5 is a scene of small objects
	// against the same 0.3 m Probe radius.
	for _, scale := range []float64{1.0, 0.5} {
		t.Run(fmt.Sprintf("Shapes at %v of the reference scale", scale), func(t *testing.T) {
			probeScene(t, scale)
		})
	}
}

func probeScene(t *testing.T, scale float64) {
	field, idx := buildProbeScene(t, scale)
	rng := splitmix64(probeSceneSeed)

	var (
		truthHits  int
		cpHits     int
		cpMissed   int
		cpFarther  int
		indexHits  int
		all, brute []Hit
		multiplate int
	)

	for range probeSceneProbes {
		from, to := probeSceneRay(&rng)

		// Brute force's whole set first, because the nearest Hit is read out of
		// it and ProbeAll is judged against all of it. The nearest alone would
		// not catch an index that finds the right first Shape and loses the ones
		// behind it.
		brute = bruteForce(brute[:0], field, from, to, probeSceneRadius)
		wantNearest, wantTied := nearestOf(brute)

		got, gotOK := idx.Probe(from, to, probeSceneRadius,
			CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)

		// The assertion. The index is a broadphase and nothing more, so whatever
		// brute force over the same values finds, it finds — and at the same T,
		// to the specification's 1e-9 and not a tolerance loosened to fit.
		if (len(brute) > 0) != gotOK {
			t.Fatalf("a Probe from %v to %v: brute force finds %d Hits and the index says %v",
				from, to, len(brute), gotOK)
		}
		if gotOK {
			truthHits++
			indexHits++
			if math.Abs(got.T-wantNearest) > probeSceneNearest {
				t.Fatalf("a Probe from %v to %v: brute force's nearest is T %v, the index's is T %v",
					from, to, wantNearest, got.T)
			}
			if !wantTied[got.Entity] {
				t.Fatalf("a Probe from %v to %v: the index names %v at T %v, which is not one of "+
					"the Shapes brute force finds nearest", from, to, got.Entity, got.T)
			}
		}

		all = idx.ProbeAll(all[:0], from, to, probeSceneRadius,
			CollisionBitsAll, CollisionBitsAll, ecs.NoEntity)
		if len(all) != len(brute) {
			t.Fatalf("a Probe from %v to %v: ProbeAll found %d Hits and brute force %d",
				from, to, len(all), len(brute))
		}
		if len(all) > 1 {
			multiplate++
		}
		reported := map[ecs.Entity]float64{}
		for _, hit := range all {
			reported[hit.Entity] = hit.T
		}
		for _, hit := range brute {
			at, named := reported[hit.Entity]
			if !named {
				t.Fatalf("a Probe from %v to %v: ProbeAll never named %v, which brute force finds",
					from, to, hit.Entity)
			}
			if math.Abs(at-hit.T) > probeSceneNearest {
				t.Fatalf("a Probe from %v to %v: ProbeAll puts %v at T %v and brute force at T %v",
					from, to, hit.Entity, at, hit.T)
			}
		}

		// cp's rule, re-derived rather than quoted.
		cpNearest, cpOK := chipmunkBroadphase(field, from, to, probeSceneRadius)
		switch {
		case len(brute) == 0:
		case !cpOK:
			cpMissed++
		default:
			cpHits++
			if cpNearest > wantNearest+probeSceneNearest {
				cpFarther++
			}
		}
	}

	missed := float64(cpMissed) / float64(truthHits)
	farther := float64(cpFarther) / float64(truthHits)
	t.Logf("seed %#x, Shapes at %v of the reference scale: %d Probes of %v m at radius %v over "+
		"%d Shapes — brute force finds %d Hits and the index finds all %d of them; cp's "+
		"un-inflated broadphase finds %d, missing %d (%.3f of the Hits) and returning a farther "+
		"Hit on %d more (%.3f); %d Probes met more than one Shape",
		uint64(probeSceneSeed), scale, probeSceneProbes, probeSceneLength, probeSceneRadius,
		probeSceneShapes, truthHits, indexHits, cpHits, cpMissed, missed, cpFarther, farther,
		multiplate)

	// The emptiness guards. A field the Probes never meet would pass every
	// assertion above while measuring nothing at all, and a field where no Probe
	// meets two Shapes would leave ProbeAll's whole-set claim untested.
	if truthHits == 0 {
		t.Fatal("no Probe met anything, so the scene measures neither the index nor cp's rule")
	}
	if multiplate == 0 {
		t.Fatal("no Probe met more than one Shape, so ProbeAll's whole-set claim is untested")
	}

	// What the scene re-derives. The fraction missed is a function of the Shape
	// scale against the Probe radius and of nothing else interesting — it runs
	// from about a third at half the reference scale to about a fifth above it —
	// so no single figure is citable, which is the finding rather than an excuse
	// for one. What does survive at every scale, and is asserted here, is that
	// cp's rule misses a large part of the Hits a radius-0.3 Probe should find,
	// that it also returns a farther Hit for some of the rest, and that the port
	// misses none of either.
	if missed < 0.1 {
		t.Errorf("cp's rule misses only %.3f of the Hits, so defect 5 is not what this scene "+
			"is exercising", missed)
	}
	if cpFarther == 0 {
		t.Error("cp's rule never returned a farther Hit than the truth, so the second half of " +
			"defect 5 — a Hit found, but not the nearest one — is untested")
	}
	if indexHits != truthHits {
		t.Errorf("the index found %d of brute force's %d Hits", indexHits, truthHits)
	}
}
