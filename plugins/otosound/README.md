# otosound

`github.com/dvoyni/cog/plugins/otosound` is the backend that gives
[sfx](../sfx) sound on desktop: a plugin binding sfx's socket over
`ebitengine/oto/v3`, in pure Go, with a software mixer of its own. It builds
for every target but js.

Status: being built: the plugin and its `Config`, the device opened at sfx's
`Start` with this kernel's player on the process's oto context, the `Device`
ready once it plays, and the player closed in `Destroy`; the handoff ring,
each flush's changes published to it by `Emit` and drained whole at a block
start, a full ring merging into the staging changes, and a destroyed clip
held until the mixer has passed it; `Prepare`'s decoded tier, a clip within
`DecodedClipLimit` decoded once on a goroutine, with its loop region,
converted there to the device's rate with a windowed sinc while still
answering its own, and installed; and the mixer, mixing every resident voice
through its gain matrix at its rate, each change and each stop declicked
over a block, a paused voice holding its playhead, and a looping voice
wrapping gaplessly at its loop region; and streaming, a clip over the
limit keeping its bytes, each voice on it decoding and converting ahead into
a read-ahead ring of its own on a goroutine, playing from its offset once
its ring primes, looping gaplessly, and halted with its voice and in
`Destroy`. The device's probe, retry and reports are designed, not yet
built. The design is in
[`docs/design.md`](docs/design.md), and the build on the
[sfx map](https://github.com/dvoyni/cog/issues/945).

## Shape

- **`otosound.NewPlugin()`**, listed beside sfx, in either order.
- **`otosound.Config`:** `SampleRate`, `BufferSize`, `DecodedClipLimit`,
  `RingDepth`, `ReadAhead`, `RefillWait` and `DeviceRetry`, each zero meaning
  its stated default. A negative value, or a `DecodedClipLimit` below -2,
  panics in `Init`. `SampleRate` and `BufferSize` are the oto context's,
  which is per process: only the first kernel's are in force.

## Kernel surface

- **Resources it installs:** none.
- **Resources it reads from other plugins:** none.
- **Events it publishes:** none.
- **Events it subscribes to:** none, so it has no `Handlers` resource.
- **Tasks it offers:** none, so it has no `Tasks` resource.
- **Sockets it declares:** none.
- **Sockets it binds:** `sfx.BackendSocket`, exclusive and not optional, with
  a private backend over the plugin, never the plugin itself: its `Voices`,
  which sfx calls from its `Start`, sizes the handoff ring and the mixer's
  voice table and then opens the device and starts this kernel's player; its `Emit` publishes each flush's
  changes to the mixer, never waiting on it; its `Device` answers the device
  as it is now, with one atomic load; its `Prepare` decodes a clip on a
  goroutine of its own and converts it to the device's rate there, or keeps
  a long one's bytes to stream, its completion drained by `TakePrepared` and
  its ID minted by `Install`.
