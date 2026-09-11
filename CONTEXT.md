# Cog Engine

Cog Engine provides typed runtime systems and the Feuds game. This glossary records the shared domain language used by its subsystems.

## Runtime Architecture

**Engine**:
The composition root. It exists once, owns the plugin set, registry, scheduler, and lifetime, and is built before startup.

**Kernel**:
The runtime handle a plugin uses during one dispatch. It is a value carrying its engine and invocation context, and it is scoped to the handler that received it.

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
A statically linked unit of engine functionality selected before startup and fixed for the engine lifetime.

**Plugin dependency**:
A requirement that another plugin complete registration and any optional startup first.

**Registrar**:
A plugin-scoped capability used only during Registration to declare owned contracts and initial resources.

**Registration**:
The lifecycle phase in which a plugin declares the contracts and initial resources it provides.

**Startup**:
The optional lifecycle phase in which a plugin begins operating after all registrations have been finalized.

**Host**:
The single plugin that owns the application's blocking runtime loop.
_Avoid_: System plugin. A System is the ECS's term for a func run over matching Entities, and has nothing to do with the Host.

**Tick source**:
What decides when an update tick is published — the driver's frame clock while running, or an explicit step request while paused. Rendering is not a tick source: a paused engine keeps drawing the last completed frame.

**Event publication**:
One delivery of an event value to its subscribers. Separate publications may execute concurrently.

**Event**:
An immutable value delivered to subscribers. Mutable shared payload belongs in a Resource.

**Subscription dependency**:
A completion-order constraint within one event publication. A dependent subscriber cannot begin until its prerequisites complete successfully.

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
A read-only account of finalized plugin order, contract ownership, subscription dependency graphs, and the lock set each handler ends up holding once its declared dispatches are folded in. It states what composition produced, which no single source file does.

**Headless engine**:
An engine without a Host. It remains running until its context is canceled.

**Shutdown**:
The optional lifecycle phase that stops active plugins in reverse dependency order before the scheduler stops.

## Entities and Components

**Entity**:
An opaque handle to one thing in the simulation. It is comparable, copyable, and usable as a map key. It carries a generation, so a handle to a despawned Entity is detectably stale rather than silently addressing whatever took its place. Its zero value means "no Entity".
_Avoid_: Id, object, actor, game object

**Entities**:
The authority on which Entities exist: it allocates them, tracks their generations, and answers whether one is alive. It knows nothing about which Components an Entity has.
_Avoid_: World, Registry

**Component**:
A plain value an Entity either has or has not, addressed by its Go type. It contains no pointers of any kind, transitively, which is checked when the type is registered. An Entity holds at most one Component of a given type.
_Avoid_: Attribute, property, field

**Tag**:
A Component with no fields. Its presence is the whole of what it says, and its purpose is to narrow a Query. It is not a place to keep a boolean: a fact the Entity carries data about belongs in that data's Component, and no fact is encoded twice.
_Avoid_: Flag, marker, label

**Component set**:
The exact set of Component types one Entity has. It describes an Entity; it is not a structure the engine keeps, and nothing groups Entities by it.
_Avoid_: Archetype, table, signature

**Component registration**:
The Registration-phase declaration that one Component type exists, made once per type by exactly one plugin. It is what makes the type's Store exist, so a type no plugin registered cannot be added, read, or locked.

**Store**:
The engine's holding of every value of one Component type. There is one per registered Component type, and it is the unit a lock is taken on.
_Avoid_: Pool, column, table

**Page**:
A block of a Store's index, allocated only when some Entity in its range has the Component. Nothing observes a Page; it is not a unit of iteration.
_Avoid_: Chunk, block

**Query**:
A struct type whose field types are the Component types one System touches. A field's pointer-ness is its access mode: a pointer field is written and yields the stored value itself, a value field is read and yields a copy. A Query matches every Entity having _at least_ those Component types, which is why it is not a Component set.
_Avoid_: View, archetype. Also Bundle, which stays unspent for a set of Components spawned together.

**Filter**:
A Query field that narrows which Entities match while reading and writing nothing. It is the reason a Tag exists.
_Avoid_: Predicate, matcher

**System**:
A plain Go func that takes Queries and is called once per tick, iterating the Entities they match itself. Its lock set is derived from its signature at registration, and it can touch no Component that signature does not name.
_Avoid_: System plugin, which is the Host

**Structural change**:
Any change to which Entities have which Components — adding or removing a Component, spawning or despawning an Entity — as opposed to a change to a Component's value.

**Spawn**:
Creating an Entity together with a complete set of Components, as one Structural change. Despawn is its inverse.
_Avoid_: Instantiate, Instance, create

## Agent Interface

**Agent**:
An external, LLM-driven client attached to a running engine. It observes through capabilities and reaches the game only through synthetic input.
_Avoid_: Client, user, bot

**Capability**:
A named, described, typed unit of engine functionality a Provider offers to an Agent. It carries its own request and response types and is fixed for the engine lifetime.
_Avoid_: Tool, action, endpoint

**Provider**:
A plugin that offers capabilities. It speaks cog contracts only; it never emits protocol vocabulary.

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
One tick's recorded declarations, rendered while they are still alive. It is not a copy of a queue: no queue outlives the tick that filled it, so a Snapshot is produced inside one and shaped by the request that asked for it. A Capture is the pixels; a Snapshot is what produced them, and the two are meant to name one moment.
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
One render pass a Camera emits, carrying the tag that selects materials for it, its target, and its clears.
_Avoid_: Render step, stage

**Pass tag**:
The name of what a Pass is for, and the key that selects which of a Scene material's entries serves it.
_Avoid_: Queue, light mode

**Scene material**:
The set of graphics materials one recorded thing offers, one per Pass tag. A Pass whose tag it has no entry for does not draw that thing.
_Avoid_: Shader

**Layer mask**:
A selection of which Cameras see a recorded item. Both a Camera and an item carry one, and an empty mask on either side means every layer.
_Avoid_: Render layer, culling group

**Selector**:
The scene and node names that address part of a model file. A Selector that matches nothing addresses nothing and never widens to the whole file.
_Avoid_: Path, query

**Re-rooting**:
The discarding of a selected node's authored world transform, so that the node's subtree is placed by the recording call's own transform instead.

**Residency**:
One model path's position in the load cycle: never asked for, loading, resident, or terminally failed.
_Avoid_: Cache state, load status

**Frame boundary**:
The point between two frames at which every deferred change to persistent scene state becomes visible — residency, unloads, and geometry bakes alike.

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
