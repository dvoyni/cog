# sfx rules

## Code

- A voice is restated to the backend by one test: each live voice holds the params the backend last had of it, `tableSlot.heldParams`, and `collect` sends an update exactly when the params `computeParams` gave differ, or a start carrying them when the voice owes a restart, after a seek, a change of `Loop` or the device arriving. A new cause of a voice sounding otherwise goes into what `computeParams` computes, never into a restate of its own, as a bus volume, the listener and the pause have none.
- W3C's equations are in `w3c.go` alone, transcribed from the W3C document down to its order of operations and its degrees-then-radians round trip, every addition a guard named where it is added. Audibility is computed in one place, `VoiceOptions.computeAudibility`, for a play ranked as it is recorded and for a live voice's params alike, so the two compare to the bit. Nothing below the seam computes any of it.
- The flush keeps the order the design's *The flush on the kernel* gives; a step moved is moved there, in `flush`'s body and in its doc comment together.
- Nothing a steady flush does allocates once warm, but a first play or preload of a path, its read, its prepare and its reports. `allocationgate_test.go` checks it over sfxtest's `Step`, and a change on the flush's path keeps it green: scratch the flush fills is sized to its bound, `MaxVoices`, in `Init`.

## Tests

- A test drives sfx through `sfxtest` and asserts on what the flush computed and published, never below the seam. The arithmetic is pinned by plain package tests in `w3c_test.go` against W3C's numbers, each with a tolerance. A test needing a device that is not ready or arrives, or a prepare answering `done=false`, binds `fakeBackend`, in `fakebackend_test.go`, through `UseBackend`.

## Docs

- A change to sfx's public API is carried into its doc comments, `doc.go`, the brief `README.md`, the guide `docs/README.md`, `docs/design.md`, and `CONTEXT.md` for a new term. The guide's every Go block compiles beside its quick start, in a package `main` of a scratch module on the workspace, and its tests run green there.
