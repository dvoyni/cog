# otosound

`github.com/dvoyni/cog/plugins/otosound` is the backend that gives
[sfx](../sfx) sound on desktop: a plugin binding sfx's socket over
`ebitengine/oto/v3`, in pure Go, with a software mixer of its own. It builds
for every target but js.

Status: being built: the plugin and its `Config`, the device opened at sfx's
`Start` with this kernel's player on the process's oto context, playing
silence, the `Device` ready once it plays, and the player closed in
`Destroy`; the handoff ring, each flush's changes published to it by `Emit`
and drained whole at a block start, a full ring merging into the staging
changes, and a destroyed clip held until the mixer has passed it; and
`Prepare`'s decoded tier, a clip within `DecodedClipLimit` decoded once on a
goroutine, with its loop region, and installed. A clip over the limit fails
its prepare until streaming is built. The rate conversion, the mixer's
voices, streaming, and the device's probe, retry and reports are designed,
not yet built. The design is in
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
  which sfx calls from its `Start`, sizes the handoff ring and then opens the
  device and starts this kernel's player; its `Emit` publishes each flush's
  changes to the mixer, never waiting on it; its `Device` answers the device
  as it is now, with one atomic load; its `Prepare` decodes a clip on a
  goroutine of its own, its completion drained by `TakePrepared` and its ID
  minted by `Install`.
