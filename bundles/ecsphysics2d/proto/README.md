# PROTOTYPE: query index cost (cog#345)

**Throwaway. Never merges.** Lives on `proto/query-index-cost` only. Delete the
branch once [prototype: what a sweep costs per call on each index, and which
structure holds it down](https://github.com/dvoyni/cog/issues/345) is closed
and the spec has taken the verdict.

It answers one question: what a `Sweep` and a `SweepAll` cost per call on
`StaticIndex` and `BodyIndex` at nox's reference workload, and which internal
structure holds that cost down. The results and the verdict are in
[`docs/research/query-index-cost.md`](../../../docs/research/query-index-cost.md).

## Layout

| Path | What |
| --- | --- |
| `queryindex/shape.go` | Closed-form sweeps: a circle (a point is radius 0) against a circle, box or segment. Exact rounded growth, cog#306's `Hit`. |
| `queryindex/index.go` | `Linear`, the brute-force oracle. |
| `queryindex/grid.go` | Uniform grid: rectangle scan for short sweeps, an ordered cell walk with early-out for long ones. |
| `queryindex/hashgrid.go` | The same grid with hashed cells: no world extent. |
| `queryindex/bvh.go` | Median-split BVH, rebuilt or refit, walked nearer-child-first. |
| `queryindex/workload.go` | Seeded layouts and query sets. |
| `queryindex/*_test.go` | Oracle agreement, zero-allocation checks, benchmarks, shape-test counts. |
| `baseline/` | A separate module: `cp` and `gox2d` on the same workload, so the numbers to beat are measured again rather than quoted. |

## Run

```sh
cd bundles/ecsphysics2d/proto/queryindex
go test .                                   # oracle agreement + zero allocations
go test -run XXX -bench . -benchtime 200ms  # every benchmark (~6 min per count)
STATS_OUT=stats.tsv go test -tags protostats -run TestStats .   # shape tests per query
```

The benchmarks call each index through an interface so one harness covers
them all. The real surface has none (cog#306), and every candidate pays the
same dispatch here.
