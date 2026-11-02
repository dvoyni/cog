# Kernel rules

## Naming

Identifiers are full words (`pinnedToMainThread`, `combinerMu`), and type names are singular (`dispatcher`, not `eventHandlers`).

Lifecycle verbs come in fixed pairs. Pick the pair by what the code does, and keep each verb to its one meaning:

| Pair | Meaning |
|---|---|
| `new*` / `make*` | Constructors. `new*` returns a pointer (`newKernel`), `make*` returns a value (`makeLockState`), as Go's `new` and `make` do. |
| `init` / `destroy` | Bring up and tear down something that already exists: a plugin, a kernel. A failed `init` destroys what it already initialized, in reverse order. `Start` / `Stop` run between them. |
| `acquire*` / `release*` | Hand out an item from a pool, or a fresh one when the pool is empty, and give it back: `Pool.Acquire` / `Pool.Release`, `acquireJob` / `releaseJob`, `acquireExecutor`. |
