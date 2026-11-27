# Sfx

Sfx is sound as a kernel sees it: clips played as voices, grouped on buses, heard from a listener, with all of the arithmetic computed in sfx and the mixing done by a backend plugin beneath it. It knows nothing of rendering, cameras or the ECS. This glossary records its domain language.

## Language

### The device

**Backend**:
What implements sfx over a real or silent device: the otosound plugin, or the headless backend, which a test or an application binds. It is handed what to mix and computes none of it.
_Avoid_: Adapter, driver, implementation

**Headless backend**:
The backend that plays nothing, for tests and for runs no one hears. It reads each clip's length so its voices still end when they would have, and it reports a device that is ready.
_Avoid_: Null backend, mock, nosound

**Device**:
What makes sound audible: a sound card. There is one per kernel, and a game neither opens nor chooses it; it only reads whether there is one. A device may be absent, not ready yet, or lost, and a game sees one thing in all three. Voices play on regardless: a voice's playhead advances whether or not anyone can hear it.
_Avoid_: Output, speaker, sink. Also backend, which is the plugin beneath sfx rather than the thing it found.

**Mixer**:
What turns voices into the samples a device consumes. It runs outside the kernel, on the device's own clock rather than any beat, and nothing that takes a lock reaches it, which is why a game never addresses one and why everything it is told arrives as one flush at once.
_Avoid_: Audio thread, callback, engine. Also bus, which is a grouping a game declares rather than the thing that does the mixing.

**Flush**:
Sfx applying, at once and in the order they were recorded, every operation recorded since the last flush, and handing the backend the result. An operation is never lost, and a parameter set twice between flushes keeps its last value.
_Avoid_: Commit, update, submit

### Clips

**Clip**:
A sound a voice plays, read by its path through the application's filesystem and kept until it is released. Its path is what finds it again, so two plays of one path play the same clip. Releasing is optional. Releasing a clip stops every voice playing it, which is why a game that wants a sound to finish simply does not release its clip.
_Avoid_: Sound, sample, audio file. Also buffer, which is a GPU object.

**Prepared clip**:
A clip turned into something a voice can be started from. What that is depends on the clip: a short one becomes samples every voice playing it shares, a long one stays as its bytes and a way to read them while it plays. Which one is the backend's own business: a game never sees a prepared clip and cannot tell which kind it got.
_Avoid_: Decoded clip, PCM, stream. Also buffer, which is a GPU object.

**Loop region**:
The span of a clip a looping voice repeats between. The clip declares it and the game never states one, so a voice told to loop repeats the way its clip says to; a clip that declares none repeats whole. It is why an intro can run into a loop at all, and why a loop has no gap where a track chained on an ending does.
_Avoid_: Loop point, marker, cue. Also seek, which is a game moving a voice rather than a clip describing itself.

### Voices and buses

**Voice**:
One playing instance of a clip, begun by a play and addressed afterwards by what that play handed back. Any number may play one clip at once, and a voice that has ended is addressed by nothing.
_Avoid_: Sound, source, channel, instance

**Stealing**:
What ends a voice to make room when every slot is taken. The voice that loses is the least audible one (its own volume through its bus, its falloff and its cone) unless a priority puts it out of reach, and the play that arrives is as stealable as anything already playing. A game hears about it through the same ending that tells it about a stop. A steal is final: nothing resumes a stolen voice, and a game that wants the sound back plays it again as a new voice.
_Avoid_: Voice limit, culling, eviction. Also ducking, which is a game lowering a bus on purpose.

**Bus**:
A group of voices the game declares, sharing one volume. Every bus sits directly under master, buses do not nest, and a voice that names none plays on master.
_Avoid_: Channel, group, mixer track, category

**Pause**:
Every voice suspended at once, by the game, each resuming on the same sample it stopped on. A voice played during a pause starts suspended. It is the game's to say, as there is no pause of the engine's for sfx to follow.
_Avoid_: Mute, which is a volume; suspend-all; engine pause

**Fade**:
A change of volume over time that a game drives itself, by changing a voice or a bus on each beat. Sfx has no word for it and no verb that performs one.
_Avoid_: Ramp, crossfade, tween, as names for anything sfx does

### Positional audio

**Listener**:
The place and facing a positional voice is heard from. There is one per kernel, and the game puts it where it wants: sfx never looks at a camera, so a listener follows one only because something copies it across.
_Avoid_: Camera, ear, microphone

**Positional voice**:
A voice that has been given a position, and so is quieter with distance from the listener and heard from its side. A voice without one is heard from nowhere in particular, which is what a UI click is. The first position makes a voice positional for the rest of its life.
_Avoid_: 3D sound, spatial sound, as though 2D were a different kind

**Falloff**:
How a positional voice grows quieter with distance from the listener. Each voice carries its own.
_Avoid_: Attenuation curve, rolloff, as the name of the whole

**Cone**:
The directions a positional voice is loud in, when it has a facing as well as a position. Without a facing a voice is loud in every direction, whatever its cone says.
_Avoid_: Directivity, beam
