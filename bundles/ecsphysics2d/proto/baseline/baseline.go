// Package baseline is PROTOTYPE code for cog#345. Throwaway; it never merges.
//
// It rebuilds the ../queryindex workload inside two third-party 2D physics
// libraries, Chipmunk (github.com/jakecoffman/cp/v2) and Box2D v3
// (github.com/neguse/gox2d), so their sweep costs sit beside cog's own grid and
// BVH on identical geometry and identical queries. It lives in its own module so
// cog's go.mod never sees either dependency. See README.md.
package baseline

import (
	"github.com/dvoyni/cog/bundles/ecsphysics2d/proto/queryindex"
	"github.com/jakecoffman/cp/v2"
	"github.com/neguse/gox2d/box2d"
)

// Hit is one library hit in the prototype's terms: the placed item's slot and
// the fraction of from→to.
type Hit struct {
	Slot int
	T    float32
}

// ---------------------------------------------------------------------------
// Chipmunk

// CP is a cp.Space holding every placed shape on the space's static body, so
// everything sits in the one static BBTree built by incremental insertion.
type CP struct {
	Space *cp.Space
	hits  []Hit
	all   cp.SpaceSegmentQueryFunc
}

// NewCP builds the space. Segments are radius-0 cp segments, circles are cp
// circles offset from the static body, boxes are cp boxes from their bounds.
func NewCP(items []queryindex.Placed) *CP {
	w := &CP{Space: cp.NewSpace()}
	body := w.Space.StaticBody
	for i, it := range items {
		s := it.Shape
		at := cp.Vector{X: float64(it.At.X), Y: float64(it.At.Y)}
		var sh *cp.Shape
		switch {
		case s.IsSegment():
			h := cp.Vector{X: float64(s.Half().X), Y: float64(s.Half().Y)}
			sh = cp.NewSegment(body, at.Sub(h), at.Add(h), 0)
		case s.IsCircle():
			sh = cp.NewCircle(body, float64(s.Radius()), at)
		default:
			h := s.Half()
			sh = cp.NewBox2(body, cp.BB{
				L: at.X - float64(h.X), B: at.Y - float64(h.Y),
				R: at.X + float64(h.X), T: at.Y + float64(h.Y),
			}, 0)
		}
		sh.UserData = i
		w.Space.AddShape(sh)
	}
	w.all = w.collect
	return w
}

// Sweep is SegmentQueryFirst with the query's radius.
func (w *CP) Sweep(q queryindex.Query) (Hit, bool) {
	info := w.Space.SegmentQueryFirst(
		cp.Vector{X: float64(q.From.X), Y: float64(q.From.Y)},
		cp.Vector{X: float64(q.To.X), Y: float64(q.To.Y)},
		float64(q.Radius), cp.SHAPE_FILTER_ALL)
	if info.Shape == nil {
		return Hit{}, false
	}
	return Hit{Slot: info.Shape.UserData.(int), T: float32(info.Alpha)}, true
}

// SweepAll is SegmentQuery with a callback appending into a reused slice. The
// hits come back in tree order, unsorted.
func (w *CP) SweepAll(dst []Hit, q queryindex.Query) []Hit {
	w.hits = dst
	w.Space.SegmentQuery(
		cp.Vector{X: float64(q.From.X), Y: float64(q.From.Y)},
		cp.Vector{X: float64(q.To.X), Y: float64(q.To.Y)},
		float64(q.Radius), cp.SHAPE_FILTER_ALL, w.all, nil)
	dst, w.hits = w.hits, nil
	return dst
}

func (w *CP) collect(shape *cp.Shape, _, _ cp.Vector, alpha float64, _ any) {
	w.hits = append(w.hits, Hit{Slot: shape.UserData.(int), T: float32(alpha)})
}

// ---------------------------------------------------------------------------
// Box2D v3

const (
	kindSegment = iota
	kindCircle
	kindBox
)

// Gox2d is one b2DynamicTree over exact (unfattened) shape bounds, fully
// rebuilt after insertion, with the shapes in per-kind arrays the proxies'
// user data indexes.
type Gox2d struct {
	Tree box2d.DynamicTree

	kind []uint8
	at   []int32 // index into the kind's array
	seg  []box2d.Segment
	circ []box2d.Circle
	poly []box2d.Polygon

	// per-query state the bound callbacks write
	best     Hit
	found    bool
	hits     []Hit
	rayFirst box2d.TreeRayCastCallbackFcn
	rayAll   box2d.TreeRayCastCallbackFcn
	castOne  box2d.TreeShapeCastCallbackFcn
	castAll  box2d.TreeShapeCastCallbackFcn
}

// NewGox2d builds the tree.
func NewGox2d(items []queryindex.Placed) *Gox2d {
	w := &Gox2d{Tree: box2d.DynamicTreeCreate()}
	for i, it := range items {
		s := it.Shape
		at := box2d.Vec2{X: it.At.X, Y: it.At.Y}
		var aabb box2d.AABB
		switch {
		case s.IsSegment():
			h := box2d.Vec2{X: s.Half().X, Y: s.Half().Y}
			g := box2d.Segment{Point1: box2d.Sub(at, h), Point2: box2d.Add(at, h)}
			aabb = box2d.ComputeSegmentAABB(&g, box2d.TransformIdentity)
			w.kind = append(w.kind, kindSegment)
			w.at = append(w.at, int32(len(w.seg)))
			w.seg = append(w.seg, g)
		case s.IsCircle():
			g := box2d.Circle{Center: at, Radius: s.Radius()}
			aabb = box2d.ComputeCircleAABB(&g, box2d.TransformIdentity)
			w.kind = append(w.kind, kindCircle)
			w.at = append(w.at, int32(len(w.circ)))
			w.circ = append(w.circ, g)
		default:
			g := box2d.MakeOffsetBox(s.Half().X, s.Half().Y, at, box2d.RotIdentity)
			aabb = box2d.ComputePolygonAABB(&g, box2d.TransformIdentity)
			w.kind = append(w.kind, kindBox)
			w.at = append(w.at, int32(len(w.poly)))
			w.poly = append(w.poly, g)
		}
		box2d.DynamicTreeCreateProxy(&w.Tree, aabb, box2d.DefaultCategoryBits, uint64(i))
	}
	box2d.DynamicTreeRebuild(&w.Tree, true)
	w.rayFirst, w.rayAll = w.rayFirstFn, w.rayAllFn
	w.castOne, w.castAll = w.castFirstFn, w.castAllFn
	return w
}

func (w *Gox2d) ray(in *box2d.RayCastInput, slot uint64) box2d.CastOutput {
	j := w.at[slot]
	switch w.kind[slot] {
	case kindSegment:
		return box2d.RayCastSegment(in, &w.seg[j], false)
	case kindCircle:
		return box2d.RayCastCircle(in, &w.circ[j])
	default:
		return box2d.RayCastPolygon(in, &w.poly[j])
	}
}

func (w *Gox2d) cast(in *box2d.ShapeCastInput, slot uint64) box2d.CastOutput {
	j := w.at[slot]
	switch w.kind[slot] {
	case kindSegment:
		return box2d.ShapeCastSegment(in, &w.seg[j])
	case kindCircle:
		return box2d.ShapeCastCircle(in, &w.circ[j])
	default:
		return box2d.ShapeCastPolygon(in, &w.poly[j])
	}
}

// the closest-hit callbacks clip the tree walk to the hit's fraction, as
// b2World_CastRayClosest does
func (w *Gox2d) rayFirstFn(in *box2d.RayCastInput, _ int, slot uint64) float32 {
	out := w.ray(in, slot)
	if !out.Hit {
		return -1
	}
	w.best, w.found = Hit{Slot: int(slot), T: out.Fraction}, true
	return out.Fraction
}

func (w *Gox2d) castFirstFn(in *box2d.ShapeCastInput, _ int, slot uint64) float32 {
	out := w.cast(in, slot)
	if !out.Hit {
		return -1
	}
	w.best, w.found = Hit{Slot: int(slot), T: out.Fraction}, true
	return out.Fraction
}

// the every-hit callbacks return -1, which leaves the walk unclipped
func (w *Gox2d) rayAllFn(in *box2d.RayCastInput, _ int, slot uint64) float32 {
	if out := w.ray(in, slot); out.Hit {
		w.hits = append(w.hits, Hit{Slot: int(slot), T: out.Fraction})
	}
	return -1
}

func (w *Gox2d) castAllFn(in *box2d.ShapeCastInput, _ int, slot uint64) float32 {
	if out := w.cast(in, slot); out.Hit {
		w.hits = append(w.hits, Hit{Slot: int(slot), T: out.Fraction})
	}
	return -1
}

func (w *Gox2d) rayInput(q queryindex.Query) box2d.RayCastInput {
	return box2d.RayCastInput{
		Origin:      box2d.Vec2{X: q.From.X, Y: q.From.Y},
		Translation: box2d.Vec2{X: q.To.X - q.From.X, Y: q.To.Y - q.From.Y},
		MaxFraction: 1,
	}
}

func (w *Gox2d) castInput(q queryindex.Query) box2d.ShapeCastInput {
	in := box2d.ShapeCastInput{
		Translation: box2d.Vec2{X: q.To.X - q.From.X, Y: q.To.Y - q.From.Y},
		MaxFraction: 1,
	}
	in.Proxy.Points[0] = box2d.Vec2{X: q.From.X, Y: q.From.Y}
	in.Proxy.Count = 1
	in.Proxy.Radius = q.Radius
	return in
}

// Sweep is DynamicTreeRayCast + ray narrowphase when the radius is 0, and
// DynamicTreeShapeCast + shape-cast narrowphase with a one-point proxy of the
// query's radius otherwise.
func (w *Gox2d) Sweep(q queryindex.Query) (Hit, bool) {
	w.found = false
	if q.Radius == 0 {
		in := w.rayInput(q)
		box2d.DynamicTreeRayCast(&w.Tree, &in, box2d.DefaultMaskBits, w.rayFirst)
	} else {
		in := w.castInput(q)
		box2d.DynamicTreeShapeCast(&w.Tree, &in, box2d.DefaultMaskBits, w.castOne)
	}
	return w.best, w.found
}

// SweepAll is the same pairing with callbacks that never clip, appending into
// a reused slice. The hits come back in tree order, unsorted.
func (w *Gox2d) SweepAll(dst []Hit, q queryindex.Query) []Hit {
	w.hits = dst
	if q.Radius == 0 {
		in := w.rayInput(q)
		box2d.DynamicTreeRayCast(&w.Tree, &in, box2d.DefaultMaskBits, w.rayAll)
	} else {
		in := w.castInput(q)
		box2d.DynamicTreeShapeCast(&w.Tree, &in, box2d.DefaultMaskBits, w.castAll)
	}
	dst, w.hits = w.hits, nil
	return dst
}
