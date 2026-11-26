# canvas rules

## Code

- Nothing canvas does in a steady frame allocates once warm: not recording an entry of any kind, singly or in bulk, not merging the command lists, sorting, culling or batching, not the instance data written, nor render's command lists recorded through each camera, nor text drawn from the layout cache, nor per-frame triangles copied. What may allocate is a first use, a layout the cache has not seen, and a frame's refusals, each outside the gate by nature. `allocationgate_test.go` checks it over canvastest's `Step`, after four warm frames, on gfx's recording adapter recording nothing. A change on the per-frame path keeps that gate green, and a change to its speed is compared with `go test -run '^$' -bench . -count 10 > new.txt` in `plugins/canvas` and `benchstat docs/benchmarks-baseline.txt new.txt`.
- canvas reaches no gfx: every GPU object and call goes through render's `Inventory` and `Frame`. What canvas needs that render lacks is a gap in render, closed in render with a line under the design's *Delta to render*, not worked around in canvas.
- A mistake canvas finds at run time is a `Report` of a `ReportKind` of its own, logged once for its subject through the kernel's `LogErrOnce` and carried past, the entry skipped or drawn the nearest way it can be, and named on the call that raises it and on its kind. A programming error its caller can check before the call panics instead: `SetCamera` with a sample count of 2, `MakeLayers` with five ranges, `ArgumentOf` of a type holding a pointer.
- Every name canvas's include, `assets/canvas.wgsl`, declares begins `canvas_`, a prefix a material's shader leaves to canvas. A change to a shader under `assets` regenerates its binding declaration beside it, with `go generate` from `builtinassets.go`'s lines; a test shader's, from `material_test.go`'s.

## Tests

- Each rule is tested at the cheapest level that shows it: plain unit tests of internals for sorting, the camera's fit and conversions, layout and markup; recording-adapter tests through `canvastest`, in a real kernel, asserting what reached render in render's words; and pixel tests on wgpu's software adapter only for what only pixels show. A test drives canvas as a plugin does, through the kernel and canvas's parameters, never calling its internals to stand in for a lifecycle step.
- A pixel test compares probes, texels where no filtering reaches, that hold at any display scale from 1, since a window's pixels per point come from the display it opens on: a value expected at its pixel's actual centre, a pixel compared with another of the same picture, or a distance field modelled as `DistanceField` stores it. It allows ±2 a channel only where a probe must sit in a gradient, and no golden image files.
- Each piece of state jobs share, behind a mutex or an atomic, is tested under `go test -race` through `concurrentUser`, most in `concurrency_test.go`, and seen failing with its guard removed. canvas's whole suite, pixel tests included, runs under the detector.
- Text in tests is drawn in the embedded default font, JetBrains Mono, or a font in `testdata/fonts` with its licence beside it and its line in `testdata/fonts/README.md`, so shaping is the same on every machine.

## Docs

- A change to canvas's public API is carried into its doc comments, `doc.go`, the brief `README.md`, the guide `docs/README.md`, whose every example compiles and whose tests run, `docs/design.md`, and `CONTEXT.md` for a new term.
