# oto v3 and our Vorbis forks today

Answers [#947](https://github.com/dvoyni/cog/issues/947) on the map [#945](https://github.com/dvoyni/cog/issues/945). Gathered 2026-11-27. Facts, with what each one means for carrying v1's otosound over; no design.

v1's spike ran against oto v3.5.0 on 2026-09-12, on Windows (WASAPI) and Chrome; its findings are in [#294](https://github.com/dvoyni/cog/issues/294). v1's own reading of oto's Windows recovery is in [#485](https://github.com/dvoyni/cog/issues/485). The calls v1 makes are in `_legacy/extensions/otosound/internal/otoaudio.go`, `clipdata.go`, `stream.go` and `_legacy/extensions/nosound/internal/backend.go`, `loopregion.go`.

## Sources and versions

| Source | Version read |
|---|---|
| oto, latest release | [`v3.5.1`](https://github.com/ebitengine/oto/tree/v3.5.1) (commit `2d91d09`, 2026-09-12; [release](https://github.com/ebitengine/oto/releases/tag/v3.5.1) published 2026-09-20); [pkg.go.dev](https://pkg.go.dev/github.com/ebitengine/oto/v3@v3.5.1) |
| oto, unreleased | `main` at [`0ec3dae`](https://github.com/ebitengine/oto/tree/0ec3daeb5af8fc4d30ce5602d04ce4ab2f75fe1d) (2026-11-14), 45 commits past v3.5.1; tags `v3.6.0-alpha` (`fef5b79`) and `v3.6.0-alpha.1` (`d169121`) |
| oto issues | [open issues](https://github.com/ebitengine/oto/issues), read 2026-11-27 |
| jfreymuth/pulse | `v0.1.3`, the version oto requires; [open issues](https://github.com/jfreymuth/pulse/issues) |
| dvoyni/vorbis | `cog` branch at [`ab3e6ce`](https://github.com/dvoyni/vorbis/commit/ab3e6ce988a8), the pin in `_legacy/go.mod` |
| dvoyni/oggvorbis | `cog` branch; pin [`546236b`](https://github.com/dvoyni/oggvorbis/commit/546236badfd4) in `_legacy/go.mod`, branch head `29b20f4` |
| jfreymuth/vorbis | `master` at [`8e67b20`](https://github.com/jfreymuth/vorbis/commit/8e67b20) (2026-03-30), latest tag `v1.0.2` |
| jfreymuth/oggvorbis | `master` at [`3110185`](https://github.com/jfreymuth/oggvorbis/commit/3110185) (2026-11-22), latest tag `v1.0.6` |
| Go | `go 1.27` in cog's `go.work`; toolchain `go1.27.4 windows/amd64` |

`O:` below means `https://github.com/ebitengine/oto/blob/v3.5.1/` and `M:` means `https://github.com/ebitengine/oto/blob/0ec3daeb5af8fc4d30ce5602d04ce4ab2f75fe1d/`.

## Answer in short

- **oto's current release is v3.5.1**, a patch over v1's v3.5.0: it fixes `FormatUnsignedInt8` conversion ([#298](https://github.com/ebitengine/oto/issues/298)) and nothing else. Its public API is v3.5.0's. The module proxy's `@latest` is v3.5.1. Everything since is on `main`, unreleased, heading for v3.6.0.
- **`NewPlayer` over an `io.Reader` with `FormatFloat32LE` still works, and there is still one context per process.** Every call v1's otosound makes compiles against v3.5.1 and `main`, and a probe run on this machine opened a context, pulled 10 ms reads and was refused a second context on both.
- **It still builds with `CGO_ENABLED=0`** for `windows/amd64`, `darwin/arm64` and `linux/amd64`, both v3.5.1 and `main`.
- **No open issue breaks otosound as v1 uses oto on macOS or Linux.** The open macOS and PulseAudio issues are about `Context.Suspend`, which v1 never calls. Two things were never checked by v1 and are open risks: v1's 10 ms default buffer was measured only on Windows, and on macOS it gives AudioQueue buffers an eighth the size oto's own comment calls necessary. And once oto's context records an error it stays failed for the life of the process on every platform, so part of v1's "retry and recover" path can never succeed.
- **The forks still build on Go 1.27** and decode with every call v1 makes. Upstream will not take our changes: jfreymuth declined to review AI-generated code, archived `jfreymuth/vorbis`, moved the decoder into `jfreymuth/oggvorbis/vorbis` in v1.0.6, and the oggvorbis README now refuses LLM-generated contributions. The `replace` pair keeps working as it is, and it is now permanent.

## 1. oto since v3.5.0

### v3.5.1, the release

[`v3.5.0...v3.5.1`](https://github.com/ebitengine/oto/compare/v3.5.0...v3.5.1) is two commits: Go 1.27 added to CI, and `oto: fix unsigned 8-bit sample conversion` (`internal/mux/mux.go`, one line, and its test). The release note says only that. otosound plays `FormatFloat32LE`, so the fix doesn't touch it. The `go` directive is `1.25.0` in both, and the requirements are unchanged: `purego v0.11.0`, `jfreymuth/pulse v0.1.3`, `golang.org/x/sys v0.47.0`.

### `main`, unreleased

[`v3.5.1...0ec3dae`](https://github.com/ebitengine/oto/compare/v3.5.1...0ec3daeb5af8fc4d30ce5602d04ce4ab2f75fe1d) is 45 commits. The ones that touch what otosound uses or leans on:

| Change | What it does | What it means for otosound |
|---|---|---|
| Option validation (`a3a65e3`, [#303](https://github.com/ebitengine/oto/pull/303)) | `NewContext` returns an error for nil options, a sample rate ≤ 0, a channel count other than 1 or 2, a negative buffer size (`M:context.go:92-104`) | v1 passes 48000, 2 and 10 ms; nothing changes |
| `NewContext` documented as once per process (`919033a`) | "NewContext can be called only once in a process. Any later call returns an error." (`M:context.go:91`). v3.5.1 already returned `oto: context is already created` (`O:context.go:94`) | Behaviour unchanged, now stated |
| Source reads sized to the room left ([#330](https://github.com/ebitengine/oto/pull/330)) | A player's source `Read` asks for `bufferSize - buffered` (`M:internal/mux/mux.go:841`) instead of the whole `bufferSize` (`O:internal/mux/mux.go:578`) | v1's Mixer ramps over one `Read`, and v1's config says the block is the buffer size. On `main` a block is whatever the driver drained. See section 4 |
| `Player.UnplayedSize` ([#307](https://github.com/ebitengine/oto/pull/307)) and played-frame reports ([#332](https://github.com/ebitengine/oto/pull/332), [#333](https://github.com/ebitengine/oto/pull/333), `0ec3dae`) | Bytes read from the source that the hardware hasn't played yet (`M:player.go:82-88`), reported by Android, browsers, PulseAudio, ALSA, macOS and iOS. Windows is a TODO (`M:driver_windows.go:49-51`) with an open PR, [#335](https://github.com/ebitengine/oto/pull/335) | New API v1 never had. It bears on what paces sfx's flush ([#948](https://github.com/dvoyni/cog/issues/948)) only once it is released |
| WASAPI: no default endpoint while polling ([#320](https://github.com/ebitengine/oto/pull/320)) | Treated as a device switch and retried (`M:driver_wasapi_windows.go:193-196`). On v3.5.1 it was joined into the context's error, which is permanent | See section 4 |
| WASAPI: endpoint HRESULT kept ([#324](https://github.com/ebitengine/oto/pull/324)) | `GetDefaultAudioEndpoint` wraps the actual Win32 HRESULT (`M:api_wasapi_windows.go:501`). On v3.5.1 every `FACILITY_WIN32` failure became `E_NOTFOUND` (`O:api_wasapi_windows.go:495`) | v1's Windows probe mirrors v3.5.1's rule. See section 4 |
| WASAPI wait bounded ([#323](https://github.com/ebitengine/oto/pull/323)) | The sample-ready wait times out at 500 ms so device polling keeps running. It was `INFINITE` (`O:driver_wasapi_windows.go:433`) | An endpoint that stops signalling no longer hangs oto's Windows loop |
| WinMM buffer divided ([#327](https://github.com/ebitengine/oto/pull/327)), retries ([#321](https://github.com/ebitengine/oto/pull/321), [#325](https://github.com/ebitengine/oto/pull/325)), lost notification ([#313](https://github.com/ebitengine/oto/pull/313)) | An explicit `BufferSize` was given to each of WinMM's six headers, six times the request. `main` divides it (`M:driver_winmm_windows.go:145`). Pending samples now survive retries | On v3.5.x, otosound's 10 ms becomes 60 ms of headers on the WinMM fallback |
| ALSA rejects a changed rate ([#326](https://github.com/ebitengine/oto/pull/326)) | `snd_pcm_hw_params_set_rate_near` returning another rate is refused (`M:driver_alsa_unix.go:228`). v3.5.1 accepts it, and the mux keeps producing the requested rate | On v3.5.x, Linux without a PulseAudio server, on a card that can't do 48 kHz, plays at the wrong speed |
| darwin: temporary rebuild failures retried ([#329](https://github.com/ebitengine/oto/pull/329)) | After `kAudioQueueErr_QueueInvalidated`, a temporary `AudioQueueNewOutput` failure no longer fails the context for good | On v3.5.x, a media-services restart during a rebuild ends sound for the process |
| Seek outside the lock ([#328](https://github.com/ebitengine/oto/pull/328)), mux fixes ([#296](https://github.com/ebitengine/oto/pull/296), [#299](https://github.com/ebitengine/oto/pull/299), [#304](https://github.com/ebitengine/oto/pull/304), `9bd9c5b`, `96b176e`) | Mostly volume, seek and EOF edge cases | otosound never calls `SetVolume` or `Seek` on its player, and its source never ends |

## 2. The API otosound uses

v1's otosound calls `oto.NewContext(&oto.NewContextOptions{SampleRate, ChannelCount, Format: oto.FormatFloat32LE, BufferSize, ApplicationName})`, waits on the ready channel, then calls `Context.Err`, `Context.NewPlayer(io.Reader)`, `Player.SetBufferSize`, `Player.Play`, `Player.Err` and `Player.PauseAndStopReading` (`_legacy/extensions/otosound/internal/otoaudio.go`). All of them exist with the same signatures in v3.5.1 and `main`. A probe making exactly those calls compiled against both.

The same probe, run on this machine (Windows 11, WASAPI, Go 1.27.4) at 48 kHz stereo float32 with `BufferSize` 10 ms and a 480-frame player buffer, for 3 s:

| | v3.5.1 | `main` `0ec3dae` |
|---|---|---|
| Ready after | 22 ms | 23 ms |
| Second `NewContext` | `oto: context is already created` | `oto: context is already created` |
| Reads of the source | 301, every one 480 frames | 300, every one 480 frames |
| Audio produced | 3.010 s in 2.997 s | 3.000 s in 2.990 s |
| Worst gap between reads | 11.2 ms | 11.4 ms |
| `Player.Err` | nil | nil |

This matches the spike's 10 ms row (mean read 9.97 ms, worst gap 11.26 ms).

**One context per process** holds as v1 found it. On top of that:

- **A context never comes back.** Its error is an `atomicError` that only joins (`O:context.go:176-190`); nothing clears it, and `NewContext` can't be called again. There is no `Context.Close` ([#149](https://github.com/ebitengine/oto/issues/149), open since 2021). Multiple contexts are an open request ([#251](https://github.com/ebitengine/oto/issues/251)).
- **The player must stay reachable**, as the spike found. [#293](https://github.com/ebitengine/oto/issues/293) is closed, and [#315](https://github.com/ebitengine/oto/pull/315) documents it. v1 keeps the player in `otoAudio.player`.
- **oto runs its own threads.** darwin builds its AudioQueue with no run loop on a goroutine that locks an OS thread (`O:driver_darwin.go:49-56`, `:169-170`), and WASAPI calls COM on a thread of its own (`O:driver_wasapi_windows.go`, `comThread`). No call otosound makes has to come from the main thread.
- **Reads are sequential** across a context's players. A blocking `Read` holds up the others ([#322](https://github.com/ebitengine/oto/pull/322)). otosound has one player per kernel.

## 3. Builds without cgo

A module requiring oto and `jfreymuth/oggvorbis` through cog's two `replace` lines, built with `CGO_ENABLED=0` on Go 1.27.4:

| Target | oto v3.5.1 | oto `main` (`v3.6.0-alpha.1.0.20261114154151-0ec3daeb5af8`) |
|---|---|---|
| `windows/amd64` | builds | builds |
| `darwin/arm64` | builds | builds |
| `linux/amd64` | builds | builds |
| `linux/arm64`, `darwin/amd64`, `windows/arm64` | builds | not tried |

The README still lists Windows, macOS and Linux as "no Cgo required". On macOS, oto opens `AudioToolbox.framework` at run time with `purego.Dlopen` (`O:api_darwin.go:87`). On Linux, oto talks to PulseAudio through the pure-Go `jfreymuth/pulse`. When no server is reachable it falls back to ALSA, loading `libasound.so.2` (or `libasound.so`) at run time through purego (`O:driver_alsa_unix.go:76`). Only a FreeBSD build at `CGO_ENABLED=0` needs an extra `-gcflags` (README, *Linux, FreeBSD, OpenBSD*).

## 4. What otosound leans on that `main` changes

v1's code and spec state three things about oto that hold in v3.5.0 and v3.5.1. Moving past v3.5.1 changes two of them, and the third is wrong in every version. [#485](https://github.com/dvoyni/cog/issues/485) asked for a re-check at any bump. This is that re-check, from source.

### The block, and so the declick ramp, is one buffer long

v1's `Config.BufferSize` says it is "how much audio the device buffers, and so how long a block the Mixer produces and how long the declick ramp is". `mixer.Read` ramps over `len(buf)`, whatever that is. On v3.5.x that holds: a source read always asks for the player's whole buffer (`O:internal/mux/mux.go:567-580`), which is also why a player can hold nearly twice its size ([#330](https://github.com/ebitengine/oto/pull/330) measured 127 bytes in a 64-byte buffer).

On `main`, a read asks only for the room left (`M:internal/mux/mux.go:830-844`). On WASAPI that is still the whole buffer, since WASAPI drains a whole period at a time (measured above: every read 480 frames on `main` too). On macOS, oto splits `BufferSize` across four AudioQueue buffers (`O:driver_darwin.go:31`, `:147`), so at 10 ms the mux drains 120 frames at a time. From the source, reads on `main` would then be 120 frames and the ramp 2.5 ms, a quarter of what v1 states. This is inferred from the source and was not measured.

### The Windows probe mirrors oto's "no device" rule

v1's `endpoint_windows.go` treats a `FACILITY_WIN32` failure of `GetDefaultAudioEndpoint` as "no endpoint", because v3.5.0's wrapper turns every such failure into `E_NOTFOUND`, and so into `errDeviceNotFound` and, with WinMM's, into the null context (`O:api_wasapi_windows.go:495`, `O:driver_windows.go:66-67`). v3.5.1 is the same. On `main` only `E_NOTFOUND` itself (`0x80070490`) is "not found" ([#324](https://github.com/ebitengine/oto/pull/324)). The probe's answer is still right, since both halves failing means no driver can start. But its comment that it "agrees with [oto] exactly" would be stale, and some machines it calls absent would get oto's joined error instead of a null context.

### A failed context is failed for the process, so some recoveries never happen

v1's spec retries the open once a second and promises that "on recovery every live Voice resumes", and #485's test plan expects "sound back within a second of re-enabling" after a 15 s outage. v1's retry reaches the same context: `otoAudio.open` returns the one already made, and `play` refuses while `Context.Err` is non-nil. That error is never cleared (section 2). From the source, the cases where oto records an error are:

- **Windows**: after 24 failed re-acquisitions, about 10 s with no usable device (`wasapiRestartRetryLimit`, `O:driver_wasapi_windows.go:138`), or on any error that isn't transient. On v3.5.x the transient list leaves out a poll that finds no default endpoint (`O:driver_wasapi_windows.go:181-200`, `:249`), so one moment without a default endpoint is enough; `main` fixes that ([#320](https://github.com/ebitengine/oto/pull/320)).
- **Linux, PulseAudio**: `pulseContext.Err` returns the stream's error (`O:driver_pulseaudio_unix.go:144-152`), and oto never reconnects, so a server restart ends sound. Default-device changes are the server's to handle and don't reach oto.
- **Linux, ALSA**: a write error that `snd_pcm_recover` can't recover.
- **macOS**: a failed queue rebuild, including a temporary one on v3.5.x ([#329](https://github.com/ebitengine/oto/pull/329)). A start that fails because the session can't be activated is retried, not recorded, as v1's spec already says.
- **Any platform**: a context whose first initialization failed. On macOS and Linux v1 opens without probing, so a machine that gets its device after startup is never heard. On Windows, v1's absent probe keeps the open from happening and so does recover.

So v1's recovery path does what it promises only where no context was made yet (Windows' absent probe) or where oto recovers by itself (a Windows device switch under ~10 s). v1 shipped this way, so carrying it over changes nothing that works today. What is wrong is the promise in its spec and test plan.

## 5. Open issues a game would hit on macOS or Linux

| Issue | Platform | What a game would see | Does v1's otosound hit it? |
|---|---|---|---|
| [#336](https://github.com/ebitengine/oto/issues/336), opened 2026-11-27 | macOS | `Suspend` leaves up to four AudioQueue buffers queued. After a seek or source change made while suspended, the old audio plays at `Resume`. No `Flush` exists | No: v1 pauses inside its Mixer and never calls `Suspend`. It would if [#950](https://github.com/dvoyni/cog/issues/950) chose `Context.Suspend` |
| [#292](https://github.com/ebitengine/oto/issues/292) | Linux (PulseAudio) | `Suspend` doesn't cork the stream, so the sink stays active and draws power. Blocked on [jfreymuth/pulse#52](https://github.com/jfreymuth/pulse/issues/52) | Only through `Suspend`, as above |
| [#229](https://github.com/ebitengine/oto/issues/229) | macOS (reported), all | The context keeps filling the device with zeros when nothing plays, at a steady CPU cost | Yes, always: otosound keeps one player playing for the kernel's life, so it pays this whatever oto does |
| [#237](https://github.com/ebitengine/oto/issues/237) | all | `IsPlaying` turns false before the hardware has played the tail | No: otosound doesn't use `IsPlaying`. `UnplayedSize` on `main` answers it |
| [#54](https://github.com/ebitengine/oto/issues/54) | all | No output device selection. oto plays to the system default | Yes, as in v1, by design |
| [jfreymuth/pulse#53](https://github.com/jfreymuth/pulse/issues/53) | Linux (PulseAudio) | `PlaybackStream.Error` reads a field the library writes from another goroutine, without a lock | A data race: oto's `Err` reads it (`O:driver_pulseaudio_unix.go:148`), and otosound polls `Err`. Only a `-race` run against a real PulseAudio server would report it. v1's tests use a fake device |
| [jfreymuth/pulse#48](https://github.com/jfreymuth/pulse/issues/48) | BSD mostly | No PulseAudio server is started on demand | Not on Linux desktops, where PipeWire or PulseAudio runs per session. Without one, oto falls back to ALSA |

Bugs fixed on `main` but still in v3.5.1, on these platforms: ALSA accepting another sample rate ([#326](https://github.com/ebitengine/oto/pull/326)), and darwin's temporary rebuild failures being fatal ([#329](https://github.com/ebitengine/oto/pull/329)).

**Not an issue, but unmeasured: v1's 10 ms default buffer on macOS and Linux.** The spike and #485 measured and read Windows only, and #485 says the darwin and Linux drivers were "unexamined". No macOS or Linux measurement of otosound is recorded in #294, #485 or the otosound spec. On macOS, 10 ms gives four 960-byte buffers of 120 frames each (`O:driver_darwin.go:147`). oto's default is 12288 bytes per buffer, 1536 frames or 32 ms, 128 ms in all, and its comment says that size "seems necessary at least on iPod touch (7th) and MacBook Pro 2020" (`O:driver_darwin.go:138-143`). v3.5.1's comment says the total is 100 ms. The arithmetic gives 128 ms, which `main`'s comment states. On PulseAudio, 10 ms is passed as the requested latency (`O:driver_pulseaudio_unix.go:74-83`), and the server may grant more.

## 6. The Vorbis forks

### What they are

`_legacy/go.mod` replaces `jfreymuth/vorbis` with `dvoyni/vorbis v1.0.3-0.20260921150454-ab3e6ce988a8` and `jfreymuth/oggvorbis` with `dvoyni/oggvorbis v1.0.6-0.20260921150857-546236badfd4`. Both pins are on the forks' `cog` branches. The oggvorbis branch has moved on to `29b20f4`, but its two newer commits change only `alloc_test.go` and add `reader_alloc_test.go`.

Over upstream `master` (`8e67b20`), the vorbis fork adds the benchmarks, the bump-pointer and codeword-assigning Huffman builder, allocation-free packet decoding, window and IMDCT tables built once per blocksize, and the shared setup cache ([#511](https://github.com/dvoyni/cog/issues/511)'s decisions). The oggvorbis fork, over upstream `94cd36b`, adds the benchmarks, pages read into buffers the reader keeps, and a packet table sized once. Neither adds public API. v1 calls only upstream's: `NewReader`, `Reader.Read`, `SetPosition`, `SampleRate`, `Channels`, `Length`, `GetCommentHeader`, `GetLength`, `ReadAll`.

### They build on Go 1.27

- Built with `CGO_ENABLED=0` for the three targets on Go 1.27.4, in the module of section 3, which requires them through the two `replace` lines.
- `go vet` on both packages compiles their tests. It reports two self-assignments, `m, a = m, m-a`, at `decode.go:183` and `:189`. That code comes from upstream, and upstream's integrated copy has since removed it. `go test`'s vet subset doesn't include that check.
- The probe decoded `_legacy/extensions/otosound/internal/testdata/pianoroll.ogg` (44.1 kHz stereo, 48704 frames) through every call v1 makes, with no error.
- Both forks' `go.mod` say `go 1.15`. The Go 1.27 toolchain builds them at that language version.
- In a workspace, two modules with identical `replace` lines for the forks build. Differing ones fail with `conflicting replacements for github.com/jfreymuth/oggvorbis`, tested in a scratch workspace. So `otosound` and `sfx/headless` carry the same pins, or `go.work` settles them. Modules outside the repository still repeat the `replace` lines, because Go ignores `replace` in a dependency ([#512](https://github.com/dvoyni/cog/issues/512)).

### What upstream changed since we forked

- **jfreymuth/vorbis is archived** (read-only). Its `master` is still `8e67b20`, our base, and `v1.0.2` is its last tag.
- **jfreymuth/oggvorbis moved the decoder into itself.** On 2026-11-22, `759c435` ("Integrate vorbis") copied the decoder into the subpackage `github.com/jfreymuth/oggvorbis/vorbis` and dropped the requirement on `jfreymuth/vorbis` from `go.mod`, and the release was tagged `v1.0.6`. The integrated copy is vorbis `master` plus three changes:
  - floor 0 with an odd order started `q` at `1 / 4`, an integer constant that is 0, and now starts it at `1. / 4`;
  - the two self-assignments above are gone;
  - an IMDCT test tolerance is tightened.
  Floor 0 is "of limited modern use. No known Vorbis encoder past Xiph.Org's own beta 4 makes use of floor 0" ([Vorbis I specification](https://xiph.org/vorbis/doc/Vorbis_I_spec.html), 1.2.4). Our vorbis fork still has `q = 1 / 4` (`floor0.go:70`). No other decoding changed.
- **The oggvorbis README now says** "Please note that LLM-generated contributions will not be accepted" (`52975e7`, 2026-11-21).
- **Our PRs are closed unmerged.** On 2026-11-03 jfreymuth answered the ping on [vorbis#13](https://github.com/jfreymuth/vorbis/pull/13): "unfortunately I currently do not have the bandwidth to review AI-generated code … Feel free to maintain your own branch". On 2026-11-10 dvoyni closed vorbis#13, #14, #16 and #17 and oggvorbis#13 and #14. [vorbis#15](https://github.com/jfreymuth/vorbis/pull/15) is still open, on a repository that can no longer merge it.

### What this means for the forks

- **The `replace` pair works unchanged.** The oggvorbis fork still imports `github.com/jfreymuth/vorbis`, which the vorbis `replace` redirects. The `replace` lines name no version on the left, so they also cover a build where something else requires oggvorbis `v1.0.6`.
- **The pair only works as long as the oggvorbis fork stays on its pre-1.0.6 base.** Rebased onto `v1.0.6`, the fork would decode with its own `vorbis/` subpackage. The vorbis `replace` would then do nothing, without any error, and the decoder work would have to move into that subpackage. A consumer that requires upstream `v1.0.6` without our oggvorbis `replace` gets none of the work.
- **The forks are permanent.** [#519](https://github.com/dvoyni/cog/issues/519) assumed the `replace` lines would come out once upstream merged and tagged our changes, and that will not happen.

## 7. What this means for carrying otosound over

- **otosound carries over unchanged on oto v3.5.1** (or v3.5.0). The API is the same, the one fix doesn't reach float32, the three targets build without cgo, and the forks build and decode on Go 1.27.
- **Moving to v3.6.0 when it ships changes three things**: the declick ramp is no longer one buffer long on drivers that drain in smaller pieces (macOS, from the source); the Windows probe's agreement with oto narrows to `E_NOTFOUND`; and `Player.UnplayedSize` appears, with Windows support still pending. v3.6.0 also fixes the Windows, ALSA and darwin recovery bugs above.
- **Not measured anywhere:** otosound's 10 ms default on macOS and on Linux.
- **v1's recovery promise is wider than oto allows.** After a context error, nothing short of a process restart brings sound back. This is in v1 already and is not a v2 change.
