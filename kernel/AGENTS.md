# Kernel rules

## Code

- A composition error (registering something invalid, a bad `Spawn` argument) panics. A runtime failure goes to `LogErr` and the kernel carries on.
- Task args, task results and events are pointer-free: no pointers, slices, maps, strings, interfaces, channels or funcs at any depth.
- A type a socket is declared of, interface or value, is named with the suffix `Socket`: `platform.WriteFSSocket`, `platform.ReadMountSocket`. A type merely used by one, such as `fs.FS` in a `ReadMountSocket`, is not.
- `configurator_gen.go` is written by `internal/generate`: change the generator and run `go generate`, never the output.

## Tests

- Tests reach a kernel the way plugins do: spawn it through the `Supervisor` (`spawnTestKernel`, `spawnHooked`) and register in a plugin's `Init`. Internals are read to assert, never called to stand in for a lifecycle step.
- An internal module with an interface of its own, such as `lockState` or `jobGraph`, is tested at that interface, without a kernel.

## Potential rules
