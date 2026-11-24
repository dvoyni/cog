# canvas rules

## Code

- Nothing canvas does in a steady frame allocates once warm: not recording an entry of any kind, singly or in bulk, not merging the draw lists, sorting, culling or batching, not the instance data written, nor the command lists recorded through each camera, nor text drawn from the layout cache, nor per-frame triangles copied. What may allocate is a first use, a layout the cache has not seen, and a frame's refusals, each outside the gate by nature. `allocationgate_test.go` checks it over canvastest's `Step`, after four warm frames, on gfx's recording adapter recording nothing. A change on the per-frame path keeps that gate green, and a change to its speed is compared with `go test -run '^$' -bench . -count 10 > new.txt` in `plugins/canvas` and `benchstat docs/benchmarks-baseline.txt new.txt`.
