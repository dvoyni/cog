# render rules

## Code

- Nothing render does per frame allocates once its pools have grown: not a command list, a pass, a parameter, a draw, a dispatch, a copy, a temporary, the clear-after-draw check or the frame's end. What may allocate in a warm frame is a fixed count that `rendertest/allocationgate_test.go` checks: a material version, and a pass given a temporary range as a `@group(0)` storage parameter where the range moved. A change on the per-frame path keeps that gate green, and a change to its speed is compared with `go test -run '^$' -bench . -count 10 > new.txt` in `rendertest` and `benchstat docs/benchmarks-baseline.txt new.txt`.
