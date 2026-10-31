# assets

`github.com/dvoyni/cog/libs/assets` is **one cache for loaded assets**, keyed by
a comparable descriptor. A plugin builds a cache for one asset family out of a
stateless loader, asks it for an asset by path or by bytes, and gets back a
value that is cached whatever it turned out to be.

It is what `gfx`, `scene` and `canvas` each hand-rolled a version of, and what a
`sound` Slot will use when it exists.
[`specs/assets.md`](specs/assets.md) is the specification the whole Library is
judged against.

assets is a **Library**: it declares no plugin, and it imports only `kernel`. It
is the first Library in the tree to need the kernel, because the Library reports
its own read failures and a cache that reports nothing is the defect this one
exists to remove. The vocabulary is in [`CONTEXT.md`](../../../CONTEXT.md).

Two properties shape everything else:

- **The descriptor is the key and never the handle.** A request and a result are
  different things. A `Descr` names an asset and learns nothing; what the load
  produced is the `T` a `Get` returns.
- **The entry is the once.** The returned value is cached whatever it is, so a
  load runs one time per key until a `Free` — which is where terminal failure,
  report-once and the absence of a retry all come from together.

## The surface

```go
package assets

type Blob struct{ /* a pointer and a length */ }

func NewBlob(b []byte) Blob
func NewBlobFromString(s string) Blob
func (b Blob) Data() []byte
func (b Blob) String() string
func (b Blob) Len() int
func (b Blob) MarshalJSON() ([]byte, error) // {"len":N}, never the bytes

type Descr[P comparable] struct {
	Name   string // a storage path; empty when the asset is named by its Blob
	Blob   Blob   // the bytes, when the caller holds them
	Params P      // the bake parameters that make one source several assets
}

type Loader[P comparable, U any, T any] interface {
	Load(k kernel.Kernel, data Blob, params P, fsys fs.FS, user U) T
	Default(d Descr[P], user U) T
	Free(value T, user U)
}

type Cache[P comparable, U any, T any] struct{ /* entries */ }

func New[P comparable, U any, T any](loader Loader[P, U, T]) *Cache[P, U, T]

func (c *Cache[P, U, T]) Get(k kernel.Kernel, d Descr[P], fsys fs.FS, user U) T
func (c *Cache[P, U, T]) Free(k kernel.Kernel, d Descr[P], user U)
func (c *Cache[P, U, T]) FreeAll(k kernel.Kernel, user U)
func (c *Cache[P, U, T]) FreeWhere(k kernel.Kernel, user U, match func(Descr[P], T) bool)
```

## `Blob`

A run of bytes treated as static: once a value holding it is built, nothing
writes the bytes again. It is a **pointer and a length**, so constructing one
allocates nothing — which is what keeps a descriptor built per frame free — and
it is **comparable**, which is what lets a `Descr` holding one be a map key.

**Identity is the run of bytes, not their contents.** Two allocations spelling
the same bytes are two Blobs; re-wrapping the same slice is one.

```go
source := []byte("fn main() {}")
copied := slices.Clone(source)

seen := map[assets.Blob]struct{}{}
seen[assets.NewBlob(source)] = struct{}{}
seen[assets.NewBlob(source)] = struct{}{} // one backing array, one entry
seen[assets.NewBlob(copied)] = struct{}{} // another array, another entry
len(seen) // 2
```

Count identities through a map rather than with a `==` between two constructor
calls: the compiler reuses one stack slot across such a comparison, so it
reports an equality the heap does not have. Only keeping the values alive at
once tells the truth.

**A Blob used as a cache key must be built once and kept.** A fresh
`[]byte{...}` at the call site is a fresh asset every time it runs, so the cache
fills with entries nothing can ask for twice. A package `var`, a `const` or a
string literal is the shape; bytes from an arena or a per-frame buffer are not.

**A Blob encodes to JSON as its length, `{"len":N}`, and never its bytes.** A
Blob can be texture-sized, and the ECS's [read by Component
name](../../../bundles/ecs/docs/README.md#reading-the-world-by-name) encodes a
Component while every System waits on it, so the size is shown and the payload
is not. An exported Blob field, such as `Descr.Blob`, encodes that way rather
than as the `{}` its unexported fields would give. Nothing decodes one.

**Inline text goes through `NewBlobFromString`.** A string literal, `const` or
package `var` resolves to the same address every evaluation, so the *built once
and kept* rule is satisfied by an ordinary Go constant with no `var` ceremony.
`[]byte(s)` allocates a fresh array per conversion and is a fresh identity every
time; so is a computed string. Measured, five calls each:

| built with | entries |
| --- | --- |
| `NewBlobFromString(literal)` | 1 |
| `NewBlob([]byte(literal))` | 5 |
| `NewBlobFromString(computed)` | 5 |

**`Blob{}` is the canonical empty.** `NewBlob(nil)`, `NewBlob([]byte{})` and
`NewBlobFromString("")` are all it, `Data()` on it is nil and `String()` is `""`.
That is what makes a path-named `Descr` legal and stops two empty blobs
comparing unequal. The cost is that a zero-byte asset cannot be named by its
bytes, and a zero-byte asset is not a thing.

**`Data()` hands out live memory, and on a string-backed Blob that memory is
read-only**, so a write faults at the offending write. That is the good kind of
hazard: a crash where the contract was broken beats silent corruption of a live
cache key.

## `Descr`

The whole of what names one asset: a storage path, or bytes the caller already
holds, plus the typed bake parameters that make one source several assets — the
size a font is baked at, the colour space a texture is uploaded in, the
preprocessor supply a shader is flattened with.

**`Name` wins when it is set.** The key is `{Name, Params}` when `Name != ""`
and `{Blob, Params}` otherwise, so a `Blob` supplied beside a `Name` is a
**payload the Library does not read**. The rule in one line: *you named it, you
own the name* — two callers supplying different bytes under one `Name` get
whichever arrived first, exactly as a path already behaves.

A `Free` therefore keys the same way a `Get` does: naming the path alone retires
the entry the payload was supplied beside.

**`P` replaces a query string.** It is typed and comparable, so there is nothing
to escape and no canonical ordering for the Library to enforce.

**A plugin's public descriptor may *become* a `Descr`** rather than map onto one:

```go
type ShaderDescr assets.Descr[ShaderDescrParams]
```

`Descr`'s fields are exported, so anyone can write one down — but the `P` type's
own fields stay **unexported**, so the only descriptor a caller can spell by
hand carries a zero `P`, which is the no-options constructor. Anything whose
spelling is load-bearing lives behind that. A defined type inherits no methods
from its underlying type, so a plugin writes its own one-line delegates and
keeps exactly the public surface it has today.

## `Loader`

The plugin-side half: it decodes bytes into `T`, supplies the value for an asset
that did not load, and releases one.

**It is stateless and long-lived.** Built once, at plugin construction, it
outlives every handler, so everything lock-bound arrives per call and is stored
by nothing:

- **`k kernel.Kernel`** — a `Kernel` is created per dispatch, and retaining one
  past the handler that received it is a bug.
- **`fsys fs.FS`** — `storage.FileSystem` is a Resource held under a read lock
  and is never retained past the handler's lock scope. `Load` receives it too,
  so a loader that needs more than one file — a shader reading its root plus the
  includes its own contents name — can open the rest.
- **`user U`** — the plugin-defined pass-through carrying whatever the loader
  needs that only a handler holds: a resource queue, a backend, a packer. The
  cache never inspects it, so it is emphatically not a route for the kernel.

**The Library reads; the loader decodes.** The Library opens `Name` through
`fsys` and hands the bytes to `Load` as a `Blob`. A descriptor with no `Name`
skips the read.

**Everything finishes inside `Load`.** `T` is immutable and nothing is ever
pending in the cache, so a plugin that wants asynchrony puts it **in front of**
`Get` and keeps its own not-yet-requested state.

**`Load` takes a kernel so a loader can report its own faults.** The Library
owns the *read* failure; everything a loader itself finds wrong — a missing
texture inside a model, a primitive with no declared bounds — goes to the kernel
as an error at the point it is produced, and the consumer decides which of them
is fatal.

**`Loader.Free` must not call `Free` on the same cache.** Re-entering a
*different* cache is allowed and expected, and re-entering its own with a `Get`
is allowed too. Only freeing its own is out, and it is the one rule the Library
cannot enforce.

## `Cache`

One table of entries for one asset family, generic in `P`, `U` and `T`. A plugin
holds several: a model table and a texture table are two instances, not two
kinds. A `Cache` is not internally synchronised — it is an ordinary engine
table, living in a resource and reached under whatever lock its holder was
granted, which is why the kernel and the filesystem arrive per call.

### `Get`

```go
func (c *Cache[P, U, T]) Get(k kernel.Kernel, d Descr[P], fsys fs.FS, user U) T
```

Returns the asset `d` names, loading it on a miss. It returns `T` and nothing
else: every plugin's `T` already carries its own state, and a second answer from
the cache would be a second source of truth about the same thing.

### `Free`, `FreeAll`, `FreeWhere`

```go
func (c *Cache[P, U, T]) Free(k kernel.Kernel, d Descr[P], user U)
func (c *Cache[P, U, T]) FreeAll(k kernel.Kernel, user U)
func (c *Cache[P, U, T]) FreeWhere(k kernel.Kernel, user U, match func(Descr[P], T) bool)
```

**All three free immediately. There is no queue**, and a free followed by a get
is a **reload, not an error**. Releasing a value needs the loader, because `T`
is immutable and holds backend handles: dropping an entry without telling it
leaks the texture, the buffer or the atlas slot.

**They collect and remove before they free**, so a loader that reaches back into
its own cache inside `Free` cannot corrupt the walk. `FreeAll` walks whatever is
in the table at the call, so an asset asked for after it and before the frame
ends was deliberately asked for and survives.

**`FreeWhere` exists because a release decision can read the value**: *evict
every shader whose sources contain this path*, every face baked from this font
file, every colour-space variant of this image. The caller cannot name those
keys, and the value already holds the answer — which is why this is not a
plugin-side reverse index. `match` is shown the descriptor as the table keys it.

**Freeing at the call needs the device at the call**, which is a lock a plugin's
unload verb may not previously have held. Unloading is a level-boundary act, so
that cost lands on a tick that is already stalling — but it must not reach a
handler that never unloads, which is what a split facade is for.

## Failure is terminal, and reported once

**The Library reports its own read failure**, once, through
`kernel.ReportErrorOnce`:

```
asset gfx.texture not found by path "textures/rust.png": open textures/rust.png: file does not exist
```

The message **names the asset by the type it would have produced**, so there is
no label on `New`, nothing to configure and nothing to keep in sync. Storage's
error is wrapped untouched, so `errors.Is(err, fs.ErrNotExist)` keeps working
for anyone who wants the distinction the Library does not draw.

**The key is the `Descr` itself.** `ReportErrorOnce` takes any comparable, and
distinct key types never collide however their values compare, so `Descr[P]`
owns a namespace per instantiation and needs no `"sprite:"` or `"model:"`
prefix.

**Why the Library and not the loader:** a loader-side report is *optional*, and
a cache that lets a consumer stay quiet is not a fix. Reporting is half of
*gets failure right*, and half a guarantee is not one.

**Failure is terminal.** The returned value is cached whatever it is, so `Load`
is not called again until a `Free`. **`Free` is the retry lever**, and it is
also the paired forget the kernel requires — without it a sprite that failed to
load once stays silent about failing again for the engine's life. That is why
`Free` takes a kernel at all: it pairs with `ForgetReportedError`, and `FreeAll`
with the family forget.

## `Default` is a value, never an error

```go
Default(d Descr[P], user U) T
```

**A method, not a field**, so it can ask a device for its 1×1 magenta at call
time rather than being built before the backend exists. **It takes the
descriptor**, so a placeholder can match the shape that was asked for.

**It never sees the error.** The Library has already reported; `Default`
supplies the *value*, and what gets drawn is a separate question from what gets
said.

**A default is only sometimes a picture.** Magenta for an sRGB slot and the zero
value for a linear one, because magenta as a normal map is a surface lit from
nowhere; the driver's white where that is what an unset handle already means; a
skip where the element was laid out at 0×0 anyway. The shape that transfers is
`Default` as a **null object**, with the loudness coming from the report rather
than from the pixels.

## What this Library is not

Each of these is something a reader will look for and not find, and in three
cases something a migration wanted and did without.

- **Not a probe.** There is no *is this loaded?* and no *what is this, without
  loading it*. `Get` is the only read, and it loads on a miss.
- **Not a walk.** There is no fifth verb. Both plugins that wanted a whole-table
  number wanted a *number*, and a number is a running counter maintained in
  `Load` and `Free`. `Each` would hand out a read over the cache's own table, a
  lifetime the Library never otherwise exposes.
- **It does not refcount.** An entry is freed when it is freed.
- **It has no budget and evicts nothing by itself.**
- **It does not watch anything.** No filesystem watch and no hot-reload: the
  asset filesystem is static by construction, so there is nothing to notify
  about. A game that overwrites a file it also loads is the one caller that
  already knows it did, and already has the verb.
- **It does not know what a frame is.** No `BeginFrame`, no tick, no boundary.

## The boundary a game cannot see

**The Library is plugin-internal.** The cache type appears in no plugin's public
API; each plugin keeps its own verbs — `UnloadModel`, `UnloadSprite`,
`ReleaseCachedResourceCmd`. The test of a successful migration is that **a game
cannot tell the plugins share a caching implementation.**

The architecture protects this rather than discipline: `libs/*` may import only
`libs` and `kernel`, and `events.go` is a root file allowed to Slots and Bundles
only, so `libs/assets` could never declare an event and there is no route by
which a plugin's cache could announce itself engine-wide.
