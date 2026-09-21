package internal

// PROTOTYPE #409, throwaway, on proto/inline-poly-verts only. It answers one
// question — what reaching a polygon through ecs.Get[Polygon] costs against a
// Shape carrying eight vertex slots inline — and is never merged.
//
// Run each arm as its own binary and alternate them:
//
//	go test -c -o a.exe .                 (arm A: four slots, 104-byte Shape)
//	go test -c -tags inline8 -o b.exe .   (arm B: eight slots, 168-byte Shape)
//	a.exe -test.run=^$ -test.bench=Proto409 -test.benchmem
//
// The scene is the reference mix: 1 024 moving Bodies and 4 224 static
// segments, every one of them in the same Shape Store, at several fractions of
// the moving Bodies being hexagons. In arm A a hexagon is ShapePoly with a
// Polygon Component beside it; in arm B it is ShapePoly with its six vertices
// inline and no Polygon at all.

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/dvoyni/cog/bundles/ecs"
	"github.com/dvoyni/cog/bundles/ecs/ecsplugin"
	"github.com/dvoyni/cog/bundles/ecsphysics2d"
	"github.com/dvoyni/cog/bundles/ecsphysics2d/internal/types"
	"github.com/dvoyni/cog/kernel"
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/slots/app"
	"github.com/dvoyni/cog/slots/app/appplugin"
)

const (
	protoBodies  = 1024
	protoStatics = 4224
)

// protoFractions are the shares of the moving Bodies that are hexagons, in
// percent: none, a few, half, all.
var protoFractions = []int{0, 10, 50, 100}

// protoIsHexagon spreads the hexagons evenly through spawn order, so they are
// spread through the Store rather than packed at one end of it.
func protoIsHexagon(i, percent int) bool { return (i*percent)/100 != ((i+1)*percent)/100 }

// protoStaticAt lays the static segments out in a field of their own, well
// clear of the moving Bodies, so they are in the Shape Store and the static
// index and nowhere near a Contact.
func protoStaticAt(i int) m.Vec2d {
	return m.Vec2d{X: 60 + float64(i%66)*1.5, Y: float64(i/66) * 1.5}
}

func TestProto409Sizes(t *testing.T) {
	t.Logf("Shape %d B, Polygon %d B, Position %d B, hexagon has %d vertices",
		unsafe.Sizeof(ecsphysics2d.Shape{}), unsafe.Sizeof(ecsphysics2d.Polygon{}),
		unsafe.Sizeof(ecsphysics2d.Position{}), len(protoHexagon(t).verts()))
}

type protoHexagonPair struct {
	shape   ecsphysics2d.Shape
	polygon ecsphysics2d.Polygon
}

func (p protoHexagonPair) verts() []m.Vec2d {
	return types.PolygonVerts(nil, p.shape, p.polygon)
}

func protoHexagon(t testing.TB) protoHexagonPair {
	shape, polygon := regularPolygon(t, 6, 0.3)
	return protoHexagonPair{shape, polygon}
}

// ---- 1 and 3: the Index System's Body walk, and the bare Store walk ------

// BenchmarkProto409IndexWalk is the Index System's Body rebuild alone: the
// bodyIndexQuery walk, the Get[Polygon] probe and copy for a ShapePoly that
// needs one, and InsertMoving into a BodyIndex — the same calls plugin.index
// makes, with nothing else in the frame.
func BenchmarkProto409IndexWalk(b *testing.B) {
	for _, percent := range protoFractions {
		b.Run(fmt.Sprintf("poly=%d%%", percent), func(b *testing.B) {
			var scratch []m.Vec2d
			benchmarkProtoWalk(b, percent, func(registrar *kernel.Registrar) {
				registrar.InitResource(ecsphysics2d.NewBodyIndex(2))
				registrar.Subscribe[protoWalkOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
					bodies *ecs.Query[bodyIndexQuery],
					polygons *ecs.Get[ecsphysics2d.Polygon],
					bodyIndex *ecs.Write[*ecsphysics2d.BodyIndex],
				) {
					body := bodyIndex.Get()
					body.Clear()
					for entity, it := range bodies.All() {
						var verts []m.Vec2d
						if it.Shape.Kind == ecsphysics2d.ShapePoly && !types.PolyInline(it.Shape) {
							if polygon, ok := polygons.Of(entity); ok {
								scratch = types.PolygonVerts(scratch[:0], it.Shape, polygon)
								verts = scratch
							}
						}
						body.InsertMoving(entity, it.Shape,
							it.Place.Current, it.Place.Previous, it.Place.Angle, it.Place.PreviousAngle, verts)
					}
				}))
			})
		})
	}
}

// BenchmarkProto409StoreWalk is the same Query with no work in it: the cost of
// streaming the Shape Store past the walk, which is where the 64 bytes every
// circle and every wall would carry in arm B are paid.
func BenchmarkProto409StoreWalk(b *testing.B) {
	for _, percent := range []int{0} {
		b.Run(fmt.Sprintf("poly=%d%%", percent), func(b *testing.B) {
			benchmarkProtoWalk(b, percent, func(registrar *kernel.Registrar) {
				registrar.Subscribe[protoWalkOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar,
					func(bodies *ecs.Query[bodyIndexQuery]) {
						sum := 0.0
						for _, it := range bodies.All() {
							sum += it.Shape.Radius + it.Place.Current.X
						}
						protoSink = sum
					}))
			})
		})
	}
}

// BenchmarkProto409NoWalk is the floor both are read against: the same engine
// and scene with nothing subscribed.
func BenchmarkProto409NoWalk(b *testing.B) {
	benchmarkProtoWalk(b, 0, func(*kernel.Registrar) {})
}

// ---- 2: the probe alone --------------------------------------------------

// BenchmarkProto409Probe is the Get[Polygon] probe and the six-vertex copy on
// their own, per hexagon, which is what arm A adds per polygon over arm B.
func BenchmarkProto409Probe(b *testing.B) {
	benchmarkProtoWalk(b, 100, func(registrar *kernel.Registrar) {
		var scratch []m.Vec2d
		registrar.Subscribe[protoWalkOnUpdate](ecs.ToHandler[app.UpdateEvent](registrar, func(
			bodies *ecs.Query[bodyIndexQuery],
			polygons *ecs.Get[ecsphysics2d.Polygon],
		) {
			n := 0
			for entity, it := range bodies.All() {
				if it.Shape.Kind == ecsphysics2d.ShapePoly && !types.PolyInline(it.Shape) {
					if polygon, ok := polygons.Of(entity); ok {
						scratch = types.PolygonVerts(scratch[:0], it.Shape, polygon)
					}
				} else {
					scratch = types.PolygonVerts(scratch[:0], it.Shape, ecsphysics2d.Polygon{})
				}
				n += len(scratch)
			}
			protoSink = float64(n)
		}))
	})
}

type protoWalkOnUpdate kernel.Subscription[app.UpdateEvent]

type protoSpawnCmd kernel.Command[int, struct{}]

type protoMover struct {
	Place ecsphysics2d.Position
	Shape ecsphysics2d.Shape
}

type protoPolyMover struct {
	Place   ecsphysics2d.Position
	Shape   ecsphysics2d.Shape
	Polygon ecsphysics2d.Polygon
}

type protoWall struct {
	Place  ecsphysics2d.Position
	Shape  ecsphysics2d.Shape
	Static ecsphysics2d.Static
}

type protoGame struct {
	hexagon   protoHexagonPair
	subscribe func(*kernel.Registrar)
}

func (*protoGame) Name() kernel.PluginName { return "proto409game" }

func (*protoGame) Dependencies() []kernel.PluginName { return []kernel.PluginName{ecs.Name} }

func (g *protoGame) Register(registrar *kernel.Registrar, _ any) error {
	ecs.RegisterComponent[ecsphysics2d.Position](registrar, protoBodies+protoStatics)
	ecs.RegisterComponent[ecsphysics2d.Shape](registrar, protoBodies+protoStatics)
	ecs.RegisterComponent[ecsphysics2d.Static](registrar, protoStatics)
	ecs.RegisterComponent[ecsphysics2d.Polygon](registrar, protoBodies)
	registrar.HandleCommand[protoSpawnCmd](ecs.ToExecute[int, struct{}](registrar, func(
		percent int,
		movers *ecs.Spawn[protoMover],
		polyMovers *ecs.Spawn[protoPolyMover],
		walls *ecs.Spawn[protoWall],
	) {
		for i := range protoBodies {
			place := ecsphysics2d.Position{Current: gridAt(i)}
			place.Previous = place.Current
			switch {
			case !protoIsHexagon(i, percent):
				movers.New(protoMover{Place: place, Shape: circle(0.3)})
			case types.PolyInline(g.hexagon.shape):
				movers.New(protoMover{Place: place, Shape: g.hexagon.shape})
			default:
				polyMovers.New(protoPolyMover{Place: place, Shape: g.hexagon.shape, Polygon: g.hexagon.polygon})
			}
		}
		for i := range protoStatics {
			walls.New(protoWall{
				Place: ecsphysics2d.Position{Current: protoStaticAt(i)},
				Shape: ecsphysics2d.NewSegmentShape(m.Vec2d{}, m.Vec2d{X: 1}, 0),
			})
		}
	}))
	g.subscribe(registrar)
	return nil
}

func benchmarkProtoWalk(b *testing.B, percent int, subscribe func(*kernel.Registrar)) {
	b.Helper()
	configs := map[kernel.PluginName]any{
		ecs.Name: ecs.Config{PrewarmEntities: 2 * (protoBodies + protoStatics)},
	}
	var failure error
	engine := kernel.New(configs).
		Handler(func(err error) error { failure = err; return nil }).
		WithPlugins(appplugin.New(), mainLoopAdapter{}, ecsplugin.New(),
			&protoGame{hexagon: protoHexagon(b), subscribe: subscribe})
	stopped := make(chan struct{})
	b.Cleanup(func() {
		engine.Quit()
		<-stopped
	})
	go func() {
		defer close(stopped)
		engine.Run()
	}()
	<-engine.Ready()
	if failure != nil {
		b.Fatalf("composing the engine: %v", failure)
	}
	executioner := engine.Executioner()
	executioner.PublishEvent(app.InitEvent{}).Wait()
	executioner.ExecuteCommand[protoSpawnCmd](percent)
	for range 100 {
		executioner.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		executioner.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
	}
}

// ---- the whole step over the same scene ----------------------------------

// BenchmarkProto409Step is one whole physics tick over the reference mix at
// rest: every System, Index included, with the moving Bodies spread so that
// nothing touches — the layout's cost with the narrowphase kept out of it.
func BenchmarkProto409Step(b *testing.B) {
	for _, percent := range protoFractions {
		b.Run(fmt.Sprintf("poly=%d%%", percent), func(b *testing.B) {
			h := newHarnessWith(b, nil, uint32(2*(protoBodies+protoStatics)))
			hexagon := protoHexagon(b)
			for i := range protoBodies {
				request := spawnRequest{
					Kind:  kindShapedBody,
					Place: ecsphysics2d.Position{Current: gridAt(i)},
					Body:  dynamic(b, 1, 0.1, 0, 0),
					Shape: circle(0.3),
				}
				if protoIsHexagon(i, percent) {
					request.Shape = hexagon.shape
					if !types.PolyInline(hexagon.shape) {
						request.Kind, request.Polygon = kindPolygonBody, hexagon.polygon
					}
				}
				h.spawn(b, request)
			}
			for i := range protoStatics {
				h.spawn(b, spawnRequest{
					Kind:  kindShapedStatic,
					Place: ecsphysics2d.Position{Current: protoStaticAt(i)},
					Shape: ecsphysics2d.NewSegmentShape(m.Vec2d{}, m.Vec2d{X: 1}, 0),
				})
			}
			h.frames(b, 100)
			if n := len(h.contacts(b)); n != 0 {
				b.Fatalf("the resting scene has %d Contacts; it is meant to have none", n)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				h.kernel.PublishEvent(app.UpdateEvent{Dt: tick}).Wait()
			}
		})
	}
}

// protoSink keeps the walks from being optimised away.
var protoSink float64
