# What Get[Polygon] costs against a bigger inline vertex array in Shape

Prototype for [dvoyni/cog#409](https://github.com/dvoyni/cog/issues/409), child of the
physics follow-up map [#315](https://github.com/dvoyni/cog/issues/315), serving
[#507](https://github.com/dvoyni/cog/issues/507), which settles what `Shape` weighs at the
end. Throwaway: it lives on `proto/inline-poly-verts` and is never merged.

**Answer: inlining does not win, so the cap stays at 4.**

- **Mostly circles.** The 8-slot layout loses the Index walk by 5–7% when 10% or fewer of
  the moving Bodies are polygons.
- **Break-even.** It draws level on the Index walk only when about 80% of the moving
  Bodies are polygons.
- **The whole step.** It is slower at every polygon fraction measured: +10 to +20 µs a
  tick, which is 3–6%.
- **What the probe costs.** Reaching a hexagon through `Get[Polygon]` and copying its
  vertices costs about **10 ns** per polygon. The 64 bytes that arm B adds to every
  `Shape` cost more than that, at every fraction where polygons are not nearly
  everything.

## Why the 64 bytes are dearer than the ticket assumed

The ticket charged the 64 bytes to the Shape Store walk alone. Two more places pay them:

- **Every index entry carries a copy of the `Shape`** (`types/index.go:62`). A wider `Shape`
  widens every BodyIndex entry and every StaticIndex entry.
- **Detect reads those entries.** That explains why the whole step is still 5.6% slower in
  arm B at 100% hexagons, where the Index walk alone is level.

Arm B is also 336 KB larger across the reference mix's Shape Store. Arm A's polygons cost
at most 131 KB, when all 1,024 moving Bodies are hexagons.

## The two arms

Both arms are built from one tree. A build tag sets how many vertex slots `Shape` has:

| | Arm A (`main`) | Arm B (`-tags inline8`) |
|---|---|---|
| Vertex slots in `Shape` | 4 | 8 |
| `sizeof(Shape)` | 104 B | 168 B |
| Where a hexagon's vertices live | `ShapePoly` plus a `Polygon` Component, probed with `Get` and copied per tick | `ShapePoly` with 6 vertices inline and no `Polygon` |

The prototype's change to the port is 21 lines:

- `shapeSlots` sets the array length;
- an `inlineN` byte uses one byte of existing padding, so arm A is still 104 B;
- `polyCount`, `polyVert`, `PolygonVerts` and `newHulledShape` read the inline run;
- the Index System skips `Get` for an inline polygon.

Arm B passes the full suite, the cp corpus at 1e-9 included. It fails only the three tests
that pin the 4-slot design, such as "a five-vertex outline carries a Polygon".

## Scene

The reference mix:

- **1,024 moving Bodies** on the 1.7 m grid;
- **4,224 static segments** in a field of their own, all in the same Shape Store;
- **hexagons (r 0.3 m) at 0, 10, 50 and 100%** of the moving Bodies, spread evenly
  through spawn order, and circles (r 0.3 m) for the rest.

The Bodies are at rest and nothing touches: `Proto409Step` fails if it finds a Contact.
This keeps the narrowphase out of the layout measurement. The existing
`BenchmarkThePolygonStep` is run as well, to cover the contact-heavy case: a quarter
boxes, a quarter triangles and half hexagons, all resting on tiles.

## Method

- Two binaries built with `go test -c`, one per arm.
- **Interleaved A/B:** A then B, ten rounds, 1,500 iterations a benchmark, reported as
  medians.
- AMD Ryzen 9 7950X3D, 32 GB, Windows 11, go1.27.1.
- Allocations are identical in both arms; the 2 to 4 per op are the kernel's dispatch.

## Results

Medians in µs a tick, with the ten-run range beside each.

| Benchmark | A | B | B − A | B/A | A range | B range |
|---|---:|---:|---:|---:|---|---|
| Index walk, 0% polygons | 120.0 | 128.0 | +8.0 | 1.066 | 117–122 | 126–130 |
| Index walk, 10% | 130.7 | 137.2 | +6.5 | 1.050 | 127–132 | 135–141 |
| Index walk, 50% | 165.3 | 168.8 | +3.5 | 1.021 | 161–168 | 166–178 |
| Index walk, 100% | 209.4 | 207.5 | −1.9 | 0.991 | 207–215 | 204–217 |
| Store walk alone, 0% | 37.4 | 39.1 | +1.7 | 1.045 | 37–39 | 39–41 |
| Probe and copy, 100% | 45.6 | 47.7 | +2.1 | 1.047 | 45–46 | 47–55 |
| Whole step at rest, 0% | 238.6 | 249.4 | +10.8 | 1.045 | 236–244 | 247–263 |
| Whole step at rest, 10% | 250.3 | 260.0 | +9.7 | 1.039 | 244–259 | 255–273 |
| Whole step at rest, 50% | 284.3 | 291.9 | +7.6 | 1.027 | 281–288 | 288–310 |
| Whole step at rest, 100% | 362.9 | 383.4 | +20.5 | 1.056 | 351–378 | 370–401 |
| `ThePolygonStep`, N=256 | 455.2 | 456.9 | +1.7 | 1.004 | 448–474 | 444–472 |
| `ThePolygonStep`, N=1024 | 1634.5 | 1632.2 | −2.3 | 0.999 | 1615–1688 | 1613–1677 |

### Reading them

- **What one probe costs.** Going from 0% to 100% hexagons, the Index walk rises 89.4 µs
  in A and 79.5 µs in B. The difference, 9.9 µs across 1,024 polygons, is the `Get` and
  the copy: **about 10 ns a polygon**.
  - That is higher than the ECS's 2.63 ns for a bare `Get` through a binding, because it
    includes copying six vertices out of an `ecs.List` one `At` at a time.
- **What the extra width costs.** B's flat penalty is +8 µs on the Index walk across 5,248
  Shapes. The two lines cross at about 80% polygons.
- **The Store walk alone** confirms the width costs something even with no Insert:
  +1.7 µs, 4.5%.
- **The whole step** amplifies the width cost, because the `Shape` is copied into every
  index entry and read again by Detect. B never catches up here.
- **With Contacts** (`ThePolygonStep`), GJK and EPA dominate and the two arms cannot be
  told apart.

## Against the ticket's criterion

> Raise the cap if the inline layout wins the Index walk by a margin that survives a scene
> which is mostly circles.

It does not win in a scene of mostly circles. It loses there, and wins only by 1% at 100%
polygons. The ticket's other branch, "leave it at 4 if the probe and the copy disappear
into the walk", holds: about 10 ns a polygon is below the cost of widening every Shape.

## Running it

```
cd bundles/ecsphysics2d/internal
go test -c -o a.exe .
go test -c -tags inline8 -o b.exe .
a.exe -test.run=^$ -test.bench="Proto409|ThePolygonStep" -test.benchtime=1500x -test.benchmem
```

Then alternate `a.exe` and `b.exe`. The benchmarks are in `internal/proto409bench_test.go`,
and the arm switch is `internal/types/proto409_slots{4,8}.go`.
