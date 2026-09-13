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

**Slot**:
The contract half of a piece of engine functionality: the commands, events, resources and subscription identities other plugins use, with no handler behind any of them.
_Avoid_: Interface, API, contract package

**Extension**:
A plugin that implements a Slot. It handles the Slot's commands, owns its resources and registers its subscriptions, and declares no contract of its own.
_Avoid_: Backend, driver, as the name of the kind

**Bundle**:
A Slot and the one Extension that implements it, shipped together, so that Extension cannot be replaced by another. It is self-contained: it needs nothing supplied from outside to work, though it may contribute Adapters to a Port that collects them. Most engine functionality is a Bundle.
_Avoid_: Module, which is a shader module or a Go module; package; feature

**Open slot**:
A Slot shipped without an Extension, so that whichever Extension an engine is composed with fills it. It declares no Resources, because a Resource needs an owner the Slot cannot name.
_Avoid_: Abstract bundle, interface

**Port**:
A plugin that ships its own contract and implementation, as a Bundle does, but works only once it is given an Adapter for an interface it declares. It declares whether it requires exactly one Adapter or collects any number. It is how the platform varies beneath a piece of engine functionality without that functionality being replaced: the renderer and storage stay, and what draws for them and what persists for them changes.
_Avoid_: Bundle, which needs nothing supplied; Open slot, whose whole implementation is replaced

**Adapter**:
An implementation of a Port's interface, contributed by a plugin and bound to that Port by the engine during composition. It is a plain value rather than a Resource, so reaching it takes no lock. A window driver's GPU backend is an Adapter of the renderer; each plugin's Provider is an Adapter of the Broker.
_Avoid_: Backend, driver, as the name of the kind

**Vocabulary package**:
The package beneath a Port that declares the contract its Adapters implement and every ID, format and enum the Port's recording API and those Adapters both speak. It imports nothing but Libraries, and the Port's root aliases none of it, so each of its types has one name. An Adapter author reads only it; a recorder imports it beside the Port's root. gfx's is `gpu`.
_Avoid_: Types package, common, shared; Slot, which is a Plugin's whole contract rather than the half an Adapter implements

**Library**:
Code that is not a plugin and defines none, importing only other Libraries and the kernel.
_Avoid_: Package, which is every Go directory; util, common

**Registrar**:
A plugin-scoped capability used only during Registration to declare owned contracts and initial resources, and the Adapters the plugin requires, collects or contributes.

**Registration**:
The lifecycle phase in which a plugin declares the contracts and initial resources it provides and the Adapters it requires, collects or contributes. Adapters are bound once every plugin has registered, before Startup.

**Startup**:
The optional lifecycle phase in which a plugin begins operating after all registrations have been finalized.

**Host**:
The single plugin that owns the application's blocking runtime loop.
_Avoid_: System plugin. A System is the ECS's term for a func run over matching Entities, and has nothing to do with the Host.

**Tick source**:
What decides when an update tick is published — the driver's frame clock while running, or an explicit step request while paused. Rendering is not a tick source: a paused engine keeps drawing the last completed frame.

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
A fixed-length run of values a Component may hold, and the only way a Component holds variable-length data at all. Its backing array is unexported and its constructors copy, so reading one hands out no way to write the Store; its one element mutator is legal under a write lock and is checked in a validating build. Its length is fixed at construction, because growing means allocating, so a List whose length changes is a new List written into the Component. Its elements may hold Lists of their own, and those are checked the same way.
_Avoid_: Slice, array, vector, buffer. A bare slice in a Component is refused, and the word for the fixed-size Go array a Component may also hold is just an array.

**Blob**:
A run of bytes treated as static: once the value holding it is built, nothing writes the bytes again. It is how an engine value carrying pixels, a buffer's contents or a parameter's raw layout says so, and the one slice a Component may hold outright. The ECS admits it on that contract rather than on a property it can check, and Validation mode cannot see a write through one.
_Avoid_: Buffer, which is a GPU object; bytes, for a run that is still being written

**Maybe**:
A value that may be absent, held inline with no pointer. Its zero value is absent, so an optional field nobody wrote reads as unset, and a present zero stays distinct from it. It is what an optional field of a storable value uses where a pointer would make the value mutable indirection.
_Avoid_: Option, nullable, pointer-to-mean-optional

**Tag**:
A Component with no fields. Its presence is the whole of what it says, and its purpose is to narrow a Query. It is not a place to keep a boolean: a fact the Entity carries data about belongs in that data's Component, and no fact is encoded twice.
_Avoid_: Flag, marker, label

**Component set**:
The exact set of Component types one Entity has. A Spawn names the one a new Entity starts with as a struct type, the way a Query is, whose value carries the Components themselves; from then on the Entity may gain and lose Components and the struct type means nothing. It is not a structure the engine keeps, and nothing groups Entities by it.
_Avoid_: Archetype, table, signature; Bundle, which is a Slot shipped with its Extension; Prefab and Template, both still unspent

**Component registration**:
The Registration-phase declaration that one Component type exists, made once per type by exactly one plugin. It is what makes the type's Store exist, so a type no plugin registered cannot be added, read, or locked.

**Store**:
The engine's holding of every value of one Component type. There is one per registered Component type, and it is the unit a lock is taken on. It knows how many Entities it holds, and that number is what a Query consults to choose its Driver.
_Avoid_: Pool, column, table. Also Page and Chunk, both of which stay unspent: a Store's index is not divided into blocks, and nothing groups its rows. Chunk is held in reserve for a run of rows sharing one change version, which is the only thing a real block would buy.

**Query**:
A struct type whose field types are the Component types one System touches. A field's pointer-ness is its access mode: a pointer field is written and yields the stored value itself, a value field is read and yields a copy. A Query matches every Entity having _at least_ those Component types, which is why it is not a Component set. It selects on presence and on nothing else: no Query narrows by what a Component _contains_, so finding every Entity whose Reference points somewhere in particular is a comparison the System makes itself, once per candidate.
_Avoid_: View, archetype

**Filter**:
A Query field that narrows which Entities match without yielding anything into the Query. It still reads its Component's Store, because presence is information and reading it is a read, so it contributes to the System's lock set like any other field. It is the reason a Tag exists.
_Avoid_: Predicate, matcher

**Driver**:
The one Store a Query walks to find candidates, every other Component it names being checked against each candidate in turn. A Query costs what its Driver is long, not what it matches, so narrowing a Query with a Tag can be the difference between visiting a hundred Entities and five thousand. A Filter can never be the Driver: it names the Entities to exclude, and nothing lists the rest.
_Avoid_: lead, primary, base

**System**:
A plain Go func called once per tick, which iterates the Entities its Queries match itself. It takes whatever a cog handler may take — Queries for its Components, Accessors for Entities it did not iterate to, the handles for Spawn and Despawn, read or write access to the Resources of any plugin it binds to, and the Kernel — and its lock set is the union of all of them, derived from the signature at registration. It can touch nothing that signature does not name. Naming the event that drove the tick is legal but is not the ordinary shape, because a System that names one can only ever be subscribed to that one; what it needs from the tick reaches it as a plain value instead.
_Avoid_: System plugin, which is the Host

**Structural change**:
Any change to which Entities have which Components — adding or removing a Component, spawning or despawning an Entity — as opposed to a change to a Component's value. A System may make one to the Entity it is currently visiting; changing whether some _other_ Entity is in the Store being iterated is undefined, and so is using any pointer into a Store after that Store has structurally changed.

**Spawn**:
Creating an Entity with a given Component set and its values, as one Structural change. Despawn is its inverse and is total: it removes the Entity from every Store, so nothing anywhere still holds it.
_Avoid_: Instantiate, Instance, create

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
A build tag that compiles in the check that nobody writes a List through a read. It is on under `-tags ecs_validate` and absent otherwise, so a release build carries no branch and no table for it. It is detection rather than prevention, and its coverage is whatever a run executes — which is a weaker guarantee than the rest of the design offers and is the price of a Component holding mutable data at all. It does not see a write through a Blob, which has no mutator to check.
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
The set of graphics materials one recorded thing offers, one per Pass tag. A Pass whose tag it has no entry for does not draw that thing. A recording call copies it — its entries and each entry's parameters, but not their Blobs — so the caller may change it the moment the call returns; two equal ones batch together however each was built, because a Scene material is keyed by content.
_Avoid_: Shader

**Transform**:
Where one recorded thing stands: a position, a rotation and a per-axis scale, and nothing else. Its zero value is the identity, and so is an all-zero scale; a scale with only some axes zero is taken literally, which is what makes a flattened scale expressible. A non-uniform scale sends that thing's normals through the inverse-transpose and costs nothing to anything else.
_Avoid_: Matrix, model matrix. A Transform has no matrix to override it; a model's flattened node world is Scene's own business.

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

## Audio

**Clip**:
A sound a Voice plays, named by its path or by a Blob of encoded bytes the caller holds. A caller never loads, holds or releases one: naming it is the whole relationship, so two plays of one path, or of one Blob, play the same Clip — and two Blobs with equal bytes are still two Clips.
_Avoid_: Sound, sample, asset, audio file. Also Buffer, which is a GPU object.

**Voice**:
One playing instance of a Clip, begun by a play and addressed afterwards by what that play handed back. Any number may play one Clip at once, and a Voice that has ended is addressed by nothing.
_Avoid_: Sound, source, channel, instance

**Bus**:
A group of Voices the game declares, sharing one volume. Every Bus sits directly under Master, Buses do not nest, and a Voice that names none plays on Master.
_Avoid_: Channel, group, mixer track, category

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
An Entity that physics moves or collides with: it has a position on the plane, and a Shape or a velocity. Every Body is exactly one of the three kinds below, and which one is said by the Components it has rather than by a flag. A Body stands on a 2D plane; placing it in 3D is a Transform's job, written by whoever draws it.
_Avoid_: Collider, rigid body (nothing rotates), physics object, actor

**Static body**:
A Body that never moves and is never pushed, and whose Shape and position never change in place: moving one means replacing the Entity. It pushes Dynamic bodies.
_Avoid_: Wall, level geometry, as the name of the kind

**Kinematic body**:
A Body the game moves by setting its velocity. It pushes Dynamic bodies and nothing pushes it, which is what infinite mass means here.
_Avoid_: Immobile, as a kind of its own

**Dynamic body**:
A Body with a mass and a Drag, moved by the forces on it and pushed by what it touches.

**Shape**:
The one region a Body occupies: a circle, an axis-aligned box, or a segment. A point is a circle of radius 0. A Body has at most one.
_Avoid_: Collider, fixture, hull

**Contact force**:
The force contact response writes into a Dynamic body and integration consumes, rebuilt from nothing every sub-step rather than summed across a tick.
_Avoid_: Contact accumulator, penetration spring, as names for what a Body carries

**Drag**:
How fast a Dynamic body's velocity decays, as a rate per second. It is what makes a pushed thing stop, where friction would elsewhere.
_Avoid_: Damping, friction

**Sweep**:
Moving a circle, possibly of radius 0, in a straight line from one position to another, and finding what it touches on the way. Line of sight is one use of a Sweep, not another operation.
_Avoid_: Ray cast, shape cast, segment query, trace

**Hit**:
What a Sweep reports about one thing it touched: the Entity, how far along the Sweep, where, and the surface normal.
_Avoid_: Contact (that is between Bodies, and is what response acts on), intersection

**Overlap**:
Asking which Entities a Shape at a position touches. It counts as touching exactly what contact would.
_Avoid_: Shape query, area query

**Static index**:
The plugin's index over Static bodies, asked when only what stands still matters.
_Avoid_: Space, world, broadphase

**Body index**:
The plugin's index over every Body with a Shape that is not static. Which index a query asks is the caller's choice.
_Avoid_: Space, world, broadphase
