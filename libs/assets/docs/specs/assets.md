# cog assets — specification

`github.com/dvoyni/cog/libs/assets` is a Library holding **one cache for loaded
assets**, keyed by a comparable descriptor. It declares no plugin. It is what
`gfx`, `scene` and `canvas` each hand-rolled a version of, and what a `sound`
Slot will use when it exists.

This document specifies the whole of it: what a `Blob`, a `Descr`, a `Loader`
and a `Cache` are, who reads, who reports, what a failure means and how long it
lasts, what release costs and whose cost that is — and then what each of the
three migrations does to the plugin it lands on, because a Library nobody has
moved onto is a design, not a decision.

The design is bound by four requirements, in this order, and every decision
below was taken against them:

1. **A cache that gets failure right by construction.** Three of the four caches
   in the tree today fail silently, and one of them re-decodes a missing file
   every frame forever. A cache that lets a consumer stay quiet is not a fix.
2. **No plugin pays a wider lock set for it.** Specifically: no ECS System, and
   no handler that does not load.
3. **It deletes more than it adds.** The measure of the migration is lines
   removed from the plugins, not lines added to the Library.
4. **A game cannot tell.** The cache type appears in no plugin's public API;
   each plugin keeps its own verbs.

Two properties fall out of taking those in that order, and most of what follows
is a consequence of one or the other. **The descriptor is the key and never the
handle** — a request and a result are different things, which is not true in the
tree today. And **the entry is the once**: the returned value is cached whatever
it is, so a load runs one time per key until a `Free`, which is where terminal
failure, report-once and the absence of a retry all come from together.

This document is the specification the implementation will be judged against. It
is assembled from the resolved tickets of
[assets: one Library cache for loaded assets, keyed by a comparable descriptor](https://github.com/dvoyni/cog/issues/443);
every section cites the tickets it came from. Where a claim rests on something
unverified it is marked **Gap** and says what would settle it; where assembling
these decisions next to each other settled something no ticket did, it is marked
**Settled here**.

**Nothing in this document is built.** `libs/assets` does not exist,
`libs/m/blob.go` does, and every file path below names the tree as it stands
before the change. [Required work](#required-work) is the checklist to build it
from.

**One thing here is younger than the rest and reverses a premise the map was
charted on.** The map assumed that making failure terminal gave up a real
developer loop and that something would have to drive eviction to get it back.
It does not, and nothing does:
[terminal failure has no retry lever](https://github.com/dvoyni/cog/issues/449)
found that **nothing an asset is loaded from ever changes**, so there is no
change to notify about. [The loss, stated](#the-loss-stated) says what that
costs and what it does not, and the sections that were written the other way say
what they used to say.

---

## Contents

- [Vocabulary](#vocabulary)
- [The surface](#the-surface) · [Blob](#blob-identity-is-the-run-of-bytes-not-their-contents) · [Descr](#descr-you-named-it-you-own-the-name) · [Loader](#the-loader-is-stateless-and-long-lived) · [Cache](#cache-four-verbs-and-no-fifth)
- [Loading](#loading) · [Failure](#failure-is-terminal-and-reported-once) · [Default](#default-is-a-value-never-an-error)
- [Release](#release) · [What freeing costs](#what-freeing-costs-and-whose-cost-that-is)
- [What the Library is not](#what-the-library-is-not)
- [The boundary a game cannot see](#the-boundary-a-game-cannot-see)
- [gfx](#gfx-two-caches-behind-a-thread-boundary) · [scene](#scene-the-state-machine-deletes) · [canvas](#canvas-five-caches-and-a-packer)
- [`canvas.UnloadAll`, the one verb this design adds](#canvasunloadall-the-one-verb-this-design-adds)
- [The costs this design accepts](#the-costs-this-design-accepts) · [The loss, stated](#the-loss-stated)
- [How this is tested](#how-this-is-tested)
- [Shapes that were rejected](#shapes-that-were-rejected)
- [Required work](#required-work) · [Out of scope](#out-of-scope)

---

## Vocabulary

**Asset** is in `CONTEXT.md` and is the glossary of record; this list is a
reading aid for the words this document adds, and [Required
work](#required-work) carries the two `CONTEXT.md` entries that stop being true.

- **Asset** — data a plugin loads and holds: named by a path under storage or
  supplied directly as bytes, decoded, installed into a backend, kept under a
  key, and released explicitly.
- **Blob** — a run of bytes treated as static: once a value holding it is built,
  nothing writes the bytes again. Its identity is the run, not the contents.
- **Descriptor** (`Descr[P]`) — the whole of what names one asset: a storage
  path, or bytes, plus the typed bake parameters that make one source several
  assets. It is comparable, and it is the cache key. It is **not** a handle, and
  it never learns anything.
- **Bake parameters** (`P`) — the part of a descriptor that is not the source:
  the size a font is baked at, the colour space a texture is uploaded in, the
  preprocessor supply a shader is flattened with.
- **Loader** — the plugin-side half: it decodes bytes into `T`, supplies the
  value for an asset that did not load, and releases one. Stateless and
  long-lived; everything lock-bound arrives per call.
- **User data** (`U`) — a plugin-defined pass-through the cache never inspects,
  carrying whatever the loader needs that only a handler holds: a resource
  queue, a backend, a packer.
- **Cache** — one table of entries for one asset family, generic in `P`, `U` and
  `T`. A plugin holds several.
- **Entry** — one descriptor and the value its load produced, successful or not.
- **Residency** — retired as a *state*. "Resident" means the cache has an entry
  and it is not a failure; there is no loading and no missing, because there is
  nothing in flight.
- **Terminal failure** — a load that failed and whose failure value is cached.
  It is not retried until the entry is freed.

---

## The surface

```go
package assets

type Blob struct{ ptr *byte; len int }

func NewBlob(b []byte) Blob
func NewBlobFromString(s string) Blob
func (b Blob) Data() []byte
func (b Blob) String() string
func (b Blob) Len() int

type Descr[P comparable] struct {
	Name   string // a storage path; empty when the asset is named by its Blob
	Blob   Blob   // the bytes, when the caller holds them
	Params P      // the bake parameters that make one source several assets
}

// Loader is stateless and long-lived: everything lock-bound arrives per call.
type Loader[P comparable, U any, T any] interface {
	Load(k kernel.Kernel, data Blob, params P, fsys fs.FS, userData U) T
	Default(d Descr[P], userData U) T
	Free(value T, userData U)
}

type Cache[P comparable, U any, T any] struct{ /* entries */ }

func New[P comparable, U any, T any](loader Loader[P, U, T]) *Cache[P, U, T]

func (c *Cache[P, U, T]) Get(k kernel.Kernel, d Descr[P], fsys fs.FS, userData U) T
func (c *Cache[P, U, T]) Free(k kernel.Kernel, d Descr[P], userData U)
func (c *Cache[P, U, T]) FreeAll(k kernel.Kernel, userData U)
func (c *Cache[P, U, T]) FreeWhere(k kernel.Kernel, userData U, match func(Descr[P], T) bool)
```

`Descr`'s fields are **exported**; a `P` type's own fields generally are not,
and [Descr](#descr-you-named-it-you-own-the-name) says why.

**`libs/assets` is the first Library in the tree to import `kernel`.** The
import table always allowed it (`libs/*` may import `libs` and `kernel`) and
nothing had needed it: `libs/m`'s entire import set across eighteen files is
`iter`, `math` and `testing`. It needs it because the Library reports, and
[Failure](#failure-is-terminal-and-reported-once) is why that is not
negotiable.

### `Blob`: identity is the run of bytes, not their contents

From [Blob becomes comparable, and what that costs the ECS](https://github.com/dvoyni/cog/issues/444).

```go
// Blob is a run of bytes treated as static: once a value holding it is built,
// nothing writes the bytes again.
//
// A Blob used as a cache key must be built once and kept. Its identity is the
// backing array and the length, so a fresh []byte{...} at the call site is a
// fresh asset every time it runs. A package-level var, a string literal or a
// const is the shape; bytes from an arena or a per-frame buffer are not.
type Blob struct {
	ptr *byte
	len int
}
```

`m.Blob` is **deleted**, not moved and not changed in place: the type is
`assets.Blob` and every one of its eleven referencing files outside its own
follows it.

**A pointer and a length, not a box.** The map first settled
`struct{ data *[]byte }`, where the box is the identity, and it is rejected on a
fact about where the type is used rather than on taste. `Blob` is also the type
of `BufferDescr.bytes`, `TextureDescr.pixels` and `ParameterDescr.raw`, which
are built **per frame**: `BufferWithBytes` is a pure value construction today —
zero allocations — at `canvas/internal/batch.go:148`, `:162` (in a loop over
named arrays), `:282` and `scene/internal/types/meshdraw.go:260`, `:265`. A box
makes every one of those a heap allocation, because `&b` on the parameter
escapes. The pointer-and-length shape keeps them free.

`unsafe` is sanctioned here by precedent rather than by exception:
`slots/gfx/internal/types/material.go:109` already fingerprints
`unsafe.SliceData(t.pixels)` for exactly this identity, two directories from
where the type now lives, and there is no rule against it in `.golangci.yml` or
the instructions. That fingerprint gets **simpler**, because the Blob now holds
what it was computing by hand.

Three things were checked rather than assumed:

- **Retention still holds.** Go's collector keeps a whole allocation alive from
  an interior pointer, so `ptr` pins the backing array exactly as a slice header
  does. Retention stays structural: the key holds the bytes alive for as long as
  the entry lives.
- **Losing `cap` is free, and slightly better.** `Data()` yields `cap == len`.
  Nothing in the tree appends *into* a Blob — the three `append` sites
  (`parameter.go:223`, `resourcequeue.go:101`, `:118`) all copy *out* of one — so
  an accidental append reallocates instead of writing into whatever followed it
  in an arena. The static contract gets marginally harder to violate by accident.
- **The one undefined case cannot arise.** `unsafe.SliceData` on a zero-length
  slice returns an unspecified pointer, and `NewBlob` short-circuits `len == 0`
  to `Blob{}` before reaching it. Those two rules need each other and belong next
  to each other.

**`Blob{}` is the canonical empty.** `NewBlob(nil)` and `NewBlob([]byte{})` both
return it and `Data()` on it returns nil rather than panicking, which is what
makes a path-named `Descr` legal and stops two empty blobs comparing unequal.
The cost is that a zero-byte asset cannot be named by its bytes, and a zero-byte
asset is not a thing.

**Strings go through `NewBlobFromString`, and the difference is the whole cache.**
Measured, five calls each:

```
entries after 5 NewBlobFromString(literal): 1
entries after 5 NewBlob([]byte(literal)):  5
entries after 5 computed strings:          5
```

`[]byte(s)` allocates a fresh backing array per conversion, so routing inline
text that way makes every `ShaderWithText("// custom")` its own entry.
`unsafe.StringData` does not copy, and a string **literal**, `const` or package
`var` resolves to the same rodata address every evaluation — so the *built once
and kept* rule is satisfied by an ordinary Go constant, with no `var` ceremony.
A **computed** string is five entries, which is the same rule and the same
sentence.

That measurement is easy to take wrongly: `fromBytes() == fromBytes()` reports
`true`, because the compiler reuses one stack slot across the comparison. Only
the loop tells the truth, and the loop disagrees.

**`Data()` on a string-backed Blob hands out read-only memory, and that is the
good kind of hazard.** A write faults immediately:

```
b.Data()[0] = 'X'
-> unexpected fault address 0x7ff6c0777189
   fatal error: fault [signal 0xc0000005]
```

The contract is *nothing writes the bytes again*. A violation that crashes at
the offending write beats one that silently corrupts a live cache key, so this
belongs in `Data()`'s doc comment rather than being designed away.

**The ECS pays nothing.** `Storable` keeps admitting `Blob` by type identity
exactly as before; `blobType` becomes `reflect.TypeFor[assets.Blob]()` and
`bundles/ecs/internal/types/component.go` imports `libs/assets` where it imported
`libs/m`, which the import table allows. `PointerFree` is unaffected — a struct
holding a `*byte` is not pointer-free, and neither was a `[]byte` — so **no
Component changes fast-path class**. It reads backwards unless it is said out
loud: the ECS does not care about caching, it cares that *this exact type* is the
engine's one static byte run, and the Library is where that type now lives.

One sentence in the ECS's own doc is **corrected rather than relocated**. It
reads *"a read handing out a copy of a static Blob hands out nothing a reader can
use to change the Store"*, and with `Data()` returning a live slice that is as
untrue after the change as before it; `component.go:183` already admits the
exception is one of trust, and the sentence above it has to stop implying
otherwise. What the new shape genuinely buys, and what replaces it: **the field
is unexported, so no holder can repoint the bytes under an entry keyed on them.**
That is new, and it is why the type moved rather than merely gaining a method.

**`Id() uint64` stays dropped.** Nothing in the tree wants to *name* a Blob:
canvas's `mcpprovider.go` and `snapshot.go` carry none, there are no goldens over
Blob bytes, and the Library's one report names an asset by path. It stays cheap
to add — an unexported field and a method, churning no call site — and that
reason belongs on the type so the next reader does not re-derive it.

### `Descr`: you named it, you own the name

From [#444](https://github.com/dvoyni/cog/issues/444), amended by
[scene migration](https://github.com/dvoyni/cog/issues/447).

**`Name` wins when it is set.** The key is `{Name, Params}` when `Name != ""`
and `{Blob, Params}` otherwise. A `Blob` supplied beside a `Name` is a
**payload**, not part of the identity — and a payload is what `Load` is handed
**in place of a read**: the file the `Name` spells is not opened. Read it as a
rule about both halves, because it is one. The key ignores the bytes; the load
uses them and nothing else.

The rule in one line: **you named it, you own the name.** Two callers supplying
different bytes under one `Name` get whichever arrived first, which is exactly
how a path already behaves.

Scene forced this, and it is worth recording why the obvious rule — *bytes or a
path, never both* — could not survive. A GLB-embedded image has no storage path
of its own: it keys on `{model path, image index, srgb}`, and its encoded bytes
are already in hand inside the model's parse. Under the old rule those two facts
could not both be served. `Name` alone means the Library reads `Name` — the whole
container — and the loader re-parses it to reach image N, and
`gltftexture.go:21-43` records that the vendored set is *all* single-buffer GLBs,
so that is the common path and not a corner. `Blob` alone leaks: the key would
move with every parse, so a model reloaded after an unload strands its old
texture entries, which nothing frees, because `UnloadModel` deliberately does not
cascade to textures.

For a **blob-named** asset the bytes are the identity, so a fresh `[]byte{...}`
literal is a fresh identity and caches twice, and a package `var`, `const` or
string literal is one. Re-wrapping the *same* slice is the same identity.

**`P` replaces a query string.** It is typed and `comparable`, so there is
nothing to escape and no canonical ordering for the Library to enforce. gfx's
`canonicalSupply` stays gfx's, which is where it already lives — and it is there
because **`&` is unsound as a separator**: *"a const A whose value is `x&B`
renders byte-identically to the define pair A=x, B ... two supplies would share
one cache entry and one of them would draw the wrong module."*

**The descriptor is the key and never the handle.** gfx's `TextureDescr` is today
both the request and the result, which is why `TextureWithResource(path).ID()` is
`0` forever and `Size()` reports `(0, 0)` permanently. Separating them is what
lets the cache keep the decoded size at all.

**A plugin's public descriptor may *become* a `Descr` rather than map onto one**
— `type ShaderDescr assets.Descr[ShaderDescrParams]` — and where it does, the
**`P` type's own fields stay unexported**. `Descr`'s fields are exported, so
anyone can write `gfx.ShaderDescr{Name: "app.wgsl", Params: ...}`, but only with
a *zero* `Params`, which is the no-options constructor: canonical, legal,
harmless. Anything whose spelling is load-bearing — a supply string, a baked
texture id — must live behind that. A defined type inherits no methods from its
underlying type, so a plugin writes its own one-line delegates and keeps exactly
the public surface it has today.

### The `Loader` is stateless and long-lived

From [The Loader interface's missing halves](https://github.com/dvoyni/cog/issues/445),
amended by [#447](https://github.com/dvoyni/cog/issues/447).

Built once, at plugin construction, and it outlives every handler. Everything
lock-bound arrives per call:

- **`k kernel.Kernel`** — because `Kernel` is *"created per dispatch and always
  passed by value ... retaining it past the handler that received it is a bug"*,
  and `Kernel.engine` is unexported, so a value that cannot be constructed cannot
  be held by a long-lived loader. It arrives on `Get`, `Free`, `FreeAll`,
  `FreeWhere` and `Load`, and is **never stored**.
- **`fsys fs.FS`** — because `storage.FileSystem` is a Resource held under a read
  lock and both existing `LookupAccess` types exist so it is *"never retained
  past the handler's lock scope."* `Load` receives it too, so a loader that needs
  more than one file — a shader reading its root plus N includes discovered by its
  own contents — can open the rest.
- **`userData U`** — the plugin-defined pass-through: a `*gfx.ResourceQueue`, a
  backend, a packer, a `*Lookup`. The cache never inspects it.

`U` is what lets the loader be built once instead of rebuilt per handler, which
is what the previous map's `Access` had to do. It is emphatically **not** a route
for the kernel: `U` is opaque to the cache by construction, so a cache holding a
`U` still has nothing it can call.

**`Load` takes a kernel so a loader can report its own faults.** The Library owns
the *read* failure; everything a loader itself finds wrong is the loader's. A
model load produces a missing texture, a primitive with no declared bounds, a
sampler it could not read — and before this amendment `Load` had nowhere to put
them. **All of it goes to the kernel as errors, at the point they are produced,
and the consumer decides which of them is fatal.** A plugin does not pre-sort its
reports into tiers.

This does not reopen what [#445](https://github.com/dvoyni/cog/issues/445)
settled. What was forbidden was making the *read* report optional, not handing a
loader a kernel for its own faults.

**`Loader.Free` must not call `Free` on the same cache.** Re-entering a
*different* cache is allowed and canvas's face loader does it. This is the one
rule the Library cannot enforce and the one a loader must be written against.

### `Cache`: four verbs, and no fifth

`Get`, `Free`, `FreeAll`, `FreeWhere`. Every one of them either loads or frees.

**There is no fifth verb, and the question is closed rather than deferred.**
`Each(func(Descr[P], T) bool)` was proposed twice and answered twice by the same
observation: both plugins that wanted a whole-table number wanted a *number*, and
a number is a running counter. Scene's `TotalPoseBytes` and `TotalMorphBytes`
become two `int` fields maintained in `Load` and `Free`; canvas's
`MaxAtlasBytes` already **is** a running counter (`atlas.go:345, 165`), and both
of those sites move inside `Load` and `Free`. `Each` would hand out a read over
the cache's own table, a lifetime the Library never otherwise exposes. It is
closed on this map rather than left for `sound` to re-raise without a case.

**There is no non-loading probe, and that is a property with consequences.**
`Get` is the only read and it loads on a miss. Both remaining migrations hit it
from opposite directions — scene cannot ask *"is this resident?"* without
loading, canvas cannot ask *"what did this decode to?"* without loading — and in
both cases the answer was to delete the question rather than to add the verb.
[What the Library is not](#what-the-library-is-not) says why.

---

## Loading

**The Library reads; the loader decodes.** The Library opens `Name` through
`fsys` and hands the bytes to `Load` as a `Blob`. A descriptor **carrying a
`Blob` skips the read**, with or without a `Name`, because the bytes are already
in hand — which is what lets an asset inside a container be named by that
container and read from nobody.

**Everything finishes inside `Load`.** `T` is immutable and nothing is ever
pending in the cache. A plugin that wants asynchrony puts it **in front of**
`Get` and keeps its own not-yet-requested state; a `Free` landing during such a
load is that plugin's problem, not the cache's. No plugin in this migration keeps
any.

### Failure is terminal, and reported once

**The Library reports its own read failure**, once, as
`asset %T not found by path %q`, through `kernel.ReportErrorOnce`
([landed](https://github.com/dvoyni/cog/issues/186) as `b3810d9`).

**Why the Library and not the loader.** A loader-side report is *optional*, and a
cache that reports nothing is this map's headline defect: `translator.textures`
re-opens and fully re-decodes a missing file every frame, forever, reported
nowhere, and `atlas.go` has no `kernel` import at all. A loader-side report lets
gfx migrate onto the Library and still report nothing. A Library-side report
makes that impossible. Reporting is half of "gets failure right", and half a
guarantee is not one.

**The key is the `Descr` itself.**

```go
k.ReportErrorOnce(d, fmt.Errorf("asset %T not found by path %q", zero, d.Name))
k.ForgetReportedError(d)                                     // Cache.Free
k.ForgetReportedErrors(func(Descr[P]) bool { return true })  // Cache.FreeAll
```

No prefix and no string key. `ReportErrorOnce` takes any comparable and
`Descr[P]` is comparable — which is what `Blob` exists for — and the kernel's own
rule does the rest: *"distinct key types never collide, however their values
compare, so a plugin naming its own type owns its own namespace."* So
`assets.Descr[P]` owns a namespace per `P` and cannot collide with canvas's
`"sprite:"` strings or scene's `"model:"` ones. The Library gets for free what
those two need a prefix convention for. `FreeAll`'s family forget is a full
table scan, which is sanctioned: *"it belongs on a cold path — an unload, not a
frame."*

**The message names the asset by its type**, so there is no label on `New`,
nothing to configure and nothing to keep in sync. It replaces
`ErrShaderNotFound{Name}`, `ErrModelUnavailable`'s read branch, and canvas's
`fmt.Errorf("canvas: sprite %q could not be loaded", clean)`.

**Absent versus unreadable does not arise.** The loader never sees the error, so
there is nothing for it to distinguish, and nothing in the tree distinguishes
today: `os.IsNotExist` appears zero times, and the one production branch on
`fs.ErrNotExist` outside storage's own mount fall-through is `Values.load`. gfx
destroys the distinction at an `ok bool` boundary anyway. The Library wraps
storage's error untouched, so `errors.Is(err, fs.ErrNotExist)` keeps working for
anyone who later wants it.

**Failure is terminal.** The returned value is cached whatever it is, so `Load`
is not called again until a `Free`. **`Free` is the retry lever**, and it is also
the paired forget the kernel requires: *"without the paired call, a sprite that
failed to load once stays silent about failing again for the engine's life."*
That is why `Free` takes a kernel at all.

Report-once is therefore the **kernel's**, not the entry table's. The entry being
the once is a true observation about the mechanism rather than the mechanism, and
the difference matters: the kernel's map lives under `errorMu`, *"so that the
dedupe and the report it guards are taken under one lock"*, and these conditions
are noticed from at least two threads — gfx's cache is render-thread, canvas's is
update-thread.

Two report-once mechanisms survive the migration and neither is the cache's.
**Cross-entry dedup** — scene's `textureReportKey`, so *"two models naming one
broken image report it once between them"* — lives in the kernel keyed by string
and is independent of any cache. And a **dedupe that gates an error the holder
returns rather than reports** is carved out by the kernel's own doc: *"a
render-thread object with no Kernel — gfx's translator, an Adapter's backend —
hands its error back to whoever does hold one."*

### `Default` is a value, never an error

```go
Default(d Descr[P], userData U) T
```

**A method, not a field**, so it can ask a device for its 1×1 magenta at call
time rather than being built before the backend exists. **It takes the
descriptor**, so a placeholder can match the shape that was asked for rather than
being one fixed value — gfx's `TextureDescrParams` carries `width`, `height` and
`format`.

**It never sees the error.** Once the Library reports, the loader has nothing to
do with it; `Default` supplies the *value*, and what gets drawn is a separate
question from what gets said.

**Settled here: a default is only sometimes a picture.** This is the general rule
the three migrations produced between them, and it is stronger than any one of
them saw.

- Scene returns **magenta for an sRGB slot and the zero value for a linear one**,
  because magenta as a normal map is a surface lit from nowhere and as
  metallic-roughness it is a mirror. `srgb` is in the key and separates pictures
  from data exactly.
- gfx returns **id `0`**, which is the driver's white, not a substitution gfx
  performs. `err.go:77` states it as a rule, and after the Library reports,
  choosing id `0` no longer means choosing silence.
- canvas returns a **skip on every tier**, having been predicted to take magenta
  and declined: a sprite's on-screen size comes from the transform's explicit
  `Size` or from the entry's own pixel dimensions, so a magenta placeholder is
  visible exactly when the draw named a size and invisible when it trusted the
  file — an arbitrary split — and in ui the element was already laid out at 0×0 by
  the measurement tier.

The shape that transfers is **scene's rule, not scene's colour**: `Default` as a
null object rather than a visible stand-in, with the loudness coming from the
report rather than from the pixels.

---

## Release

**`Free`, `FreeAll` and `FreeWhere` free immediately. There is no queue.**

The map carried a justification for one and it was wrong. Both plugins that
defer justify their unload queue with a **dangling draw** — a sprite unloaded
during update whose draw was already recorded this tick — and **neither call
order supports that**: canvas drains at `plugin.go:175` and resolves sprites at
`:213-224`; scene drains at `:183` and expands models at `:201`. Both drain
*before* they resolve, so an asset drawn and unloaded in the same tick is retired
and then re-requested by the resolution that follows. It reloads; it never
dangles. Canvas's own comment — *"after any draw ops recorded this frame have
already been resolved"* — describes the opposite of the order its plugin uses.

**The accepted cost is stated rather than discovered: a free followed by a get is
a reload, not an error.** That is what both plugins already do.

**Releasing a value needs the loader**, because `T` is immutable and holds GPU
handles: dropping an entry without telling the loader leaks the texture, the
buffer or the atlas slot.

**`FreeAll` and `FreeWhere` collect, remove, then free**, so a loader that
re-enters its own cache cannot corrupt the walk. This is the one part of the
re-entrancy hazard that survives the queue's deletion, and it is not theoretical:
every drain in the tree today is `range` then `[:0]`, in five places, so anything
appended during a loop is unvisited and then silently truncated. Four of those
five disappear with the queues.

**`FreeAll` walks whatever is in the table at the call.** Scene's flag semantics
— consumed first regardless of call order, *"a flag rather than a walk because
the table it would walk can still change before the boundary arrives"* — are
replaced by gfx's ordered semantics, where a free-all sits in the same ordered op
list as single frees. An asset asked for after a `FreeAll` and before the frame
ends was deliberately asked for, and survives. `TestUnloadAllClearsEveryModel`
still holds; nothing pins the reversed case.

**`FreeWhere` exists because a release decision can read the value.** gfx's
`releaseCachedResource(path)` evicts *every shader whose sources contain that
path*, because a path may root several variants, may be an included source of a
module rooted elsewhere, and a `ShaderWithText` root can include resources. The
caller cannot name those keys. The alternative — a plugin-side reverse
`path -> []Descr` index, written inside `Load` and unwritten inside `Free` — was
**rejected**: it is a second table holding what `shader.sources` already holds,
and the value having the answer is the whole reason it needs no index. *"Evict
everything derived from this file"* turned out to be the general shape: canvas
takes it for one font file backing many sized faces, and scene for one path
backing many colour-space and image-index variants.

### What freeing costs, and whose cost that is

Freeing at the call needs the device at the call, which is a lock a plugin's
unload verb did not previously hold. **That cost is a game's, not the engine's.**
`UnloadSprite`, `UnloadFont`, `UnloadModel`, `UnloadTexture` and `UnloadAll` have
**zero non-test call sites** across `bundles`, `slots` and `extensions` — the
only hit is a doc comment in `scene/err.go`, and the only callers anywhere in the
sibling repos are in `cog-examples/cmd/scene/loading/loading_test.go`, whose
tests exist to demonstrate the levers themselves. They are game-facing API
exclusively, and unloading is a level-boundary act, so the serialisation lands on
a tick that is already stalling.

What that cost must **not** do is reach a handler that never unloads, and both
canvas and scene had to split a facade to keep it out. See
[`LookupDeviceAccess`](#the-convention-lookupdeviceaccess).

**This is what deletes code.** canvas's `unloadSprites` and `unloadFonts`,
scene's `unloadModels`, `unloadTextures` and `unloadEverything`, and both
`applyUnloads`. gfx is unchanged: its release already runs on the render thread,
so `Free` was always immediate there, and its two commands stay because they are
the *thread crossing*.

---

## What the Library is not

Recorded as properties because each is something a reader will look for and not
find, and in three cases something a migration wanted and did without.

- **It is not a probe.** There is no *is this loaded?* and no *what is this,
  without loading it*. `Get` is the only read.
- **It is not a walk.** See [Cache](#cache-four-verbs-and-no-fifth).
- **It does not refcount.** An entry is freed when it is freed. Canvas's atlas
  keeps a per-page `live` count, which counts occupied slots on a GPU page — one
  level *below* the key — and is therefore not a refcount on an entry.
- **It has no budget and evicts nothing by itself.** Canvas's `MaxAtlasBytes`
  stays canvas's, because it is a property of atlas packing rather than of
  caching.
- **It does not watch anything.** See [The loss, stated](#the-loss-stated).
- **It does not know what a frame is.** No `BeginFrame`, no tick, no boundary.

---

## The boundary a game cannot see

**The Library is plugin-internal.** The cache type appears in no plugin's public
API; each plugin keeps its own verbs — `UnloadModel`, `UnloadSprite`,
`ReleaseCachedResourceCmd`. The test of a successful migration is that **a game
cannot tell the plugins share a caching implementation.**

This is protected by the architecture rather than by discipline, which is worth
recording because it also killed a shape. `events.go` is a root file allowed to
Slots and Bundles only, and `libs/*` may import only `libs` and `kernel` — so
`libs/assets` could never declare an event, and there is no route by which a
plugin's cache could announce itself engine-wide.

**The name.** `libs/assets`. The collision with `assets.go`, which means *"the
embedded files this plugin ships"* in canvas and scene, is readability only;
those two files are renamed.

---

## gfx: two caches behind a thread boundary

From [gfx migration](https://github.com/dvoyni/cog/issues/446).

gfx owns two of the four caches and they behave oppositely. **Textures** are
keyed on the path alone and cache no failure — a missing file re-opened and fully
re-decoded every frame, forever, reported nowhere. **Shaders** are keyed on
`(root, supply)`, cache failure terminally and report once, with the reasoning
this whole map adopted. The migration is paid for by two type changes rather than
by new machinery, **the lock set does not grow by a single resource**, and the
Library grows one verb.

### The descriptors become `assets.Descr`

```go
type ShaderDescrParams struct{ supply, supplyMalformed string }
type ShaderDescr assets.Descr[ShaderDescrParams]

type TextureDescrParams struct {
	width, height int
	format        TextureFormat
	mipmaps       bool
	copyData      bool
	id            TextureID
}
type TextureDescr assets.Descr[TextureDescrParams]
```

`Name` is the resource path, `Blob` is the inline payload — shader text or pixel
bytes — and `Params` is everything else. **`shaderSource`, `textureSource` and
`TextureSource` are deleted**: the case is now which field is set, and the three
cases are disjoint, so the enums were restating them.

`ShaderWithText(text string)` keeps its signature and routes through
`NewBlobFromString`, not through `[]byte(s)` — see
[Blob](#blob-identity-is-the-run-of-bytes-not-their-contents) for the measurement
that makes this the difference between one cache entry and five. `flatten`'s
`rootSource` gets the text back through `Blob.String()`, also zero-copy.

The baked-texture case is untouched and stays public: `types.BakedTexture(id, w, h)`
keeps its signature and builds a `Descr` whose `Params` carries the id, so
`ensureTexture` splits — a baked descriptor yields `descr.Params.id` with no cache
involved, a path descriptor yields `textures.Get(...).id`.

### What each cache stores

```go
type texture struct {                 // value: nothing mutates it after load
	id            gfx.TextureID
	width, height int
	format        gfx.TextureFormat
}

type shader struct {                  // cachedShader, renamed
	id      gfx.ShaderID
	err     error
	sources []string
}
```

| | textures | shaders |
| --- | --- | --- |
| `P` | `TextureDescrParams` | `ShaderDescrParams` |
| `T` | `texture` (value) | `*shader` |
| `U` | `struct{ backend gfx.Backend; ops *gfx.Queue }` | `*translator` |

`U` names exactly what each loader touches. Textures mint an id and emit a bake.
Shaders need the whole translator, because `ensureShader` writes `t.layouts`
through `shaderLayout` and sets `t.diagnostic` from `checkWebLimits`, and because
`releaseShader`'s cascade sweeps `t.pipelines` and `t.parameterPlans` for the dead
`ShaderID`. **That cascade moves into `Loader.Free`**, which is its right home: it
is the release half of what `Load` built.

**Gap: whether `shader` stays a pointer.** It was made one because
`cachedShader.report()` set a `reported` flag on every hit and a value in a map
cannot be mutated in place. `Load` now takes a kernel and runs one time per key,
so the flag retires and with it the only thing that forced the pointer. It still
carries `sources`, which `FreeWhere`'s predicate reads, and a slice is a
pointer-friendly shape. **Settled at build time, not here**, and with it the one
question the flag's retirement leaves open: whether the translator still *returns*
the compile error on every cache hit — gfx's `firstErr` route through
`renderOnRender` — or whether reporting from inside `Load` replaces the return.
Either is consistent with this design; they must not both happen.

**The cache now keeps the decoded size.** `LoadTextureResource` returns
`(width, height, pixels, ok)` and `ensureTexture:662` writes
`types.BakedTexture(id, 0, 0)`, discarding both. Nothing reads the size today, so
it is carried because the loader has it in hand and dropping it is the defect,
not because a caller waits. This does **not** fix
`TextureWithResource(path).Size()` returning `(0,0)`: that is the caller's own
request value, and a value cannot learn. Closing that needs a query verb gfx does
not have, and this design does not add one.

### Eviction, the kernel, and the frame

`releaseCachedResource(path)` becomes **one keyed `Free` for the texture and one
`FreeWhere` for the shaders**, whose predicate body is
`slices.Contains(v.sources, path)`: today's scan, unmoved, on the other side of
the wall. `freeCachedResources` clears `t.pipelines` and `t.parameterPlans`
**before** the two `FreeAll`s, so the per-entry cascade scans empty maps instead
of running O(shaders × pipelines).

**A text shader with no `#include` has `sources == nil` and no path can evict
it.** It gains no release route. Noting the inline root under a sentinel name
would make it evictable, and that is a sentinel inside a path namespace — the
exact shape canvas is deleting when `whiteAtlasKey` stops being one. Inventing a
path for the one thing defined by not having one is the wrong direction.
`FreeAll` is its route, and `ReleaseCachedResourceCmd` gets a sentence saying what
it cannot reach. Context: `ShaderWithText` has **zero non-test callers** in the
tree.

**The kernel and the filesystem travel together, hoisted to once a frame.**
`Get` needs both where `ensureTexture` and `ensureShader` sit, five frames below
the handler that holds them. The kernel is free and already present —
`renderOnRender` receives one — and is threaded as a parameter, never stored. The
filesystem has a measured price, because handing `storage.FileSystem` out as an
interface boxes:

```
BenchmarkConcreteGet    1.063 ns/op     0 B/op   0 allocs/op
BenchmarkBoxedAsFsFS   14.05  ns/op    32 B/op   1 allocs/op
```

Today that 32 bytes is paid only on a **miss**, because both `ensure*` functions
probe the cache before calling `files()`. `Get` needs the `fs.FS` materialised
before the call, so it would be paid on every **hit** — once per texture parameter
and once per draw, tens of kilobytes a frame at a thousand draws. So `files()` is
hoisted to one call at the top of `translate`, and the three per-dispatch values
travel as one struct rather than growing six signatures by two parameters each:

```go
type frame struct {
	k       kernel.Kernel
	fsys    fs.FS
	backend gfx.Backend
}
```

**Confinement holds and costs nothing.** Both caches, both loaders and the
`Loader` values stay translator fields, reached only on the render thread inside
`ConsumeCmd`. `renderOnRender` already write-locks `readList`, `readyList` and
`*gfx.ResourceQueue` and read-locks `storage.FileSystem`. **Release stays a
command**: `ReleaseCachedResourceCmd` and `FreeCachedResourcesCmd` are the thread
crossing, not an API inconsistency.

**A stale doc line, not a missing key field.** `docs/README.md:399-402` says
*"Both constructors take a `gfx.TextureFormat` ... the same path in two formats is
two textures."* `TextureWithResource(path string)` does not take one — it
hardcodes `FormatRGBA8Srgb`, and its own comment explains why: *"the loader
decodes PNG and JPEG, both of which are gamma-encoded by definition, so there is
nothing for a caller to choose."* `Mipmaps()` is likewise always false on that
path. There is no second format a path can have, so `Format` does not enter the
key and the README line is fixed instead.

---

## scene: the state machine deletes

From [scene migration](https://github.com/dvoyni/cog/issues/447).

Scene's model table is the ~300 lines of state machine every other cache
approximates: `Missing → Loading → Resident → Failed`, per-entry generations,
terminal failure reported once, and a release queue drained at the frame
boundary. **All of it deletes**, because every one of those parts exists to
describe a load that is in flight, and after this there is none.

### Two caches

```go
type ModelDescrParams struct{}                          // the path is the whole key
type textureDescrParams struct{ image int; srgb bool }  // unexported

type modelUserData struct{ lookup *Lookup; resources *gfx.ResourceQueue }

// models:   assets.Cache[ModelDescrParams,   *modelUserData,     *residentModel]
// textures: assets.Cache[textureDescrParams, *gfx.ResourceQueue, textureEntry]
```

**`P` for models is empty.** `PoseSampleRate` rides `LoadModelRequest` today for
one stated reason — *"carried on the request because the parse holds no Lookup"* —
and the Lookup is now in hand inside `Load`, so it comes off `l.config` and never
enters the key. It is startup configuration, fixed for the Lookup's life; a key
carrying it would have exactly one value forever.

**`T` for models is `*residentModel`** — `ModelEntry` minus `State` and
`Generation` — and `Default` returns **nil**. A nil model expands to no
primitives, so the draw skips: *"skip, never substitute"* survives verbatim, with
`Default` as a null object. It is a pointer for a second reason too: `Get` is on
the per-draw path and the entry is nine fields with six slices among them, where
gfx's `texture` is four words and copies for free. Failure is a non-nil
unexported `err` on an otherwise-zero value.

**`U` re-enters the Lookup that owns the cache**, because the model loader needs
`claimMesh`, `layouts.resolve` and `pendingReleases`. `Load` is safe by
inspection — it only mints slots and appends — and `Free` is safe by the
Library's collect-remove-free discipline. Scene's loader does not free on its own
cache, since `UnloadModel` deliberately does not cascade to textures.

**The texture table reopens as a real cache.** The previous map ruled `l.textures`
a bake-dedup table that stays a plain field; that is reversed, and the release
side is why. `UnloadTexture(path)` frees every variant a path baked, which is
`FreeWhere(k, userData, func(d, _) bool { return d.Name == path })` exactly — so
`unloadTexture`'s hand-written scan and both report-key loops delete, along with
`l.textures`, `residentTextures` and `textureKey`. It also kills a stated defect:
*"two models sharing an external image path both decode it and only the first
uploads it"*, because the cache was consulted at install time and the decode
happened in the parse. A cache is consulted **before** the read, so the second
model decodes nothing.

`TestUnloadModelDoesNotFreeTextures` keys its texture `{path: modelPath, image: 0}`
— an **embedded** image freed by `UnloadTexture(modelPath)` — so the embedded case
is the one the pinned behaviour is about, which is why
[`Name` winning](#descr-you-named-it-you-own-the-name) had to be settled for scene
before this table could move.

### Where `Get` runs, and the one lock it costs

`expandModels` calls `Get` per model draw per frame, so **scene's flush gains
`Read[storage.FileSystem]` — the one lock added anywhere in this migration.**
Measured: `storage.FileSystem` is *write*-locked only by storage's own three mount
commands, and canvas's flush, ui's update and gfx's render already hold it as a
read, so scene's flush does not serialise against any of them on it.

The whole of `modelcmd.go` in both packages goes, along with `installModelCmd`,
`installModelRequest`, `installModelCmdImpl`, `loadModelCmdImpl`,
`types.LoadModelCmd` and `types.LoadModelRequest`.

**The honest cost.** Today the parse holds *only* the filesystem read, so a 20 MB
glTF decodes in parallel with the frame and only the upload blocks. After this,
the JSON parse, the image decodes, tangent generation and the vertex pack all run
inside the flush holding `Write[*gfx.ResourceQueue]` and `Write[*gfx.OpQueue]`,
and canvas's flush waits behind it. **`Preload` stays the documented lever and
the hitch is a stated property of the design, not an accident.** A game that skips
`Preload` takes it on first draw, which is what gfx and canvas already do.

Scene must copy gfx's `files()` hoist after all: `Get` is per model draw per
frame, so the 32-byte box is hoisted to once per `flushFrame`. (An earlier note on
scene's ticket said not to, reasoning from a per-load-command conversion. That
reasoning died with the command.)

### The facade splits, so no ECS System pays for loading

Eleven `LookupAccess` methods trigger a load. Under this design each needs an
`fs.FS` and the resource queue at the call, and the blunt route —
`NewLookupAccess(k, lookup, fsys, resources)` — reaches
`cog-examples/cmd/ecs/fountain/systems.go:25`, an **ECS System** whose entire use
of the facade is two `BakeMesh` calls. It would have had to declare
`*ecs.Write[*gfx.ResourceQueue]` to bake a cube, serialising it against canvas's
flush, scene's flush and gfx. **Widening an ECS System's lock set is rejected
outright in this repo, not traded off.**

- **`LookupAccess(k, lookup)`** keeps `BakeMesh`, `UpdateMesh`, `ReleaseMesh`, the
  unload verbs and everything that does not load. Fountain is untouched.
- **`LookupDeviceAccess(k, lookup, fsys, resources)`** carries `Preload`, `State`,
  `Nodes`, `Bounds`, `AABB`, `ModelLights`, `Joints`, `Clips`, `MorphTargets`,
  `PoseBytes` and `MorphBytes`.

The five production callers are all in cog-examples; four already hold
`Write[*scene.Lookup]` beside `Write[*canvas.OpQueue]`, so the added queue write
costs them no new serialisation, and the cost becomes visible where it should be —
in each demo's `Lock` closure.

### `ModelState` deletes, and so do generations

`ModelLoading` can never be observed once the load runs inside `Get`, and
`ModelMissing` already cannot be: `State`'s own doc says *"the very act of asking
moves it to ModelLoading"*. A two-valued enum is a bool wearing a costume.

**`State(path string) error`** — nil for resident, the load's own failure
otherwise. It is the same `err` the zero `*residentModel` carries, so it is the
value the kernel was already told about, and a HUD prints the reason rather than a
state word. `ModelState`, `ModelMissing`, `ModelLoading`, `ModelResident` and
`ModelFailed` all delete.

The concept of residency survives; it stops being a *state*. Atomicity is
unchanged — `Load` still uploads geometry, materials, textures and animation
buffers before it returns, so there is still no frame in which half a model draws.

**An invalid path never enters the cache.** `ModelKey` validates before `Get`, so
`../escape.glb` is `ErrModelPathInvalid` computed on the spot, with no entry and
no tombstone. `TestUnloadClearsAnInvalidPath` changes meaning rather than
breaking: the second `State` still returns the failure and the report count is
still two, because `UnloadModel` still clears the report keys — but a typo is now
permanently a typo, which is the truer answer.

**Generations delete.** The counter exists for one sequence: an unload landing
while a load is in flight, so the completing install must not write into a slot
nobody asked for. There is no in-flight load. It is read in exactly three places,
all of which go, and `TestAnUnloadWhileLoadingDiscardsTheCompletingLoad` goes with
them; the *"reset rather than delete"* tombstone rule goes too, because there is no
longer anything for a tombstone to defeat. `MeshRef.generation` is a different
mechanism and is untouched, as are `pendingReleases`/`drainMeshes` — those guard
`BakeMesh` and `ReleaseMesh` handles, not cache entries.

**Two counters, not a fifth verb.** `TotalPoseBytes` and `TotalMorphBytes` each
walk `l.models` today. Two `int` fields on the Lookup, added in `Load` and
subtracted in `Free`, make both O(1) and stop them needing the `ModelResident`
test that just deleted.

---

## canvas: five caches and a packer

From [canvas migration](https://github.com/dvoyni/cog/issues/448).

Canvas has more asset tables than any other plugin and the weakest failure
policy: `Atlas.BeginFrame` clears the failed set **every frame**, *"so a file that
has since appeared is tried again"*, and because the decode runs before the pack,
a sprite that fails re-opens and fully re-decodes every frame, reported nowhere.

Four tables become five caches, and the fifth thing turns out never to have been a
table at all.

```go
type spriteDescrParams struct{ generated bool }   // the white texel's case
type fontDescrParams struct{ px int }

type spriteUserData struct{ packer *packer; resources *gfx.ResourceQueue }

// sprites:  assets.Cache[spriteDescrParams, *spriteUserData,    AtlasEntry]
// tiled:    assets.Cache[struct{},          *gfx.ResourceQueue, StandaloneEntry]
// sizes:    assets.Cache[struct{},          struct{},           m.Vec2i]
// sources:  assets.Cache[struct{},          struct{},           *opentype.Font]
// faces:    assets.Cache[fontDescrParams,   *fontUserData,      *Font]
```

`AtlasEntry` and `StandaloneEntry` are held **by value**: nothing mutates either
after load and `freeEntry` only reads. `*Font` and `*opentype.Font` are pointers,
the first because `Glyphs` is a mutable per-face memo. None of this reaches a
caller — `AtlasEntry` lives in `internal/types` and is not public API.

### ui's lock set does not move, and the sizes tier is why

The trap scene warned canvas about — `Get` needs the resource queue, and canvas's
facade deliberately holds none — springs the other way, because **canvas's flush
does not use the facade**: `flushFrame` reaches the atlases through friend
functions and calls `ResolveSprite` directly. **ui's handler does not change a
character**: it keeps its eight resources and its three-dependency
`canvas.NewLookupAccess(k, lookup, filesystem)`.

**The measurement tier's `U` is `struct{}`** — no GPU anywhere in it — and that is
not an accident of what its loader happened to need. It is the property that keeps
ui's lock set off the device, and it belongs in this document as a requirement.
Canvas's third table, which scene had to invent a facade split to imitate, is what
pays for it.

**The casualty.** `SpriteSize` prefers a resident atlas entry over the header
table today — *"a resident atlas entry carries authoritative decoded dimensions;
prefer it so layout tracks the pixels actually drawn even if the file changed"* —
and that read is a **non-loading probe of the sprite cache**, which the Library
does not have. So the preference deletes and `SpriteSize` answers from the header
cache alone, always. If a file changes on disk between the header read and the
full decode, layout tracks the header. This is the same one-probe rule that killed
scene's state table, arriving from the opposite direction.

### The convention: `LookupDeviceAccess`

`Free` is immediate and needs the queue, so `UnloadSprite` and `UnloadFont` cannot
stay on the facade ui builds.

```go
canvas.NewLookupAccess(k, lookup, fsys)             // SpriteSize, FontMetrics, Measure*
canvas.NewLookupDeviceAccess(k, lookup, resources)  // UnloadSprite, UnloadFont, UnloadAll

scene.NewLookupAccess(k, lookup)                              // bake, unload, no loading
scene.NewLookupDeviceAccess(k, lookup, fsys, resources)       // Preload, State, queries
```

**Settled here as a convention rather than twice as a local choice.** The two
plugins split *opposite halves* — scene split loading off, canvas split unloading
off — and naming each for its verbs (`LookupLoadAccess`, `LookupUnloadAccess`) was
rejected for exactly that reason: it reads as two unrelated facades when it is one
constraint appearing twice. **The constraint is the device**, so both are named for
what they carry, and a consumer reading two plugins sees the same word.

### The packer stays, the glyph table was never real

`Atlas` today is a packer (arrays, shelves, free list, `bytes`) plus three tables
(`entries`, `standalone`, `failed`). The packer is persistent state that must
outlive a handler, so it stays on the `Lookup` and travels in `U`; the tables
become caches; `failed` and `BeginFrame` delete.

**`Atlas.entries` on the fonts atlas is write-only.** `LoadGlyph` writes it under
`"\x01%s\x00%d\x00%d"` and nothing ever reads a glyph key: `ResolveSprite` reads
the *sprites* atlas, `releasePath` is only ever called with a sprite path, and
`insert`'s one read is the white-texel guard, which glyph inserts skip. It is an
`AtlasEntry` retained per rune per size for the life of the glyph atlas, indexing
nothing; `face.Glyphs` is the real index and always was. So the glyph atlas
becomes **a packer with no table**, and `AtlasEntry.category` and the whole
`atlasCategory` type go with it — their single reader is `insert`'s reservation
guard, which moves.

**The two packer refusals, and why neither gets a mechanism.** `place` walks every
layer of every array before it considers allocating, so layers and arrays absorb
almost everything. There are exactly two refusals: **bigger than a page** (`insert`
pads by 2 each side, so the largest sprite that can ever pack is `AtlasSize - 4`,
**4092** at defaults) and **the budget wall** (one array is 4096×4096×4×2 = 128 MiB
against a 256 MiB budget, so two arrays allocate and the third is refused — **four
4096-texel pages, 67 megapixels of sprite**, and each atlas carries its own budget).
`resolveConfig` already refuses a budget that cannot hold one whole array, so
neither condition is reachable by configuration alone.

**Both are terminal, both report through `Load`'s kernel, and both return the zero
value.** They are development-stage errors: a 5000px sprite and 67 megapixels of
resident art both surface the first time a scene is assembled, not in front of a
player. The failure value is the same zero `AtlasEntry` a missing file produces, so
the sprite tier has exactly one failure value.

`BeginFrame` deletes **with nothing in its place**. The retry mechanism built for
it is in [Shapes that were rejected](#shapes-that-were-rejected), and
[`UnloadAll`](#canvasunloadall-the-one-verb-this-design-adds) is what answers the
sequence it was built for.

### The white texel, and why its reservation moves

It is **blob-named**: `Name == ""`, `Blob = assets.NewBlobFromString("\xff\xff\xff\xff")`,
`Params{generated: true}`. `Name` is not set, because there is no file and
nothing to name one after. `whiteAtlasKey` and its sentinel namespace inside a
string key space delete.

The reservation prologue — `insert` recursing into itself — moves into
`flushFrame`, which `Get`s the white descriptor once, immediately after
`ensureQuad` and before any layer's ops. **It matters more than housekeeping.**
`batchEntry` keys the batch on `entry.Texture`, so a white texel that landed in
array 2 would split **every fill away from every sprite it draws with**. Reserving
it first is what keeps fills and sprites in one instanced draw — which is why the
reservation exists at all; the comment at `atlas.go:251` says *what* and not *why*.
A frame whose white texel does not pack draws nothing, which is what the recursion
already did, silently.

### The font store is two caches, and the resize decides it

Sources keyed `{Name: path}` to `*opentype.Font`; faces keyed
`{Name: path, Params: {px}}` to `*Font`. The face loader re-enters the source cache
inside its own `Load` — a different cache, which is allowed.

The release side is why this shape. `unloadFont(path)` is
`faces.FreeWhere(k, userData, func(d, _) bool { return d.Name == path })` plus
`sources.Free(k, {Name: path}, userData)`, which is `FreeWhere`'s second use.
`invalidateFontsOnResize` is `faces.FreeAll()` with the sources kept — `ClearFontFaces`
verbatim, *"closes baked faces but keeps parsed sources for re-baking"* — plus the
glyph packer's `releaseAll`. **One cache keyed by path with the faces inside `T`
cannot express the resize at all**, and one cache keyed by `(path, px)` re-parses
the file once per size.

**A leak this does not fix, and which stops being silent.** `unloadFont` drops
faces without reclaiming their glyph atlas slots, and with the glyph table gone
those slots stay unreclaimable until a framebuffer scale change. It is unchanged
behaviour, not a regression, and it belongs in this document as a stated property
rather than inherited as a comment.

### Validation converges, and `"."` stops being white

`validateResourcePath` is byte-identical to scene's and goes with it. The draw
path's `NormalizeResourcePath` is weaker — it accepts absolute paths and `..`, and
maps `"."` to `""`, which today resolves to the white texel. They converge on the
strict rule, applied where the path enters, at record time where
`NormalizeResourcePath` already runs. An invalid path is reported once by canvas
and **never reaches the cache**, which is scene's rule. `""` keeps meaning the
white texel, now by resolving to the generated descriptor rather than to a
sentinel string; `"."` becomes an invalid path rather than a silent white quad.
Nothing pins the old behaviour.

---

## `canvas.UnloadAll`, the one verb this design adds

From [terminal failure has no retry lever](https://github.com/dvoyni/cog/issues/449).

```go
func (la LookupDeviceAccess) UnloadAll()
```

gfx has `FreeCachedResourcesCmd{}` and scene has `UnloadAll()`. Canvas has only
`UnloadSprite` and `UnloadFont`, so a game giving up a level must name every
sprite and font path it ever drew, and a path it forgets stays resident for the
process's life with nothing that reports it.

**It is memory at a level boundary, not a developer loop** — which is what gfx's
and scene's own docs say their coarse levers are for. Post-migration it is five
`FreeAll` calls on the facade that already holds the other two verbs, and it
**spares nothing**: canvas has no `BakeMesh` equivalent handing out caller-owned
refs, which is the whole reason scene's `UnloadAll` spares what it does, and the
white texel re-reserves at the top of the next frame by construction.

**It also answers the one failing sequence the migrations produced.** Canvas's
budget wall is contingent on what else is resident, so this design introduces a
sequence today's code survives:

1. Level 1 fills all four pages.
2. Level 2's `spriteA.png` finds no room; the refusal is terminal and caches.
3. Level 1's sprites unload. Three pages of slots return to the free list.
4. `spriteA.png` is asked for again, hits the cached failure, and never packs.

Step 3 **is** a `Free`, so the lever is being pulled — just not on the entry that
needs it, and the game would have to call `UnloadSprite("spriteA.png")` on a
sprite it has every reason to believe was never loaded. A game streaming levels
calls `UnloadAll()` at the boundary instead, which frees the cached failure along
with everything else, so level 2 packs into an empty atlas. **The remedy lands on
exactly the event the sequence happens at**, and it is the same shape in all three
plugins, because a failed entry evicts like any other.

**What is genuinely given up**, stated rather than left to be discovered: a game
that unloads per-path rather than wholesale keeps the cached failure. That is the
residual, it is narrow, and it is visible the moment a developer looks.

---

## The costs this design accepts

Each of these is a real regression against today, taken deliberately, and each is
here so it arrives as a sentence in a spec rather than as a surprise in a
migration commit.

- **Scene's load hitch.** The parse, decode, tangent generation and vertex pack
  move inside the flush. `Preload` is the lever and it is documented as such.
- **`SpriteSize` tracks the header, not the pixels.** The atlas-first preference
  has no non-loading probe to run on.
- **One missing file, up to three reports.** The Library keys the read failure by
  `assets.Descr[P]`, and a path that is measured, drawn and drawn tiled is three
  caches with three distinct descriptor types, where `"sprite:" + path` is one key
  today. Accepted with the reason written down: they are three different failures
  at three different times — layout, draw, tiled draw — each still once per
  episode. The only way to collapse them is a plugin-side *have I reported this
  path* map, which is precisely the `lookup.reported` map `b3810d9` deleted.
  `TestLookupReportsMissingAndInvalidPathsOncePerEpisode` survives untouched
  because it drives `SpriteSize` alone; the change is invisible to the suite and
  visible to a game.
- **canvas's font glyph slots stay unreclaimable** until a framebuffer scale
  change. Unchanged behaviour, newly stated.
- **`ShaderWithText` with no `#include` can only be freed by `FreeAll`.**
- **A per-path unload leaves a cached failure cached.** See
  [`UnloadAll`](#canvasunloadall-the-one-verb-this-design-adds).

### The loss, stated

**A failed load is terminal until a `Free`, nothing in the engine issues one, and
that is sound — because no file an asset is loaded from changes while the engine
runs.**

The map was charted believing a real developer loop was being given up, and the
fact that dissolves it is not about caching at all. storage's read mounts are
fixed at composition — an embedded FS, a preloaded bundle, or `os.DirFS` on a
desktop, contributed by a plugin through `storage.ReadMountPort` and never added
to afterwards — and
`storage.FileSystem` exposes exactly one method, `Open`: no `Stat`, no mtime, no
subscription surface. The only mutable mount is `PermanentMount`, and the only
thing the engine writes through it is the values file; every other writer in the
tree — canvas's, ui's, gfx's and mcp's providers — calls `os.WriteFile` on a *host*
path and bypasses storage entirely.

So the asset filesystem is **static by construction**. There is no change to
notify about. A game that overwrites a file it also loads is the one caller that
already knows it did, and already has the verb.

**What actually goes away is narrower than it looked**: a desktop dev build
mounting `os.DirFS` over the asset directory stops picking up edits mid-run — and
it only ever picked them up because gfx's texture cache re-opened and fully
re-decoded every missing file every frame, reported nowhere. The loop was a side
effect of the defect this design removes, not a feature anyone designed.

**And the trade already shipped once, with a word standing in for a mechanism.**
This is the honest measure of what the loop was worth, and it is a correction to
`main` rather than a migration cost. Three places in gfx describe a developer loop
that does not exist and never did:

- `slots/gfx/docs/specs/preprocessor.md:934` — *"The developer loop is unchanged:
  fix the file, hot-reload evicts, the next frame retries and reports afresh."*
- `slots/gfx/internal/translator.go:710-712` — the same sentence, as the comment
  justifying why the include set is recorded on failure.
- `slots/gfx/internal/plugin_test.go:1519` and `:1563` — the test that pins it,
  which stands in for the hot-reload by calling `ReleaseCachedResourceCmd` **by
  hand**.

gfx's shader cache made exactly this trade a release ago, was praised in this map
for being the one cache that gets failure right, and papered over the consequence
with a word. Nobody noticed, this map included, while charting the ticket about
it. **The four sentences are deleted, not softened.** The include set is still
recorded on failure, for the real reason: a failed entry must evict like any
other, so a coarse release does not leave failures behind it.

---

## How this is tested

**Two levels, and no new seams.** The migrations are asserted through the seam
each plugin already uses, and the only new tests are the Library's own.

**`libs/assets` — its own package tests**, against a fake `Loader` that counts
`Load`, `Default` and `Free` calls, and an `fstest.MapFS`. This is the highest
seam for everything that is the Library's:

- The key rules: `Name` wins over `Blob`; a blob-named literal caches once per
  identity; `Blob{}` is one empty; two `Descr` differing only in `Params` are two
  entries.
- Terminal failure: a missing file loads once, reports once, and does not load or
  report again until a `Free`; `Free` then `Get` reloads and reports afresh.
- `Default` is called on the failed path and its value is what `Get` returns.
- `Free`, `FreeAll` and `FreeWhere` call `Loader.Free` exactly once per entry,
  and do it **after** the entry leaves the table — asserted by a loader that
  re-enters its own cache inside `Free` and must not corrupt the walk.
- `FreeAll` walks what is in the table at the call: an entry added after it, in
  the same tick, survives.
- The measured claims that carry design weight are benchmarks, not prose:
  `NewBlob` and `NewBlobFromString` allocate nothing, and the five-call identity
  counts above are a test rather than a comment. Per house rule, **no `-race` in
  this environment**; concurrency-adjacent assertions run under `-count=10` and
  say so.

**Each plugin — the engine-level seam it already has.** gfx's
`slots/gfx/internal/plugin_test.go` drives a real engine with `fakeBackend` and
`fstest.MapFS` and already counts opens, cache entries and uploads; canvas's
`canvas_test.go`, `target_test.go` and `font_test.go` do the equivalent; scene's
plugin and `internal/types` tests likewise. Every migration assertion goes there,
because that is where the behaviour a game sees is visible.

**A good test here asserts what a game can observe** — how many times a file was
opened, whether a draw appeared, what the kernel was told — and not which table
holds it. That is the property that lets this whole migration land: the tables
change identity completely, and a test written against the old ones would have to
be rewritten rather than run.

**Two named tests change meaning rather than breaking, and both must say so in
their names.**

- `TestFailedTextureResourceLoadIsRetried` (`plugin_test.go:1489`) asserts
  `opens == 2` across two frames with no eviction between them: it pins the
  every-frame re-read as a feature. It is **rewritten and renamed** as the texture
  twin of `TestFailedShaderIsCachedAsFailedAndEvictedByItsPath` (`:1520`), same
  three phases — first frame opens once and reports once; second frame opens zero
  more and reports zero more; then `ReleaseCachedResourceCmd{Path}` and the third
  frame opens again, uploads, and reports afresh. **The rename carries the change
  of meaning**, so a reader of the diff cannot miss that a behaviour was
  deliberately given up.
- `TestUnloadClearsAnInvalidPath` keeps passing with a different meaning: a typo
  is now permanently a typo.

**And these are deleted with what they pin:**
`TestAnUnloadWhileLoadingDiscardsTheCompletingLoad` (there is no in-flight load),
and every test that names `ModelState`, a generation, or a queue drain.

---

## Shapes that were rejected

Recorded so they are not reinvented, each with the reason that actually killed it
rather than the first objection raised.

**`Blob` as a box — `struct{ data *[]byte }`.** It was the map's own settled
shape, and it loses on where the type is used rather than on elegance: `Blob` is
also `BufferDescr.bytes`, `TextureDescr.pixels` and `ParameterDescr.raw`, built
per frame at five sites where construction is allocation-free today. A box makes
every one a heap allocation, because `&b` on the parameter escapes.

**`type Blob *[]byte`.** Compiles as a map key and **cannot have methods at
all** — *"invalid receiver type Blob (pointer or interface type)"* — so `Data()`,
`Len()` and any future `Id()` are impossible.

**Interning Blobs by content hash.** It would make the inline form correct —
`NewBlob([]byte{...})` twice would be one key — and it makes `NewBlob` O(n) on a
path whose whole point is that it is free, and needs a table that either holds
every Blob for the process's life or becomes a weak map with its own lifetime
question. It buys nothing a `var` does not, because a Blob that is a key is built
once either way.

**Bytes or a path, never both.** The original key rule. Dissolved by scene: a
GLB-embedded image has no path of its own and its bytes are in hand, and under
this rule `Name`-only re-reads and re-parses the whole container once per embedded
image while `Blob`-only leaks entries on every reload.

**A `report func(error)` closure on the cache.** Scene's existing idiom, minted
once and threaded through fifteen sites — and a closure in a design that has just
replaced closures with an interface.

**A fourth `Loader` method for reporting.** Moot once the Library reports.

**`Load` called with a zero `Blob` on a failed read**, letting the loader decide.
Mechanically sound, and it throws the error away and makes reporting optional,
which is the defect the Library exists to make impossible.

**`Default` gaining the error and becoming `Failed(err, params, userData)`.**
Recommended and rejected: once the Library reports, the loader has nothing to do
with the error.

**A label on `New` to give the report plugin vocabulary.** `%T` already names the
asset, and a label is a second name to keep in sync with the first.

**The kernel arriving through `U`.** The obvious alternative and the one thing
that cannot work: `U` is opaque to the cache by construction, so a cache holding a
`U` still has nothing it can call.

**A free queue and `ApplyFrees`.** Carried on the map with the wrong
justification, which the tree contradicts: both plugins cite a dangling draw and
**both drain before they resolve**, so the hazard their queues defend against is
not one their call order can produce. What was actually load-bearing is a lock
set, and that is answered by splitting a facade rather than by deferring.

**Keeping the queue for canvas and scene while gfx frees immediately.** It buys
nothing once freeing at the call produces a reload rather than an error, and it
leaves the five hand-rolled fields in place, which is most of what this design is
for.

**A `Get` cancelling a queued `Free`.** Dissolved with the queue. It was never
built, nothing in the tree does it, and the cache could not implement the narrowed
form — *never on a failed entry* — in any case, because `T` is opaque and it
cannot tell a failed entry from a good one.

**`Each(func(Descr[P], T) bool)`, a fifth verb.** Twice proposed, twice answered
by a running counter. It hands out a read over the cache's own table, a lifetime
the Library never otherwise exposes.

**A plugin-side reverse `path -> []Descr` index for shader eviction.** A second
table holding what `shader.sources` already holds. `FreeWhere` reads the value
instead.

**Noting a text shader's inline root under a sentinel name so a path can evict
it.** A sentinel inside a path namespace — the exact shape canvas is deleting when
`whiteAtlasKey` goes.

**Scene keeping a command hop** that held filesystem, Lookup and queue and called
`Get` inside it. It fails on one fact: the cache has no non-loading probe, so
`expandModels` — which must ask *is this resident?* without loading — would have
kept a state table in front of the cache. Two tables for one asset is the shape
this design exists to delete.

**`ExecuteCommandAsync` for scene's loading verbs.** The kernel's own answer for
*"work whose locks you do not want to widen the handler with"*, and it is why
`ModelLoading` exists today. Refused because it puts the asynchrony back and with
it the state machine, to buy a lock that four of the five callers already hold.

**Naming the two facades for their verbs** — `LookupLoadAccess` and
`LookupUnloadAccess`. It reads as two unrelated facades when it is one constraint
appearing twice, and the constraint is the device.

**Passing the resource queue as an argument** — `la.UnloadSprite(path, resources)`
— or hanging a nil-able queue on the existing facade. The first puts a resource
handle in a public signature no other verb in either plugin takes; the second
invents a runtime failure for a compile-time question.

**Putting the material slot in scene's texture key** so a default could be
slot-aware. It turns one ORM texture — occlusion, roughness and metalness packed
into one image, the standard glTF packing — into three GPU textures. Slot-aware
substitution stays the material binding's business.

**Canvas's failed-for-space retry.** Built and then discarded. The mechanism that
fitted was an unexported *failed-for-space* mark on `T`, a dirty flag the packer
sets whenever `freeEntry` returns slots, and a `FreeWhere` over that mark at the
exact line `BeginFrame` occupies — strictly less clearing than today, at the same
call site, firing on an event rather than on a clock. Refused on the ground that
**a game that fills four 4096-texel pages and then streams across that boundary is
over its configured budget and will learn so in development**, and shipping a
mechanism to make a misconfiguration recoverable at runtime is paying for the
wrong thing. `UnloadAll` is what the sequence actually wanted.

**The Library re-probing its own read failures.** The Library performs the `Open`
itself, so it can tell *its* failures (file absent) from the loader's (bytes
present, decode failed), and could cache the loader's terminally while re-probing
its own — one failed `Open` per absent asset per frame, no decode, no second
report. Materially different from the re-decode already rejected, and refused
anyway: in a shipped game a missing asset is a permanent bug, so it is a syscall
per missing asset per frame forever; it does nothing for the corrupt-file case;
and it makes *failure is terminal* conditional on the failure's kind, which is a
second rule where this design has one. Given the static filesystem it also buys
nothing, because the file that was absent stays absent.

**`storage.FileChangedEvent{Path string}`, with each plugin subscribing.**
Proposed as the one engine-wide act, to spare a caller having to know which plugin
owns `hero.png`. Dead on the static filesystem: read mounts are fixed at
composition, so there is nothing to publish. Worth recording that `libs/assets`
could not have declared it in any case — `events.go` is a root file allowed to
Slots and Bundles only — which is also what would have protected [the boundary a
game cannot see](#the-boundary-a-game-cannot-see).

**An `assets_reload` mcp capability.** cog already has an agent-facing control
plane, opt-in by composition (`ProviderPort` is a `CollectedPort`, zero included),
with gfx already contributing `gfx_capture` and `gfx_frame`. It was the cheapest
of the three homes — a game that does not compose the broker compiles in neither
the SDK nor the capability — and it goes with the event: a reload capability with
nothing to reload from.

**A build tag on the `-tags ecs_validate` model.** The repo's one precedent for a
development-only capability compiled out of release, where the flag is a
`const false` so the binary carries no branch, no table and no load. It does not
transfer: that tag exists because validation costs the ECS **hot path** something
no composition choice can remove. Nothing here costs a hot path anything, and
composition was always the gate.

**A central fan-out — one plugin that knows all five unload verbs.** Legal
(`X/internal/…` may import any root) and wrong: it must know which plugin owns
which path, it must be edited every time a plugin is added, and composing it drags
scene into a 2D game's binary to reload a sprite.

---

## Required work

The checklist to build from, in dependency order. **Nothing here is built.**

**`libs/assets` — the Library**

- `Blob`: the two-field struct, `NewBlob`, `NewBlobFromString`, `Data`, `String`,
  `Len`, `Blob{}` as the canonical empty, and the doc comments carrying the
  *built once and kept* rule and the read-only-write hazard.
- `Descr[P comparable]` with exported fields, and the `Name`-wins key rule.
- `Loader[P, U, T]` — `Load(k, data, params, fsys, userData)`,
  `Default(d, userData)`, `Free(value, userData)` — with the *must not free on
  its own cache* rule on the interface.
- `Cache[P, U, T]`, `New`, and `Get`, `Free`, `FreeAll`, `FreeWhere`, the last
  three collecting and removing before they free.
- The read, the `ReportErrorOnce` under the `Descr` key, and the paired
  `ForgetReportedError` / `ForgetReportedErrors`.
- `libs/assets/docs/README.md`, the package's API per the repo's layout rule.
- `README.md`'s package list gains `assets`. It is the second Library and the
  first with a `docs/specs/`.

**`libs/m` and `bundles/ecs` — the type moves**

- Delete `libs/m/blob.go`.
- `bundles/ecs/internal/types/component.go`: `blobType` becomes
  `reflect.TypeFor[assets.Blob]()`, importing `libs/assets`.
- Correct the *hands out nothing a reader can use to change the Store* sentence,
  and replace it with the unexported-field argument. Amend the *conversion is
  free* paragraph: still free, no longer implicit.
- `CONTEXT.md`: **Blob** gains that identity is the run of bytes rather than
  their contents; **Residency** stops being a four-valued state; **Frame
  boundary** stops listing residency and unloads among what it defers. Do these
  with the code, not before it.

**`slots/gfx`**

- `ShaderDescr` and `TextureDescr` become `assets.Descr` instantiations;
  `shaderSource`, `textureSource`, `TextureSource` and `gfx.TextureSourceBaked`
  delete; the `Params` types keep unexported fields and gfx keeps its delegates.
- `texture` and `shader` (renamed from `cachedShader`), the two loaders, the two
  `Cache` fields on the translator.
- `*frame` threaded through `translate`, `translatePasses`, `translateDraw`,
  `emitResources`, `ensureTexture`, `ensureShader`; `files()` hoisted to once per
  `translate`, and `readFiles`'s comment amended to say so.
- `releaseCachedResource` becomes one `Free` plus one `FreeWhere`;
  `releaseShader`'s cascade becomes the shader loader's `Free`;
  `freeCachedResources` clears pipelines and plans first.
- `ErrShaderNotFound` deletes; `material.go:108-114` hashes the Blob's two fields
  and drops its `unsafe.SliceData` call.
- Decide the `shader` value-or-pointer question and the compile-error
  report-or-return question together — see the **Gap** above.
- Delete the hot-reload claims at `preprocessor.md:934`, `translator.go:710-712`,
  `plugin_test.go:1519` and `:1563`, replacing the translator comment with the
  evicts-like-any-other reason. Fix `docs/README.md:399-402`.
- Rewrite and rename `TestFailedTextureResourceLoadIsRetried`.

**`bundles/scene`**

- The two caches, the two loaders, `residentModel`, `State(path) error`.
- Delete `ModelState` and its four constants, `ModelEntry.State`,
  `ModelEntry.Generation`, `requestModel`, `installModel`, `unloadModel`,
  `unloadTexture`, `applyUnloads`, `LookupApplyUnloads`, `unloadModels`,
  `unloadTextures`, `unloadEverything`, `l.textures`, `textureKey`,
  `residentTextures`, `installModelCmd`, `installModelRequest`,
  `installModelCmdImpl`, `loadModelCmdImpl`, `types.LoadModelCmd`,
  `types.LoadModelRequest`, and both `modelcmd.go` files.
- Split the facade into `LookupAccess` and `LookupDeviceAccess`; the flush gains
  `Read[storage.FileSystem]` and hoists the `fs.FS` conversion.
- `TotalPoseBytes` and `TotalMorphBytes` become running counters.
- `UnloadModel` → `Free`, `UnloadTexture` → `FreeWhere`, `UnloadAll` → two
  `FreeAll`s, `Preload` → `Get` with the result discarded.
- Magenta for sRGB, zero for linear, in `Default`; `bindModelMaterial` keeps its
  per-slot linear defaults unchanged.
- The spec at `docs/specs/scene.md` states the load hitch as a property, and
  `Preload` as the lever.

**`bundles/canvas`**

- The five caches, the two loaders' users, the packer extracted from `Atlas`.
- Delete `Atlas` as a combined packer and table, `atlasCategory`,
  `AtlasEntry.category`, `whiteAtlasKey`, `Atlas.entries`, `Atlas.standalone`,
  `Atlas.failed`, `Atlas.BeginFrame`, `ResolveSprite`, `ResolveStandalone`,
  `releasePath`, `Lookup.unloadSprites`, `Lookup.unloadFonts`,
  `Lookup.applyUnloads`, `LookupApplyUnloads`, `Lookup.spriteSizes`, `FontStore`,
  `fontKey`, `unloadFont`, `ClearFontFaces`, `validateResourcePath`, the sprite
  load error and its report key, the glyph atlas's key format string, and
  `SpriteSize`'s atlas-first branch.
- Split the facade into `NewLookupAccess(k, lookup, fsys)` and
  `NewLookupDeviceAccess(k, lookup, resources)`, both public in
  `bundles/canvas/utils.go`. **ui's handler must not change.**
- The white texel becomes blob-named and its reservation moves to `flushFrame`,
  after `ensureQuad` and before any layer's ops.
- Validation converges on the strict rule at record time.
- **Add `UnloadAll()`** — five `FreeAll` calls, sparing nothing.
- The spec states the unreclaimable glyph slots and the three-report cost.

**Both canvas and scene**

- Rename the `assets.go` that means *"the embedded files this plugin ships"*.

**Tests** — per [How this is tested](#how-this-is-tested).

---

## Out of scope

Ruled beyond this design, with the reasoning verified against the tree. None of
it graduates; redrawing any of it is a fresh effort.

- **Temporary and pooled per-frame assets.** gfx's temporary textures are keyed
  by *shape*, acquired from a free list rebuilt every `Reset()`, never shrunk and
  never released. No key, no load, no failure, no release: a frame allocator
  wearing a cache's clothes.
- **A cache budget or automatic eviction.** Canvas's `MaxAtlasBytes` stays
  canvas's, because it is a property of atlas packing rather than of caching.
- **GPU buffers, and a path that loads one.** All five buffer constructors take
  `[]byte`; of 31 `BufferDescr` signatures not one takes a path or a filesystem.
  Nothing reads a file into a buffer without a parse between — the vertices
  reaching `BakeBuffer` are rebuilt from glTF accessors, never file bytes. glTF
  already goes through a cache: the path-keyed model table, whose entry owns its
  buffers, which is what keeps residency atomic. Cross-model geometry dedup is a
  **new capability**, not a migration.
- **Cache invalidation on device loss.** The transition does not exist:
  `extensions/gogpu/internal/gfxbackend.go:339` is the only write to the ready
  flag and it only ever stores `true`, and the Port's doc calls it one-way. A
  `FreeAll` caller for it means inventing the transition, the caller and the
  recovery semantics together, which is a Port and driver change rather than a
  caching one. `FreeCachedResourcesCmd` remains the manual lever.
- **gfx's pipelines, samplers, layouts and parameter plans.** Keyed on
  `ShaderID`-derived structs, with no path, no source and no load. They are freed
  by the shader loader's cascade, which is a different thing from being cached.
- **A filesystem watch, hot-reload, or anything else that evicts a cache by
  itself.** storage's read mounts are fixed at composition and
  `storage.FileSystem` exposes only `Open`: the asset filesystem is static by
  construction, so there is no change to notify about. Redrawing this needs a
  filesystem that changes, which is a storage effort rather than a caching one.
- **`sound`.** The Slot does not exist. This design is what it will use; nothing
  here is shaped by it, and `Clip` in `CONTEXT.md` already describes an asset this
  Library can hold unchanged.
