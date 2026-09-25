# Cog Engine

Cog Engine provides typed runtime systems and the Feuds game. This glossary records the shared domain language used by its subsystems.

## Runtime Architecture

**Engine**:
The composition root. It exists once, owns the plugin set, registry, scheduler, and lifetime, and is built before startup.

**Kernel**:
The runtime handle a plugin uses during one dispatch. It is a value carrying its engine and nothing else, and it is scoped to the handler that received it. It carries no context: cog is a standalone application, so a deadline that describes the work travels in the request that asks for the work.

**Executioner**:
A superset of the kernel that can also dispatch a command synchronously without declaring it. Only the engine mints one, for plugin lifecycle methods and host callbacks, which run outside any handler and so hold no locks.

**Command**:
A synchronous request-response contract handled by exactly one plugin. Its identity is a distinct defined type, declared together with the request and response types that carry its payload.
_Avoid_: Message, RPC, service call

**Handler**:
The factory a plugin registers for one command or subscription. It runs once, during registration, and returns the Lock that binds its resource handles plus the body the engine runs per invocation.

**Command handler**:
The Handler implementing one Command. It is always private: the command type is the name callers dispatch, and the handler behind it is the package's own business. Code abbreviates it to `Impl`, as in a `CmdImpl` suffix.

**Declared dispatch**:
A handler's statement, made in its `Lock`, that it dispatches a given command. Composition folds that command's lock closure into the handler's own set, so the handler never names the resources behind it.

**Plugin**:
A statically linked unit of engine functionality selected before startup and fixed for the engine lifetime. Every Plugin is exactly one of a Slot, an Extension or a Bundle, and what it offers other plugins is declared apart from how it works.

**Plugin dependency**:
A requirement that another plugin complete registration and any optional startup first.

**Slot**:
A Plugin that cannot work until an Adapter fills a Port it requires, and that composition refuses to start without one. It is how the platform varies beneath a piece of engine functionality without that functionality being replaced: the renderer, storage and the application loop stay, and what draws, persists and drives for them changes. What it offers names only its own types.
_Avoid_: Interface, contract half; Open slot, which is retired

**Extension**:
A Plugin that fills Slots' required Ports with Adapters and offers no API of its own. It may also contribute to a collected Port. A plugin that would need both is two plugins. One that fills exactly one Slot is named with that Slot as its suffix, as diskstorage and jsstorage fill storage; one that fills several, as gogpu fills app and gfx, is named freely.
_Avoid_: Backend, driver, as the name of the kind; implementation of a Slot, its retired meaning

**Bundle**:
Every Plugin that is neither a Slot nor an Extension: it requires no Port, though it may collect Adapters through a Port of its own or contribute them to another plugin's. Most engine functionality is a Bundle.
_Avoid_: Module, which is a shader module or a Go module; package; feature

**Port**:
A declared point at which a plugin takes Adapters from other plugins, identified by its own type and typed by the interface its Adapters implement, or by the value type they are when an Adapter is plain data. A required Port takes exactly one, and declaring one is what makes a plugin a Slot; a collected Port takes any number, zero included.
_Avoid_: Plugin, as the name of a kind; the interface alone, which is what a Port carries rather than what it is

**Adapter**:
A plugin's implementation of another plugin's Port, identified by its own type and bound to that Port by the engine during composition. It is a plain value rather than a Resource, so reaching it takes no lock. A window driver's GPU backend is an Adapter of the renderer's backend Port; each plugin's Provider is an Adapter of the Broker's Port.
_Avoid_: Backend, driver, as the name of the kind

**Open slot**, **Vocabulary package**, **Contract root**:
_Retired._ An Open slot was a contract shipped with no implementation, filled by whichever Extension an engine was composed with; a Slot now ships its own implementation and requires an Adapter instead. A Vocabulary package held a Port's Adapter contract beneath it, apart from its recording API; a Slot now declares both together. A contract root was the package holding a plugin's API beside the logic that API needed; a plugin's root now holds declarations only.
_Avoid_: all three.

**Library**:
Code that is not a plugin and defines none, importing only other Libraries and the kernel.
_Avoid_: Package, which is every Go directory; util, common

**Asset**:
Data a plugin loads and holds: named by a path under storage or supplied directly as bytes, decoded, installed into a backend, kept under a key the plugin chooses, and released explicitly. A texture, a model, a sprite, a font face and a sound clip are Assets.
_Avoid_: Resource, which is engine-coordinated shared state; gfx already spells this meaning "resource" in names that predate the term

**Registrar**:
A plugin-scoped capability used only during Registration to declare owned contracts and initial resources, and the Adapters the plugin requires, collects or contributes.

**Registration**:
The lifecycle phase in which a plugin declares the contracts and initial resources it provides and the Adapters it requires, collects or contributes. Adapters are bound once every plugin has registered, before Startup.

**Startup**:
The optional lifecycle phase in which a plugin begins operating after all registrations have been finalized.

**Report**:
A failure handed to the centralized error handler through the Kernel. It is not a return value and carries no answer back: the reporter has finished with the failure. It is the only way a failure nobody can act on travels.
_Avoid_: raise, throw, propagate

**Verdict**:
What the error handler answers to one Report. Nil continues; any error terminates the engine. It is the only place termination is decided — the engine has no opinion of its own, including about a plugin panic.

**Cause**:
The error Run returns. It is the first non-nil Verdict of the engine's life, or the initialization failure that stopped startup. There is exactly one, and whatever it knocked over afterwards is not it.

**Termination point**:
One of exactly two places an engine's life can end: initialization, or a Verdict. Nothing else ends it, and neither refuses work already in flight.

**Quit**:
The ordinary end of a run, with no Cause: something asked the engine to stop, and a Host is asked to leave its loop. A Quit is not a Termination point, because nothing failed.

**Host**:
The single plugin that owns the application's blocking runtime loop, which the engine runs on the calling thread. It is a role the kernel gives one plugin, not a kind: the plugin playing it is still a Slot, an Extension or a Bundle.
_Avoid_: System plugin. A System is the ECS's term for a func run over matching Entities, and has nothing to do with the Host.

**MainLoop**:
The Port app requires exactly one Adapter for: the platform main loop, which gogpu fills on the desktop and the web. app calls it only to hand over its Loop and to quit; the MainLoop runs the platform loop and calls the Loop every frame, and the Loop publishes app's events. The plugin filling it is ordinarily the Host.
_Avoid_: Driver, which is the Store an ecs Query walks; Loop, which is the half app implements and the MainLoop calls

**Tick source**:
What decides when an update tick is published — the MainLoop's frame clock while running, or an explicit step request while paused. Rendering is not a tick source: a paused engine keeps drawing the last completed frame.

**Tick number**:
Which tick, counted from the engine's first and never reset. It is what names the moment something recorded inside a tick describes, so that two such records can be shown to describe one tick rather than assumed to. It is not a frame number: a frame may publish several ticks or none.
_Avoid_: Frame number, timestamp

**Hold**:
A deliberate stop on the publication of a pending step, so that requests arriving over several frames still share one tick. It carries a deadline and expires by itself, because an engine nothing can step is worse than a window that closed early. Without one, sharing a step is opportunistic: the window is only as wide as the gap before the next drawn frame.
_Avoid_: Lock, freeze, barrier; a Hold does not stop the tick — Pause does

**Event publication**:
One delivery of an event value to its subscribers. Separate publications may execute concurrently.

**Event**:
An immutable value delivered to subscribers. Mutable shared payload belongs in a Resource.

**Subscription dependency**:
A completion-order constraint within one event publication. A dependent subscriber cannot begin until its prerequisites complete. A subscriber that Reports has still completed; only one that panicked blocks its dependents.

**Publication handle**:
The completion result of an event publication. Callers may discard it or wait for every runnable subscriber to finish.

**First phase**:
The initial subscriber group in an event publication. Members may execute concurrently subject to dependencies between members; later phases wait for the group to complete.

**Last phase**:
The final subscriber group in an event publication. Members may execute concurrently subject to dependencies between members and begin only after earlier phases complete.

**Resource**:
Shared state whose declared access is coordinated by the engine. Plugins may also own state and coordinate its concurrent access themselves.
_Avoid_: A second, ECS-local meaning — what other engines call a resource is exactly this.

**Resource handle**:
A binding to a resource cell, obtained during registration and valid for the engine lifetime. The value it exposes is valid only while the owning handler holds its lock.

**Lock**:
The closure that binds a handler's resource handles. Requesting a handle is what declares the corresponding lock, so declaration and use cannot drift apart. It runs once, during registration.

**Required resource**:
A resource that must have an owner and initial value when registration is finalized.

**System fault**:
An unexpected engine or plugin failure that cannot be represented as an expected command response.

**Invocation context**:
The cancellation and deadline scope of one command or subscriber execution, bounded by the engine lifetime.

**Conflict-aware FIFO**:
Scheduler fairness in which later work may pass an earlier request only when it does not access any resource requested by the earlier work.

**Nested command**:
A command executed by another handler using only resource access already held by that handler.

**Architecture description**:
A read-only account of finalized plugin order, contract ownership, the Adapters each Port was bound to, subscription dependency graphs, and the lock set each handler ends up holding once its declared dispatches are folded in. It states what composition produced, which no single source file does.

**Headless engine**:
An engine without a Host. It remains running until its context is canceled.

**Shutdown**:
The optional lifecycle phase that stops active plugins in reverse dependency order before the scheduler stops.

## Entities and Components

**Entity**:
An opaque handle to one thing in the simulation. It is comparable, copyable, and usable as a map key. It carries a generation, so a handle to a despawned Entity is detectably stale rather than silently addressing whatever took its place. Its zero value means "no Entity".
_Avoid_: Id, object, actor, game object

**Entities**:
The authority on which Entities exist: it allocates them, tracks their generations, and answers whether one is alive. It knows nothing about which Components an Entity has, but it can reach every Store, because a Despawn has to empty all of them and no Entity records which ones it is in. Holding it for write is therefore the one lock that covers every Store at once; every System that touches any Store holds it for read, and that is what makes the coverage true rather than merely intended. There is exactly one per Engine, and that is what makes an Engine the boundary of one simulation: a second simulation is a second Engine, never a second Entities.
_Avoid_: World, Registry

**Writeable Entities**:
The promotion of a write-locked Entities into the thing that can Spawn and Despawn. A System gets one only by declaring the write, so the authority to change which Entities exist is visible in its signature and nowhere else.

**Component**:
A plain value an Entity either has or has not, addressed by its Go type. It contains no _mutable_ indirection, transitively — every pointer it holds, it holds to memory nothing can write — which is checked when the type is registered. That admits numerics, bools, fixed-size arrays, an Entity, a string, a Blob and a List, and refuses pointers, bare slices, maps, channels, funcs and interfaces. The rule is about the lock unit rather than the collector: a read yields a copy, and a copy of a slice header is a write handle on the Store that no lock names, where a copy of a string is not. An Entity holds at most one Component of a given type.
_Avoid_: Attribute, property, field

**List**:
A fixed-length run of values a Component may hold, and the only way a Component holds variable-length data at all. Its backing array is unexported and its constructors copy, so reading one hands out no way to write the Store; its one element mutator is legal only on the List a Component holds, reached under a write lock, and only while no other Component holds the same one, and is checked in a validating build. Its length is fixed at construction, because growing means allocating, so a List whose length changes is a new List written into the Component. Its elements may hold Lists of their own, and those are checked the same way.
_Avoid_: Slice, array, vector, buffer. A bare slice in a Component is refused, and the word for the fixed-size Go array a Component may also hold is just an array.

**Blob**:
A run of bytes treated as static: once the value holding it is built, nothing writes the bytes again. It is how an engine value carrying pixels, a buffer's contents or a parameter's raw layout says so, and the one run of bytes a Component may hold outright. Its identity is the run rather than the contents: two allocations spelling the same bytes are two Blobs, and re-wrapping the same one is one Blob, which is what lets a value holding one be compared and used as a cache key. The ECS admits it on that contract rather than on a property it can check, and Validation mode cannot see a write through one.
_Avoid_: Buffer, which is a GPU object; bytes, for a run that is still being written

**Maybe**:
A value that may be absent, held inline with no pointer. Its zero value is absent, so an optional field nobody wrote reads as unset, and a present zero stays distinct from it. It is what an optional field uses where a pointer would make the value mutable indirection, whether the value is stored, compared or rendered for an agent. It crosses JSON as the nullable value it holds, and an agent's tool schema reads it the same way.
_Avoid_: Option, nullable, pointer-to-mean-optional

**Tag**:
A Component with no fields. Its presence is the whole of what it says, and its purpose is to narrow a Query. It is not a place to keep a boolean: a fact the Entity carries data about belongs in that data's Component, and no fact is encoded twice.
_Avoid_: Flag, marker, label

**Component set**:
The exact set of Component types one Entity has. A Spawn names the one a new Entity starts with as a struct type, the way a Query is, whose value carries the Components themselves; from then on the Entity may gain and lose Components and the struct type means nothing. It is not a structure the engine keeps, and nothing groups Entities by it.
_Avoid_: Archetype, table, signature; Bundle, which is a kind of Plugin; Prefab and Template, both still unspent

**Component registration**:
The Registration-phase declaration that one Component type exists, made once per type by exactly one plugin. It is what makes the type's Store exist, so a type no plugin registered cannot be added, read, or locked.

**Store**:
The engine's holding of every value of one Component type. There is one per registered Component type, and it is the unit a lock is taken on. It knows how many Entities it holds, and that number is what a Query consults to choose its Driver.
_Avoid_: Pool, column, table. Also Page and Chunk, both of which stay unspent: a Store's index is not divided into blocks, and nothing groups its rows. Chunk is held in reserve for a run of rows sharing one change version, which is the only thing a real block would buy.

**Transform**:
Where one thing stands — an Entity, or a draw recorded straight into a renderer: a position, a rotation and a per-axis scale, and nothing else. It is `m.Transform`, built with `m.At` and `m.LookAt`, and there is exactly one such type in the engine. Its zero value is the identity, and so is an all-zero scale; a scale with only some axes zero is taken literally, which is what makes a flattened scale expressible. A non-uniform scale sends that thing's normals through the inverse-transpose and costs nothing to anything else. Its one Store is the ecs plugin's, registered by the ecs plugin itself rather than by a plugin that defines the type, so a game's own Systems and every binding read the same placement. Two Components describing one position would be unrelated to the scheduler, whose lock unit is the Component type: two Systems writing them run concurrently, and nothing reports that they disagree. The remedy is direction: exactly one System writes each, and a copy from one into the other never runs back.
_Avoid_: Matrix, model matrix. A Transform has no matrix to override it; a model's flattened node world is Scene's own business.

**Query**:
A struct type whose field types are the Component types one System touches. A field's pointer-ness is its access mode: a pointer field is written and yields the stored value itself, a value field is read and yields a copy. A Query matches every Entity having _at least_ those Component types, which is why it is not a Component set. It selects on presence and on nothing else: no Query narrows by what a Component _contains_, so finding every Entity whose Reference points somewhere in particular is a comparison the System makes itself, once per candidate.
_Avoid_: View, archetype

**Filter**:
A Query field that narrows which Entities match without yielding anything into the Query. It still reads its Component's Store, because presence is information and reading it is a read, so it contributes to the System's lock set like any other field. It is the reason a Tag exists.
_Avoid_: Predicate, matcher

**Driver**:
The one Store a Query walks to find candidates, every other Component it names being checked against each candidate in turn. A Query costs what its Driver is long, not what it matches, so narrowing a Query with a Tag can be the difference between visiting a hundred Entities and five thousand. A `Without` can never be the Driver: it names the Entities to exclude, and nothing lists the rest. app's platform loop is the MainLoop, not a Driver.
_Avoid_: lead, primary, base

**System**:
A plain Go func called once per tick, which iterates the Entities its Queries match itself. It takes whatever a cog handler may take — Queries for its Components, Accessors for Entities it did not iterate to, the handles for Spawn and Despawn, read or write access to the Resources of any plugin it binds to, and the Kernel — and its lock set is the union of all of them, derived from the signature at registration. It can touch nothing that signature does not name. Naming the event that drove the tick is legal but is not the ordinary shape, because a System that names one can only ever be subscribed to that one; what it needs from the tick reaches it as a plain value instead.
_Avoid_: System plugin, which is the Host

**Structural change**:
Any change to which Entities have which Components — adding or removing a Component, spawning or despawning an Entity — as opposed to a change to a Component's value. A System may make one to the Entity it is currently visiting; changing whether some _other_ Entity is in the Store being iterated is undefined, and so is using any pointer into a Store after that Store has structurally changed.

**Hook**:
One record of one act on one Component of an Entity: the act's kinds, and that Component's value — for a removal, the last one at that moment; for an addition or a change, the latest before the Entity next loses the Component. There are five kinds, all relative to that Component — added, removed, spawned with it, despawned holding it, and changed, meaning its value differs once the System that could write it has finished running — and one act can be several at once, since a Spawn is also an addition, a Despawn also a removal, and every addition also a change. Between two additions or removals of one Entity there is at most one record of change, and an addition absorbs it. A System reads the Hooks of one Component, for one of a fixed set of kind combinations, in the order they happened, each System its own copy, gathered since the end of its own last run, and is never shown the changes of value it made itself. A Hook narrows by nothing else: a System that cares about Entities holding several Components reads the Hooks of each one that can change its answer and checks the rest itself. Nothing runs on a Hook's behalf at the moment of change, and recording one never stops two Systems that could run in parallel from doing so.
_Avoid_: Callback, observer, listener, trigger, `OnAdd`/`OnRemove`, Entered/Exited (a Hook is about one Component, never about a Query's match), and Event, which is a kernel term for something published rather than read.

**Spawn**:
Creating an Entity with a given Component set and its values, as one Structural change. Despawn is its inverse and is total: it removes the Entity from every Store, so nothing anywhere still holds it.
_Avoid_: Instantiate, Instance, create

**Drain**:
Applying queued Spawns and Despawns, done by a System holding Entities for write. Queuing one is not a Structural change and changes nothing anyone can see — not even the System that queued it: a queued Despawn leaves its Entity alive and iterated, and a queued Spawn does not exist. The Drain is the Structural change, made by the System that drains, so who sees it follows from which Systems ran after that one, exactly as for an immediate Spawn or Despawn. Nothing reports what is queued.
_Avoid_: Flush, commit, apply, sync, and command buffer, which names the type-erased general queue that was rejected

**Reserved Entity**:
What a queued Spawn hands back: an Entity whose handle is fixed when the Spawn is queued and which exists from the Drain on. Until then it is not alive, it can be stored as a Reference that resolves to nothing, and any Spawn, immediate or queued, is never given the same handle. A queued Despawn of it is applied after it is spawned.
_Avoid_: Placeholder, pending Entity, proxy, promise

**Deferred Spawn**, **Deferred Despawn**:
The two handles that queue a Structural change instead of making one. Each is a System parameter beside the immediate Spawn and Writeable Entities, holds its own buffer of what it has queued, and keeps the immediate handle's method name, so making a System's spawns deferred is a change to its signature and to nothing else. A Deferred Spawn names a Component set and hands back a Reserved Entity; a Deferred Despawn names nothing and reports nothing, because the Drain rather than the call decides what a queued Despawn does. Holding either declares the read of the authority and never the write, which is the whole point, and a Deferred Spawn declares a read of each of its Component set's Stores besides — not because it writes them, but because declaring is what names the plugin whose Components it spawns. What they hold is queued; what they are is deferred, and the two words do not swap.
_Avoid_: Command buffer, pending list, spawn queue as a name for the handle rather than for what it holds

**Reference**:
An Entity kept inside a Component — a missile's target, a light's owner. Following one is the ordinary way to relate two Entities, and it stays safe when the far Entity is gone: a Reference to a despawned Entity resolves to nothing, because a Despawn empties every Store and a generation cannot match twice. It points one way only. The far Entity does not know it is referenced and nothing anywhere lists what points at a given Entity, so a relation with a many side keeps that side as several References on the one side — the count fixed by whoever declares the Component, never by the engine.
_Avoid_: Link, pointer, handle. Also Parent, Child and Hierarchy, which are a game's words for its own Components and mean nothing to the engine. Relation is held in reserve for a Store holding many rows per Entity keyed by the Entity each names, which is the only shape that would make the far side answerable without an index every Structural change has to maintain.

**Accessor**:
A System's means of reaching one Component of an Entity it did not iterate to, which is how a Reference is followed. It comes in a reading form and a writing one, and like a Query field it declares its Component in the System's lock set — reaching an Entity through a Reference is not a way to touch a Store the signature did not name.
_Avoid_: Lookup, fetch, getter

**Hash**, **Name table**:
_Retired._ The ECS supplied a 64-bit name hash and the table a consumer resolved one through, because a Component could not hold a string. One can, so naming an engine-side thing is a matter between the plugin that writes the name and the plugin that resolves it, and the engine has no word for it. Measured before removal: a stored hash resolved in 6.66 ns against 7.79 ns for the name itself as a map key, and a dense index in 0.64 ns against either.
_Avoid_: reintroducing either word in the ECS. A consumer that wants a process-stable name is free to hash in its own package, where it is that package's vocabulary.

**Validation mode**:
A build tag that compiles in the checks that nobody writes a List except through the Component holding it under a write lock, and that a System reading Hooks keeps pace with the Systems writing its Component. It is on under `-tags ecs_validate` and absent otherwise, so a release build carries no branch and no table for it. It is detection rather than prevention, and its coverage is whatever a run executes — which is a weaker guarantee than the rest of the design offers and is the price of a Component holding mutable data at all. It does not see a write through a Blob, which has no mutator to check.
_Avoid_: Debug mode, safety checks, assertions.

## Agent Interface

**Agent**:
An external, LLM-driven client attached to a running engine. It observes through capabilities and reaches the game only through synthetic input.
_Avoid_: Client, user, bot

**Capability**:
A named, described, typed unit of engine functionality a Provider offers to an Agent. It carries its own request and response types and is fixed for the engine lifetime.
_Avoid_: Tool, action, endpoint

**Provider**:
A plugin's capabilities, contributed to the Broker as an Adapter. It speaks cog contracts only; it never emits protocol vocabulary.

**Broker**:
The single plugin that collects capabilities from every provider and serves them to an agent. It holds no knowledge of what any capability does.
_Avoid_: Server, gateway, bridge

**Capture**:
One rendered colour target taken off the GPU and written to a file the Agent names. It names a moment: a Capture shows the game as of a tick that began after the request, rather than the last frame kept around. One request may ask for several, but each one still names its own moment — see Burst.
_Avoid_: Screenshot, frame grab; a Snapshot is the other thing an Agent asks for from the same moment

**Burst**:
One Capture request that writes a still per tick for several consecutive ticks, at a fixed interval. It is not a recording: nothing is encoded, nothing plays, and an Agent reads a few of the stills rather than all of them.
_Avoid_: Sequence, film, recording, video

**Readback**:
The transfer of a rendered texture from GPU memory into CPU memory, which a Capture is built on. It is renderer vocabulary and belongs to gfx: a Provider offers Captures, and only gfx and its Backend speak of readback.

**Snapshot**:
One tick's recorded declarations, rendered while they are still alive. It is not a copy of a queue: no queue outlives the tick that filled it, so a Snapshot is produced inside one and shaped by the request that asked for it. It carries the Tick number of the tick it was produced in, so which moment it describes is something an Agent reads rather than infers. A Capture is the pixels; a Snapshot is what produced them, and the two are meant to name one moment.
_Avoid_: Dump, inspection, capture

**Synthetic input**:
Input an Agent sends through the same seam a person's input arrives on: folded into the polled input state and published as the same discrete events, carrying no mark that distinguishes it and no lifetime of its own. A key an Agent presses stays down until something releases it.
_Avoid_: Simulated input, fake input, injection

**Tool**:
The protocol rendering of one Capability: its name, JSON schema, description, and annotations as an agent's client sees them. Written by the Broker, never by a Provider.
_Avoid_: Use Capability when talking about cog

## UI Declarations

**Element**:
A frame-local declaration of layout, interaction, and visual intent.
_Avoid_: Widget, control

**Modifier**:
A value transformation that derives one Element declaration from another.
_Avoid_: Setter, constructor

**Pixel value**:
An absolute distance in screen pixels.
_Avoid_: Absolute value

**Relative value**:
A ratio of the containing axis, except for pivots, where it is a ratio of the Element's own axis.
_Avoid_: Percentage

**Borrowed children**:
An Element sequence the UI may hold by reference instead of copying. Its storage remains caller-owned and must stay stable while the UI frame is processed; whether any given sequence is borrowed or copied is unspecified.
_Avoid_: Copied children, owned children

**UI Frame**:
The complete set of root Element declarations produced for one update tick.
_Avoid_: Retained UI, scene

## Scene Declarations

**Camera**:
A declaration of a viewpoint and of the passes drawn from it. Its identity is also its place in the frame's ordering space, so declaring one twice is an error.

**Pass**:
One render pass a Camera emits, carrying the tag that selects materials for it, its target, and its clears. Each clear is a Maybe: an absent one preserves what the target holds, so a zero Pass clears nothing.
_Avoid_: Render step, stage

**Pass tag**:
The name of what a Pass is for, and the key that selects which of a Scene material's entries serves it.
_Avoid_: Queue, light mode

**Scene material**:
The set of graphics materials one recorded thing offers, one per Pass tag. A Pass whose tag it has no entry for does not draw that thing. A recording call copies it — its entries and each entry's parameters, but not their Blobs, and once per frame per distinct content — so the caller may change it the moment the call returns; two equal ones batch together however each was built, because a Scene material is keyed by content.
_Avoid_: Shader

**Model material**:
What one material in a model file becomes once loaded: for each shader variant, the graphics material that draws it in a forward pass, together with the numbers the bundled PBR reads, and a content key fixed at load. It names no Pass tag. A renderer wraps it in its own material under whatever tag it chooses, so the same loaded file serves both renderers unchanged, and nothing re-keys it per draw.
_Avoid_: Scene material (that is a renderer's, and carries tags)

**Layer mask**:
A selection of which Cameras see a recorded item. Both a Camera and an item carry one, and an empty mask on either side means every layer.
_Avoid_: Render layer, culling group

**Selector**:
The scene and node names that address part of a model file. A Selector that matches nothing addresses nothing and never widens to the whole file.
_Avoid_: Path, query

**Re-rooting**:
The discarding of a selected node's authored world transform, so that the node's subtree is placed by the recording call's own transform instead.

**Residency**:
Whether a model path is loaded and drawable. It is not a cycle and has no in-flight state: the load runs inside the call that asks for it, so by the time that call returns the model is either loaded or it failed, terminally, with the reason it failed.
_Avoid_: Cache state, load status, loading state

**Frame boundary**:
The point between two frames at which the geometry a caller staged is uploaded and the buffers it gave up are released. Loading and unloading are not deferred to it: both happen where the caller stands.

**Rest pose**:
A model's placement with no animation playing: its authored hierarchy resolved once. It is what a bounds query answers about, whatever the frame is playing.
_Avoid_: Bind pose, default pose

**Debug shape**:
A recorded primitive drawn from a plugin-owned unit mesh rather than from an asset on disk.
_Avoid_: Gizmo, primitive

**Instance**:
One placement of one recorded thing. A recording call carrying many transforms declares many Instances that share everything else it said.

**Batch**:
One run of recorded work merged into a single draw call, whatever supplies its per-item data. In Scene that is Instances sharing a mesh and a Scene material; in Canvas it is sprites sharing an Instance record buffer, or triangles sharing concatenated vertices.
_Avoid_: Cluster, group

## Mesh Storage

**Vertex layout**:
The ordered mapping from an interleaved vertex buffer's bytes to shader locations — a format and an offset per attribute, and the stride they imply. What a pipeline is keyed on, and what a shader's declared inputs are checked against.
_Avoid_: Vertex format, vertex declaration

**Named layout**:
One of the vertex layouts Scene blesses and the bundled PBR knows. The set is closed and small; anything else is a Custom layout.

**Custom layout**:
A Vertex layout an app defines itself. Legal, and obliges the draw to carry its own material, because the bundled PBR knows only the Named layouts.

**Authoring vertex**:
The Go struct an app writes when it builds a mesh itself. Write-only: nothing in Scene ever hands one back, so it has exactly one authoritative form and it flows one way.
_Avoid_: Vertex struct

**Storage vertex**:
The bytes Scene actually uploads for one vertex. Derived from the Authoring vertex when the mesh is baked, and read by a shader — so it is public contract rather than an internal detail, and need not have the same shape as what was authored.

**Sparse target**:
Which *vertices* one morph target stores a record for. Distinct from the sparse weight list, which is which *targets* reach the shader for a draw; the two are different mechanisms in different buffers.

**Live span**:
The contiguous run of vertices a Sparse target stores records for. Records are dense within it, and a vertex outside it has no record rather than a zero one.

## Shader Sources

**Shader source**:
One `.wgsl` file. Sources are what an include composes; a source is not a unit the GPU ever sees.

**Shader module**:
The compiled WGSL unit behind one shader identity. Many Shader sources flatten into one Shader module, so the two are never interchangeable.
_Avoid_: Calling an included source a module

**Root source**:
The Shader source a shader descriptor names, by path or by text. It is the entry point of one flatten, and the only source addressed from Go.

**Define**:
A valueless flag readable only by a preprocessor conditional. It never reaches WGSL.
_Avoid_: Macro, symbol

**Const**:
A named value that becomes a WGSL `const`. It is never readable by a conditional, and its value is text the preprocessor does not interpret.
_Avoid_: Macro, override

**Supply**:
The set of Defines and Const values Go provides to one shader, fixed when its descriptor is constructed.
_Avoid_: Options, flags

**Variant**:
The Shader module one Root source plus one Supply produces. One path with two Supplies is two shaders, not one shader with two states.

## Canvas Materials

**Canvas material**:
A shader, pipeline state, and the parameters that belong to the material rather than to a draw, bound to a Canvas draw. Canvas supplies a default per Family, publishes others an app can name, and an app may supply its own.
_Avoid_: Shader, effect

**Family**:
Sprite or triangles: the two Canvas draw shapes, which sample differently and can never be one shader. A Canvas material belongs to exactly one.
_Avoid_: Kind, mode

**Material set**:
One Canvas material per Family plus one shared parameter list. A Scope names a Material set because it covers draws of more than one Family; a draw names a single Canvas material because at a draw the Family is known.
_Avoid_: Using set and material interchangeably

**Scope**:
Something that covers many draws and supplies a Material set to those that name none: the whole Canvas op queue, a layer, a UI frame, or a UI element subtree. The nearer scope wins, and a scope that names an empty set stops the wider one rather than reading as saying nothing.
_Avoid_: Context, group

**Instance record**:
The fixed record one sprite contributes to its Batch's storage buffer. It is not a parameter, not a uniform, and not extensible.
_Avoid_: Instance buffer, which is the array of them

**Reserved parameter name**:
A parameter name Canvas consumes itself and never forwards to the Canvas material.
_Avoid_: Built-in parameter

**Halo**:
A soft outward fade in a named colour, drawn by a Canvas material that paints the band and no mark, so a caller records the same marks on a halo layer and on the ink layer above it.
_Avoid_: Glow, outline, shadow

## Audio

**Device**:
What makes sound audible: a sound card, or the browser’s audio context. There is one per Engine, and a game neither opens nor chooses it — it only reads whether there is one. A Device may be absent, not ready yet, or lost, and a game sees one thing in all three. Voices play on regardless: a Voice’s playhead advances whether or not anyone can hear it.
_Avoid_: Output, speaker, sink. Also backend, which is the Adapter beneath the Slot rather than the thing it found.

**Clip**:
A sound a Voice plays, loaded from a path or from a Blob of encoded bytes and kept until it is released. What named it is what finds it again, so two plays of one path, or of one Blob, play the same Clip — and two Blobs with equal bytes are still two Clips. Releasing is optional: a game that drives sound through Components names Clips and never releases one. Releasing a Clip stops every Voice playing it, which is why a game that wants a sound to finish simply does not release its Clip.
_Avoid_: Sound, sample, audio file. Also Buffer, which is a GPU object.

**Prepared Clip**:
A Clip turned into something a Voice can be started from. What that is depends on the Clip: a short one becomes samples every Voice playing it shares, a long one stays as its bytes and a way to read them while it plays. Which, is the backend's own business — a game never sees a Prepared Clip and cannot tell which kind it got.
_Avoid_: Decoded clip, PCM, stream. Also Buffer, which is a GPU object.

**Loop Region**:
The span of a Clip a looping Voice repeats between. The Clip declares it and the game never states one, so a Voice told to loop repeats the way its Clip says to; a Clip that declares none repeats whole. It is why an intro can run into a loop at all, and it is the reason a loop has no gap where a track chained on an ending does.
_Avoid_: Loop point, marker, cue. Also Seek, which is a game moving a Voice rather than a Clip describing itself.

**Voice**:
One playing instance of a Clip, begun by a play and addressed afterwards by what that play handed back. Any number may play one Clip at once, and a Voice that has ended is addressed by nothing.
_Avoid_: Sound, source, channel, instance

**Stealing**:
What ends a Voice to make room when every slot is taken. The Voice that loses is the least audible one — its own volume through its Bus, its Falloff and its Cone — unless a Priority puts it out of reach, and the play that arrives is as stealable as anything already playing. A game hears about it through the same ending that tells it about a Stop. A steal is final: nothing resumes a stolen Voice, and a game that wants the sound back plays it again as a new Voice.
_Avoid_: Voice limit, culling, eviction. Also ducking, which is a game lowering a Bus on purpose.

**Bus**:
A group of Voices the game declares, sharing one volume. Every Bus sits directly under Master, Buses do not nest, and a Voice that names none plays on Master.
_Avoid_: Channel, group, mixer track, category

**Mixer**:
What turns Voices into the samples a Device consumes. It runs outside the Engine, on the Device’s own clock rather than the tick, and nothing that takes a lock reaches it — which is why a game never addresses one and why everything it is told arrives as a whole tick at once.
_Avoid_: Audio thread, callback, engine. Also Bus, which is a grouping a game declares rather than the thing that does the mixing.

**Listener**:
The place and facing a Positional Voice is heard from. There is one per Engine, and the game puts it where it wants: audio never looks at a Camera, so a Listener follows one only because something copies it across.
_Avoid_: Camera, ear, microphone

**Positional Voice**:
A Voice that has been given a position, and so is quieter with distance from the Listener and heard from its side. A Voice without one is heard from nowhere in particular, which is what a UI click is. The first position makes a Voice positional for the rest of its life.
_Avoid_: 3D sound, spatial sound, as though 2D were a different kind

**Falloff**:
How a Positional Voice grows quieter with distance from the Listener. Each Voice carries its own.
_Avoid_: Attenuation curve, rolloff, as the name of the whole

**Cone**:
The directions a Positional Voice is loud in, when it has a facing as well as a position. Without a facing a Voice is loud in every direction, whatever its Cone says.
_Avoid_: Directivity, beam

**Fade**:
A change of volume over time that a game drives itself, from a timeline, by changing a Voice or a Bus each tick. Audio has no word for it and no verb that performs one.
_Avoid_: Ramp, crossfade, tween, as names for anything audio does

## Physics

**Body**:
An Entity that physics moves or collides with: it has a position and an Angle on the plane, and a Shape or a velocity. Every Body is exactly one of the three kinds below, and which one is said by the Components it has rather than by a flag. A Body stands on a 2D plane; placing it in 3D is a Transform's job, written by whoever draws it. The copy runs one way, from physics' `Position` into `m.Transform`, in one System, and never back.
_Avoid_: Collider, rigid body, physics object, actor

**Static body**:
A Body that never moves and is never pushed, and whose Shape and position never change in place: moving one means replacing the Entity. It pushes Dynamic bodies.
_Avoid_: Wall, level geometry, as the name of the kind

**Kinematic body**:
A Body the game moves by setting its velocity. It pushes Dynamic bodies and nothing pushes it, which is what infinite mass means here.
_Avoid_: Immobile, as a kind of its own

**Dynamic body**:
A Body with a mass, a Moment of inertia and a Damping, moved and turned by the forces and Torques on it and pushed by what it touches.

**Sleeping body**:
A Dynamic body that physics has stopped moving because it and everything in its Island stayed idle long enough, until something disturbs it. Sleeping is off unless the game turns it on.
_Avoid_: Frozen, inactive, disabled, deactivated

**Island**:
The Dynamic bodies joined by touching or by Joints, which fall asleep together and wake together. A Static or Kinematic body never belongs to one and never joins two.
_Avoid_: Component, group (Component is the ECS’s word)

**Constants**:
The physics values that hold for the whole world rather than for one Body, such as gravity. Physics starts them at its own defaults and reads them every tick; a game that wants others changes them itself, and a change applies from the next tick.
_Avoid_: Config, settings (those are fixed when physics starts), World

**Angle**:
How far a Body has turned on the plane, in radians, counted on from every earlier turn rather than wrapped into one revolution.
_Avoid_: Rotation, heading, facing, orientation

**Angular velocity**:
How fast a Body's Angle changes, in radians per second.
_Avoid_: Spin, rotation speed

**Torque**:
What turns a Dynamic body, as a Force moves it; the game adds it and the physics consumes it each tick.
_Avoid_: Angular force, moment (alone)

**Moment of inertia**:
How hard a Dynamic body is to turn, as mass is how hard it is to move. An infinite Moment of inertia is a body that does not turn, not a kind of its own.
_Avoid_: Inertia (alone), rotational mass, fixed rotation

**Centre of gravity**:
The point a Body moves and turns about, which is the Body's position itself. A Shape is placed relative to it and need not be centred on it.
_Avoid_: Centre of mass, pivot, origin, anchor

**Shape**:
The one convex region a Body occupies, named by its kind: a circle, a segment, or a Polygon. A point is a circle of radius 0, and a box is a Polygon of four vertices that turns with the Body. Any kind may be rounded, which thickens it by a radius everywhere rather than adding a second Shape, so a rounded segment is a capsule. A Body has at most one, and an outline that is not convex is a chain of segments, one Body each, never one Shape.
_Avoid_: Collider, fixture, concave shape

**Polygon**:
A Shape bounded by straight edges between its vertices, always convex and always wound the same way. A triangle and a box are the small ones. Vertices describing a dent do not make a Polygon, and nothing is quietly rounded off to pretend they do: what comes back is a point, saying why, so a mistake is never mistaken for the region that was asked for.
_Avoid_: Mesh, hull (that is the outline it makes, not the Shape), n-gon, vertex buffer

**CollisionBits**:
Which collision groups a Shape is in; it may be in several, and a Shape in none of them collides with nothing. An Entity with no Shape collides with nothing either. A query names the groups it is in the same way. Rules about a particular pair, such as a projectile passing its own caster, are not groups.
_Avoid_: Layer, group (alone), category, mask, collision filter

**CollidesWith**:
Which collision groups a Shape collides with, carried by the Shape itself and changed by writing it. Two Entities collide only when each one's groups are among the other's, so either side alone can refuse. A query says what it looks for the same way, and a Shape that collides with nothing is invisible to queries too.
_Avoid_: Mask, collision matrix, layer matrix, filter

**Sensor**:
A Shape that reports contacts but is never pushed and pushes nothing. Its collision groups pair it like any other Shape's. A sensor that moves, whatever its Shape, reports everything it touched on its way through the tick, not only where it ended, which is what keeps a point from passing through a wall unseen. What a sensor does on contact is the game's, never the physics.
_Avoid_: Trigger (a Hook word), ghost, phantom

**Contact**:
Two Entities whose Shapes touch, found once a tick for each pair their collision groups let collide, whatever kinds of Body they are. It holds the at most two Contact points where they meet, and one surface normal for both. A contact is marked as begun this tick, continuing from the last, or ended, so the tick two Entities stop touching is reported too. When one party is a Sensor, or a Body was stopped short, the contact also says how far through the tick the touch happened. A game may drop a contact for the tick, or ignore the pair until the two come apart; a dropped contact that was continuing is reported as ended.
_Avoid_: Collision, collision event, manifold, touch

**Contact point**:
One of the at most two places a Contact touches, each with where it is, how deep the two Shapes overlap there, and the Impulse the physics delivered there. Two is enough because two convex Shapes meet along a line at most, and its two ends say everything its middle would. A Contact involving a round Shape has one.
_Avoid_: Manifold (that is the pair of them together, and the game never needs a word for it), contact patch, feature, collision point

**Slop**:
How far two Shapes are allowed to overlap and be left alone: a small distance the physics never bothers to push out. It exists because driving every overlap to exactly nothing makes resting Bodies jitter against each other for ever, and leaving a sliver settles them. It is why a Body at rest sits slightly inside whatever holds it up, and it is one distance for the whole world rather than anything a Shape carries.
_Avoid_: Skin, margin, tolerance, penetration allowance, linear slop

**Impulse**:
How much push was delivered in an instant, rather than a Force spread over a second, which is what a game asks when it wants to know how hard something was hit. A Contact reports one at each of its points, and a Joint reports the one it delivered holding its two Bodies together — which is how a game notices a Joint under a load worth breaking.
_Avoid_: Force (it is not one), momentum transfer, hit strength

**Friction**:
How strongly two touching Shapes resist sliding across each other, as a share of how hard they are pressed together rather than a speed or a force. Pressing harder grips harder in the same proportion, which is why a heavy crate and a light one shoved the same way slide the same distance. Each Shape carries its own and a Contact's is the two of them multiplied, so one slippery Shape is enough to make a pair slide. It exists only where two Shapes touch, which is what separates it from Damping.
_Avoid_: Drag, Damping, grip, traction, coefficient of friction

**Restitution**:
How much of its approach speed a Body keeps when it bounces off what it hit: none means it stops dead against the surface, all of it means it leaves as fast as it arrived. Each Shape carries its own and a Contact's is the two of them multiplied, so one dead Shape absorbs the bounce however lively the other is.
_Avoid_: Bounciness, elasticity, bounce, springiness, coefficient of restitution

**Damping**:
How fast a Dynamic body's velocity decays on its own, as a rate per second; its Angular velocity decays by an angular Damping of its own. It is what makes a pushed thing stop and a spun thing settle when nothing else holds it back. It is a rate, not the share of velocity kept each second. It slows a Body wherever it is, touching nothing, which is what separates it from Friction.
_Avoid_: Drag, damping ratio, Friction (a Contact's, not a Body's)

**Joint**:
A rule the game sets that holds two Bodies to each other: at a fixed distance, about a shared pivot, within a range of Angles, turning in step. It is an Entity of its own rather than something a Body carries, because one Body may be held by several. It has nothing to do with the two Bodies touching — what it holds is where they are and how fast they move, enforced each tick by pushing them. A Joint may also be told to let its two Bodies pass through each other, which is what makes a jointed figure of limbs possible.
_Avoid_: Constraint, link, connection, hinge, weld

**Spring**:
A Joint that pushes its two Bodies towards a rest distance, or a rest Angle, harder the further they are from it, and settles instead of swinging for ever by an Absorption of its own. It is the one kind of Joint that holds nothing: it only pushes, so anything else acting on the two Bodies can win against it.
_Avoid_: Damper, elastic, rubber band, soft constraint

**Absorption**:
How strongly a Spring resists its two ends moving apart or together, as force per unit of that speed. It is what makes a Spring settle rather than swing for ever. It belongs to one Spring, which separates it from Damping, a Body's wherever it is, and from Friction, a Contact's where two Shapes touch.
_Avoid_: Damping, damping ratio, damper, drag

**Probe**:
Moving a Shape, without turning it, in a straight line from one position to another, and finding what it touches on the way. The Shape is most often a circle, possibly of radius 0. Line of sight is one use of a Probe, not another operation.
_Avoid_: Sweep, ray cast, shape cast, segment query, trace

**Continuous collision**:
Stopping a Body that moves far enough in one tick to get through something, either clean past it or deep enough to be pushed out its far side, so it meets what it would have hit instead of ending up beyond it. It engages only for a Body whose movement in the tick reaches its own thinnest width; anything slower is caught by ordinary contact. It always stops a Body at the Static bodies in its way; meeting the Kinematic and Dynamic bodies in its way too is asked for by the Body's Shape, because it costs a fast Body several times as much, and without it a fast Body can pass through them. It follows the straight line between where the Body was and where it is, so a thin thing spinning fast can still slip through. A Kinematic body is never stopped: what it would have hit is carried along with it instead.
_Avoid_: Sweep, bullet, CCD, time of impact (one way of doing it, not the behaviour)

**Hit**:
What a Probe reports about one thing it touched: the Entity, how far along the Probe, where, and the surface normal.
_Avoid_: Contact (that is what detection reports each tick, never a query result), intersection

**Overlap**:
Asking which Entities a Shape at a position touches. It counts as touching exactly what contact would.
_Avoid_: Shape query, area query

**Static index**:
The plugin's index over Static bodies, asked when only what stands still matters.
_Avoid_: Space, world, broadphase

**Body index**:
The plugin's index over every Body with a Shape that is not static. Which index a query asks is the caller's choice.
_Avoid_: Space, world, broadphase
