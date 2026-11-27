# otosound rules

## Code

- The device thread only copies, multiplies and interpolates: the files it runs, the list in `TestTheDeviceThreadsFilesImportNothingThatCouldDecode`, allocate, lock, free and decode nothing, import only what that test allows, and call into neither sfx nor the kernel. A file the device thread comes to run joins that list.
- Whatever the mixer needs is made on the flush side, at `Voices`, `Prepare` or a voice's start, and a clip it read from is let go on the flush side once the applied counter passes its destroy; the allocation tests in `mixer_test.go` and `readahead_test.go` hold a full block at zero.
- The resampler in `resampler.go` is reached from `Prepare` and a streamed voice's read-ahead alone, the allow list of `TestTheResamplerIsReachableOnlyFromPrepareAndTheReadAhead`; the device thread interpolates linearly for a voice's rate and never converts a base rate.
- A ring nobody drains holds nothing: while the handoff ring is not `pulled`, anything new `Emit` keeps for the mixer is let go, as the staging changes, the releases and the read-aheads are. The ring is marked pulled before the `Device` is stored ready, and detached and emptied only after the player has stopped reading, in the order the design's *The handoff ring* and *Loss, the retry, and what it can recover* give.
- Once a device has been ready in a kernel, otosound reports nothing about the device again: a later failure is a loss.

## Tests

- No test opens a real device: a test sets `fakeDevice` on the plugin it builds, through `spawnOverFake` or `makeTestBackend`, since CI has no sound card and a process has one oto context to spend.
- Every piece of state shared between the flush and otosound's goroutines has a race test, with its row in the design's *Concurrency under `go test -race`* table naming the test and the breaks it fails with. A test counting allocations skips its count when `raceEnabled`.

## Docs

- A change to `Config`, the Kernel surface or what a game can rely on of the device is carried into the doc comments, `doc.go`, `README.md` and `docs/design.md` together.
