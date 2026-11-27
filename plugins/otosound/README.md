# otosound

`github.com/dvoyni/cog/plugins/otosound` is the backend that gives
[sfx](../sfx) sound on desktop: a plugin binding sfx's socket over
`ebitengine/oto/v3`, in pure Go, with a software mixer of its own. It builds
for every target but js.

Status: being built: the plugin and its `Config`, the device opened at sfx's
`Start` with this kernel's player on the process's oto context, playing
silence, the `Device` ready once it plays, and the player closed in
`Destroy`; and the handoff ring, each flush's changes published to it by
`Emit` and drained whole at a block start, a full ring merging into the
staging changes, and a destroyed clip held until the mixer has passed it;
and the device's probe, retry and reports: on Windows a machine with no
output device is asked about before the open, a device that could not be
taken is logged once as a warning and retried every `DeviceRetry`, an open
device is polled for loss as often, a lost one is reported never, and a
later kernel's ignored `SampleRate` or `BufferSize` is logged once as an
error. Decoding, the mixer's voices and streaming are designed, not yet
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
  which sfx calls from its `Start`, sizes the handoff ring and then takes the
  device and starts this kernel's player on its device goroutine; its `Emit` publishes each flush's
  changes to the mixer, never waiting on it; its `Device` answers the device as it is now, with one atomic
  load.
