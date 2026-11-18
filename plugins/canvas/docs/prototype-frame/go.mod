// PROTOTYPE for https://github.com/dvoyni/cog/issues/871: throwaway, outside
// go.work. Run with GOWORK=off go run . (or go run . pixel) from this folder.
module prototype.invalid/canvasframe

go 1.27

require github.com/dvoyni/cog/libs/m v0.0.0

replace github.com/dvoyni/cog/libs/m => ../../../../libs/m
