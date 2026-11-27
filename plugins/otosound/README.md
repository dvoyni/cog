# otosound

`github.com/dvoyni/cog/plugins/otosound` is the backend that gives
[sfx](../sfx) sound on desktop: a plugin binding sfx's socket over
`ebitengine/oto/v3` v3.5.1, in pure Go, with a software mixer of its own. It
is its own Go module (Go 1.27), importing [kernel](../../kernel), sfx,
[libs/m](../../libs/m), oto, `golang.org/x/sys` for the Windows probe, and
`jfreymuth/oggvorbis`, which the repository's `go.work` replaces with cog's
forks of the decoder. A module outside the repository repeats `go.work`'s two
`replace` lines, since Go ignores `replace` in a dependency.

## Features

- **Everything sfx's seam asks of a backend.** Each flush's changes reach the
  mixer whole and in order, none lost however many flushes land between two
  blocks; every change and every stop is declicked over a block; a paused
  voice resumes on the sample it stopped on; a looping voice wraps at its
  clip's loop region with no gap.
- **Short clips decoded once, long ones streamed.** A clip within
  `DecodedClipLimit` is decoded in `Prepare`, off the flush, and shared by
  every voice on it; a longer one keeps its bytes, and each voice on it
  decodes ahead on a goroutine of its own.
- **Converted without aliasing.** A clip at another rate than the device's is
  converted once, with a windowed sinc, in `Prepare` or a streamed voice's
  read-ahead; a voice's rate is a linear interpolation in the mixer.
- **A device thread that never stalls.** The mixer allocates, locks, frees and
  decodes nothing, and calls into neither sfx nor the kernel.
- **Never stopped by a device.** A game starts and runs with no sound card; a
  device that could not be opened is said once, as a warning, and a lost one
  never.

## Shape

- **`Plugin`**, from `NewPlugin()`, is otosound. **`PluginName`**,
  `"otosound"`, is its name in the kernel's config, the name its `Device`
  answers, and the application name it gives oto.
- **`Config`** is its part of the kernel's config, below.
- **`ErrDeviceUnavailable`**, **`ErrDeviceConfigIgnored`**,
  **`ErrNotOggVorbis`**, **`ErrNoStreamFormat`** and **`ErrNoStreamLength`**
  are its reports, under *Reports*.

## Config

`otosound.PluginName: &otosound.Config{...}`, read in `Init`. Each zero field
means its default. A value out of its range is a composition error, and `Init`
panics naming the field; it panics too on a config that is not a `*Config`.

| Field | Zero means | What it sets | Panics when |
|---|---|---|---|
| `SampleRate` | 48000 | the rate the mixer produces and every clip is converted to | negative |
| `BufferSize` | 10 ms | how much audio the device buffers, and so how long a block, and a declick ramp, is | negative |
| `DecodedClipLimit` | 512 KiB | the largest decoded size, in bytes, at which a clip is still decoded once, in `Prepare`; a larger one streams. `-1` is no limit, always decoding; `-2` always streams | below -2 |
| `RingDepth` | 8 | how many flushes' changes wait for the mixer; past it, each flush merges into the changes the mixer has yet to take, never blocking or dropping one | negative |
| `ReadAhead` | 200 ms | how much audio a streamed voice decodes ahead | negative |
| `RefillWait` | 5 ms | how long a streamed voice's read-ahead sleeps while its ring is full | negative |
| `DeviceRetry` | 1 s | how often a device that could not be taken is retried, and an open one polled for loss | negative |

`SampleRate` and `BufferSize` are the oto context's, which is per process (see
*Several kernels*). A clip's decoded
size is its frames × channels × 4 bytes, so the default limit holds about
1.37 s of stereo at 48 kHz.

## Kernel surface

- **Resources it installs:** none.
- **Resources it reads from other plugins:** none.
- **Tasks it offers:** none, so it has no `Tasks` resource.
- **Events it publishes:** none.
- **Events it subscribes to:** none, so it has no `Handlers` resource.
- **Sockets it declares:** none.
- **Sockets it binds:** `sfx.BackendSocket`, exclusive and not optional, with
  a private backend over the plugin, never the plugin itself: its `Voices`,
  which sfx calls from its `Start`, sizes the handoff ring and the mixer's
  voice table and then takes the device; its `Emit` publishes each flush's
  changes to the mixer, never waiting on it; its `Device` answers the device
  as it is now, with one atomic load; its `Prepare` decodes a clip, or keeps
  a long one's bytes to stream, on a goroutine of its own, its completion
  drained by `TakePrepared` and its ID minted by `Install`.

otosound is listed beside sfx, in either order, and names no dependencies:
its `Init` reads no other plugin's resource, and the binding resolves after
every `Init`. Nothing of it is pinned to the main thread.

## Runs on

Windows (WASAPI, falling back to WinMM), macOS (AudioToolbox, opened at run
time) and Linux (PulseAudio, falling back to ALSA's `libasound`, loaded at run
time), every one at `CGO_ENABLED=0`. Its files build for every target but js,
so a web build composing otosound fails to compile rather than ship a mixer
that starves. The output is stereo float32, always.

## The device

- **It opens when sfx starts**, at `Voices`, never blocking or failing the
  game. sfx's `DeviceView` answers it not ready, named `"otosound"`, until
  this kernel's player plays, and then ready, at the rate and the buffer in
  force, two channels, its `Latency` the context's buffer. `Prepare`,
  `Install` and `Emit` work either way.
- **A machine with no output device is asked about on Windows**, before the
  open: no WinMM device and no default render endpoint. Such a machine is
  reported, never answered ready, and its open is withheld, so a device
  plugged in later is heard. macOS and Linux need no probe, since an open
  there fails when there is no device.
- **A device that could not be opened** is logged once, as a warning, and
  otosound behaves as the headless backend does: it accepts everything, plays
  nothing, and the game runs, its voices advancing and ending on schedule. The
  open is retried every `DeviceRetry`.
- **A lost device is never reported.** An open device is polled every
  `DeviceRetry`; on an error its `Device` goes not ready, which is the whole of
  the notice, its player is stopped, and the open is retried as often.
- **What the retry recovers is what oto allows.** It recovers a Windows
  machine that had no device at start, and oto rides out a Windows device
  switch by itself, never seen as a loss. An oto context that has recorded an
  error stays failed until the process restarts, the game playing on in
  silence, `Ready` false: on Windows a while with no usable device, on Linux
  a PulseAudio server restart or an ALSA write error it cannot recover, on
  macOS a failed queue rebuild, and on macOS and Linux any first open that
  failed, so a device plugged in after the game started is never heard there.
  The design's *Loss, the retry, and what it can recover* gives each, on the
  oto release otosound requires.
- **On recovery, every live voice restarts** at its playhead: sfx restates
  each as a start when `Ready` turns true, and a streamed voice's read-ahead
  reopens at that offset.
- **A ring nobody drains holds nothing.** While no player pulls the mixer,
  before the first plays or after a loss, nothing is audible, so `Emit` hands
  the mixer nothing, lets every released clip go at once, and halts every
  streamed voice's read-ahead, opening none: a silent run holds no clip its
  game has released, and decodes nothing ahead.
- **A pause is the voices'**, sfx's `Queue.SetPaused`: the device stays open
  and is fed silence, never suspended.

## Several kernels

oto has one context a process, which is never closed. Each kernel's otosound
takes its own player on it, so every kernel is audible, and `Destroy` closes
only its own. The first kernel's `SampleRate` and `BufferSize` are in force
for the life of the process, after that kernel is destroyed too: a later
kernel asking for others is logged once, as an error, `ErrDeviceConfigIgnored`
carrying what it asked for and what is in force, and its `Device` answers
those in force.

## Reports

otosound reports through the kernel's Logger, from `Voices` or its device
goroutine, which `Destroy` stops before it returns.

| What otosound learns | How it is logged |
|---|---|
| No device could be opened: the open was refused, or the Windows probe found no output device | a warning, `LogWarnOnce`, of `ErrDeviceUnavailable` wrapping the cause |
| A later kernel's `SampleRate` or `BufferSize` is ignored | an error, `LogErrOnce`, of `ErrDeviceConfigIgnored`, on which the kernel's default `StdLogger` panics |
| A device that was ready is lost | never |

A clip's failures cross the seam to sfx, which logs each once under the
clip's path, wrapped in `sfx.ErrClipFailed`, and ends its voices with
`sfx.ReasonFailed`:

- **`ErrNotOggVorbis`:** headers that will not parse, or a stream that fails
  to decode. Ogg Vorbis is the only format.
- **`ErrNoStreamFormat`:** no rate, no channels, or more than two.
- **`ErrNoStreamLength`:** a stream with no frames, one whose length reads
  zero among them: such a clip is refused rather than streamed.

A clip's loop region is read from its tags by sfx's `loopregion`, as sfx's
[guide](../sfx/docs/README.md) says, a malformed one answered as sfx's
`ErrLoopRegionIgnored`. A streamed voice whose decoder fails after its clip
was prepared ends in silence, reported nothing.

See [docs/design.md](docs/design.md) for the design and why it is this way,
and sfx's [CONTEXT.md](../sfx/CONTEXT.md) for its words.
