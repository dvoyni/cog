# Web Audio: Ogg Vorbis across browsers, and streaming long clips

Research for [#375](https://github.com/dvoyni/cog/issues/375), a child of map #294 (`jssound`).

- **Date:** 2026-09-14.
- **Versions checked:** Chrome 154.0.8037.17 stable ([chromiumdash](https://chromiumdash.appspot.com/fetch_releases?channel=Stable&platform=Windows&num=1)), Firefox 155.0.1 ([product-details](https://product-details.mozilla.org/1.0/firefox_versions.json)), and Safari 26.6 (released 2026-07-27, [WebKit blog](https://webkit.org/blog/18178/webkit-features-for-safari-26-6/)). I also read the source on WebKit `main` and on the `safari-7624.*` release branches, Chromium `main`, and Firefox `main` as of today.
- **Method:** specs, engine source, MDN browser-compat-data (BCD), caniuse, and first-party release notes. **I ran no browser tests.** Nothing here was measured in a real browser.
- Tags: **[V]** means verified against the linked primary source. **[I]** means inference. **[U]** means I could not verify it.

## Verdict

`jssound` can use `decodeAudioData` for Ogg Vorbis in every current target. Safari is the exception: it needs macOS 15.4 or later, or iOS/iPadOS 18.4 or later. Safari 18.4 and later on macOS Sonoma or Ventura still cannot decode Ogg. The wasm decoder stays the only fallback for those systems. WebCodecs `AudioDecoder("vorbis")` ships in all three engines, but Safari's implementation uses the same OS codec, so it adds no Safari coverage. What it does add is incremental decoding off the main thread.

A media element with a blob URL is a poor fit for music. Chrome and WebKit implement `loop` as a seek, so loops are not gapless. WebKit requires a user gesture per element. The element runs on its own clock, not the `AudioContext` clock. `AudioBufferSourceNode.start(when, offset)` is sample-accurate in all three engines. So long clips should stream as incrementally decoded PCM chunks, scheduled back to back on the context clock.

## 1. Ogg Vorbis in `decodeAudioData`

- **Spec [V]:** `decodeAudioData` accepts "any of the formats supported by the `<audio>` element". The content type is found by sniffing, not from a MIME type. Decoding runs on a separate decoding thread, and the `ArrayBuffer` is detached. Source: [Web Audio ED `index.bs`](https://github.com/WebAudio/web-audio-api/blob/main/index.bs), `decodeAudioData` algorithm.
- **Chrome [V]:** Ogg Vorbis has been supported for a long time. caniuse marks Vorbis "y" from Chrome 4 ([caniuse ogg-vorbis](https://github.com/Fyrd/caniuse/blob/main/features-json/ogg-vorbis.json)). Chromium registers `audio/ogg` with codecs {FLAC, OPUS, VORBIS} ([mime_util_internal.cc](https://source.chromium.org/chromium/chromium/src/+/main:media/base/mime_util_internal.cc)). `decodeAudioData` decodes through FFmpeg ([audio_file_reader.cc](https://source.chromium.org/chromium/chromium/src/+/main:media/filters/audio_file_reader.cc)).
- **Firefox [V]:** supported since Firefox 3.5 per caniuse. `MediaBufferDecoder.cpp` sniffs the container and uses the same decoders as media playback ([source](https://github.com/mozilla-firefox/firefox/blob/main/dom/media/webaudio/MediaBufferDecoder.cpp)).
- **Safari [V]:** Apple's [Safari 18.4 release notes](https://developer.apple.com/documentation/safari-release-notes/safari-18_4-release-notes) say: "Added support for Ogg Opus and Ogg Vorbis on macOS Sequoia 15.4, iOS 18.4, iPadOS 18.4, and visionOS 2.4." The same notes say Safari 18.4 also ships for macOS Sonoma and Ventura. **Ogg therefore depends on the OS version, not the Safari version** (see also [WebKit blog](https://webkit.org/blog/16574/webkit-features-in-safari-18-4/)).
  - [I] Safari 18.4 or later on Sonoma or Ventura cannot decode bare Ogg, because the codec comes from the OS media frameworks.
  - [V] **Vorbis inside WebM is an earlier, separate path.** caniuse marks Safari 14.1–18.3 "partial" with the notes "Supports the Vorbis audio codec, but not the Ogg container" and "Supported on macOS Big Sur 11.3 or later". iOS Safari is partial from 17.4. WebKit's `decodeAudioData` has had its own WebM demuxer since 2021 (commit `f1a836c378`, "[WebAudio] Add webm/opus container support").
  - [V] **How WebKit decodes.** On the `safari-7624.*` release branches, `AudioFileReaderCocoa.cpp` uses AudioToolbox `AudioFileOpenWithCallbacks`/`ExtAudioFile`. That path also serves bare Ogg once the OS supports it. On `main`, WebKit rewrote the reader onto AVFoundation `AVAssetReader` ([bug 307632](https://bugs.webkit.org/show_bug.cgi?id=307632), 309140@main, 2026-03-12).
  - [V] That rewrite broke Ogg decoding: the sniffer returned `application/ogg`, but AVFoundation wants `audio/ogg`. The fix is [bug 316172](https://bugs.webkit.org/show_bug.cgi?id=316172) (314462@main, 2026-06-03), which added the layout test `webaudio/decode-audio-data-ogg-vorbis.html`.
  - [I] The rewrite is not on the 7624 branches, so the regression most likely affected only the Safari 27 / iOS 27 betas. [U] I did not confirm which release first ships the rewrite.
  - [V] WebKit's iOS `TestExpectations` marks the Vorbis decode tests as failing because "vorbis decoder not available on simulator". That entry is about the simulator, not devices.
- **iOS Low Power Mode [V]:** I found no exception in the decode path. In WebKit's `HTMLMediaElement.cpp` and `MediaElementSession.cpp`, `RequireUserGestureForVideoDueToLowPowerMode` applies only when `element->isVideo()`. It does not affect Web Audio or `<audio>`.
- **Chrome loop-tail padding [U]:** [oggmented](https://github.com/jfrancos/oggmented) says Blink's FFmpeg adds samples to the end of decoded Vorbis, which clicks on loop. I found no Chromium bug confirming this for current versions. Mitigation [I]: set `loopStart`/`loopEnd` from the true sample count, which is the last page's granule position. Go can read that from the bytes.

## 2. The fallback when Safari cannot decode

- **WebCodecs `AudioDecoder` [V]** (BCD [`api/AudioDecoder.json`](https://github.com/mdn/browser-compat-data/blob/main/api/AudioDecoder.json)):
  - Supported in Chrome 94, Firefox 130, and Safari 26. Firefox for Android does not support it.
  - The interface is `[Exposed=(Window,DedicatedWorker), SecureContext]` ([WebCodecs spec](https://github.com/w3c/webcodecs/blob/main/index.src.html)), so it needs HTTPS.
  - **All three engines accept `codec: "vorbis"` and require a `description`:** Chromium ([audio_decoder.cc](https://source.chromium.org/chromium/chromium/src/+/main:third_party/blink/renderer/modules/webcodecs/audio_decoder.cc)), Firefox ([AudioDecoder.cpp](https://github.com/mozilla-firefox/firefox/blob/main/dom/media/webcodecs/AudioDecoder.cpp)), WebKit ([AudioDecoderCocoa.cpp](https://github.com/WebKit/WebKit/blob/main/Source/WebCore/platform/audio/cocoa/AudioDecoderCocoa.cpp), mapped to AudioToolbox `'vorb'`).
  - The description is Xiph extradata: `page_segments` + `segment_table` + the identification, comment, and setup headers ([Vorbis codec registration](https://github.com/w3c/webcodecs/blob/main/vorbis_codec_registration.src.html)). Go would demux Ogg pages into packets and build this.
  - [V] Safari 26 runs on macOS Sonoma, Sequoia, and 26 ([Safari 26 notes](https://developer.apple.com/documentation/safari-release-notes/safari-26-release-notes)).
  - [I] WebKit's WebCodecs Vorbis uses the same OS decoder as `decodeAudioData`, so it probably fails on Sonoma exactly where `decodeAudioData` fails. **It widens no Safari coverage.** Its value is streaming (see §3). Check at runtime with `AudioDecoder.isConfigSupported`.
- **wasm (`jfreymuth/oggvorbis`) stays the only coverage fallback** [I]. It is needed for macOS < 15.4 and iOS < 18.4.
  - Option [I]: run a second wasm instance in a dedicated Worker to get the decode off the main thread. That costs a second Go runtime.
  - Option [I]: decode incrementally on the main thread. From the ticket's measurement (1.3 s per 5 min), that is about 4.3 ms of main-thread time per second of audio.
- **Detection [I]:** at startup, decode a tiny embedded Ogg Vorbis clip with `decodeAudioData`. If it rejects with `EncodingError`, switch to wasm. This is more reliable than parsing the user agent, because support depends on the OS.

## 3. Streaming a long clip

**Memory [V, arithmetic]:** 300 s × 44 100 × 2 channels × 4 bytes ≈ 106 MB. `decodeAudioData` has no partial output (spec).

### `HTMLMediaElement` (blob URL) → `MediaElementAudioSourceNode`

- **Start latency [U]:** no spec bound exists, and I measured nothing. `play()` resolves its promise asynchronously. Needs a prototype measurement.
- **Seek accuracy:**
  - [V] `currentTime = t` is an exact seek. `fastSeek()` is the one that may snap ("approximate-for-speed") ([HTML media spec](https://html.spec.whatwg.org/multipage/media.html)).
  - [V] WebKit passes zero tolerance to `AVPlayerItem seekToTime` for exact seeks ([MediaPlayerPrivateAVFoundationObjC.mm](https://github.com/WebKit/WebKit/blob/main/Source/WebCore/platform/graphics/avfoundation/objc/MediaPlayerPrivateAVFoundationObjC.mm)).
  - [I] Seeks finish asynchronously (`seeked`), and the element's clock is not the `AudioContext` clock, so a seek cannot land on a context time.
- **Loop gaplessness:**
  - [V] The spec says a looping element must "seek to the earliest possible position" at the end.
  - [V] Chromium does exactly that (`Seek(EarliestPossiblePosition())` in [html_media_element.cc](https://source.chromium.org/chromium/chromium/src/+/main:third_party/blink/renderer/core/html/media/html_media_element.cc)), and so does WebKit (`seekInternal(MediaTime::zeroTime())` in `HTMLMediaElement.cpp`). [I] So loops are not gapless in Chrome or Safari.
  - [V] Firefox loops seamlessly: [bug 654787](https://bugzilla.mozilla.org/show_bug.cgi?id=654787) (Firefox 59), and `media.seamless-looping: true` in the current `StaticPrefList.yaml`.
- **Pitch via `playbackRate` with `preservesPitch = false`:**
  - [V] The spec says that with `preservesPitch` false, "the user agent must speed up or slow down the audio without any pitch adjustment".
  - [V] Unprefixed `preservesPitch` shipped in Chrome 86, Firefox 101, and Safari 17.2 (Safari had `webkitPreservesPitch` since 4) (BCD `api/HTMLMediaElement.json`).
  - [V] Chromium accepts rates from 0.0625 to 16 (`kMinPlaybackRate`/`kMaxPlaybackRate`). Gecko mutes audio outside 0.25–4 ([MDN](https://developer.mozilla.org/en-US/docs/Web/API/HTMLMediaElement/playbackRate)).
- **User gesture separate from the `AudioContext`:**
  - [V] Chrome's gate is page-level ("user has interacted with the domain"), per the [Chrome autoplay policy](https://developer.chrome.com/blog/autoplay).
  - [V] Firefox's gate is sticky activation ([MDN Autoplay guide](https://developer.mozilla.org/en-US/docs/Web/Media/Guides/Autoplay)).
  - [V] **WebKit's gate is per element.** Each element's `MediaElementSession` starts with `RequireUserGestureForAudioRateChange`. `play()` removes it only when called while `processingUserGestureForMedia()` (`HTMLMediaElement::removeBehaviorRestrictionsAfterFirstUserGesture`).
  - [I] I found no special case for elements routed into a `MediaElementAudioSourceNode`, so resuming the `AudioContext` does not unlock them. Each element must `play()` once inside a gesture.
- **Blob URL bug in Safari [V]:** "Playback of application/ogg blob media is broken" ([bug 301509](https://bugs.webkit.org/show_bug.cgi?id=301509), 304530@main, 2025-12-16). The fix is present on `safari-7624.1.16.13-branch`. [I] Build the blob with an explicit `type: "audio/ogg"`.
- **Other [V]:** the node resamples to the context rate. It outputs silence for CORS-cross-origin resources, which does not apply to same-origin blob URLs (Web Audio spec).

### Streaming without a media element [I]

- **Structure:** demux Ogg in Go and decode a few seconds ahead. Wrap each chunk in an `AudioBuffer` and schedule the chunks back to back with `start(t_n)`, where `t_{n+1} = t_n + frames_n / sampleRate`.
- **Decoders:** WebCodecs `AudioDecoder` where supported, and wasm otherwise. Memory stays bounded to the lookahead.
- **Seams:** make chunk frame counts exact at the context rate. The simplest way is to create the `AudioContext` at the clip rate or resample in Go. Otherwise each node resamples its own chunk and may interpolate across the seam without neighbouring samples.
- **Alternative:** an `AudioWorklet` ring buffer fed by `postMessage`. Supported in Chrome 66, Firefox 76, and Safari 14.1 (BCD). The spec itself says "If sample-accurate playback of network- or disk-backed assets is required, an implementer should use AudioWorkletNode". The risk is main-thread jank starving the worklet, so buffer generously.

## 4. Memory limits

- **No per-tab `AudioBuffer` quota exists in any engine [V]:**
  - WebKit caps a single buffer at 2^30 frames per channel and 2^32 samples in total (`s_maxChannelLength`/`s_maxLength` in [AudioBuffer.h](https://github.com/WebKit/WebKit/blob/main/Source/WebCore/Modules/webaudio/AudioBuffer.h)). That is about 6.7 h per channel at 44.1 kHz.
  - Blink fails only when `DOMFloat32Array::CreateOrNull` fails ([audio_buffer.cc](https://source.chromium.org/chromium/chromium/src/+/main:third_party/blink/renderer/modules/webaudio/audio_buffer.cc)).
- **macOS Safari [V]:** WebKit kills a WebContent process over a footprint threshold ([MemoryPressureHandler.cpp](https://github.com/WebKit/WebKit/blob/main/Source/WTF/wtf/MemoryPressureHandler.cpp)).
  - Active process: 7 GB + 1 GB per tab, or 15 GB + 1 GB per tab when RAM exceeds 16 GB.
  - Inactive process: 3 GB + 1 GB per tab, capped at 90 % of RAM.
  - The monitor is enabled only on `PLATFORM(MAC)` (`ENABLE_PERIODIC_MEMORY_MONITOR` in `PlatformEnableCocoa.h`).
- **iOS Safari [U]:** WebKit sets no threshold of its own. The OS jetsam limit governs, and Apple does not document its value per device. [I] Treat ~106 MB per 5-minute track as unaffordable and stream music. Budget decoded SFX explicitly.
- **Transient peaks [I]:**
  - WebKit's `AVAssetReader` path keeps every compressed sample buffer alive while it decodes.
  - The wasm fallback holds PCM twice, on the Go heap and in the `AudioBuffer`, until the Go slice is freed.

## 5. Scheduling

- **`AudioBufferSourceNode.start(when, offset)` is sample-accurate on the context clock.**
  - [V] Spec: "the exact value of `when` is always used without rounding to the nearest sample frame". The offset "can be expressed with sub-sample precision". A `when` earlier than `currentTime` starts immediately.
  - [V] Blink and WebKit compute the start frame with `TimeToSampleFrame(..., RoundUp)` and pass the sub-frame remainder (`start_frame_offset`) into interpolation ([audio_scheduled_source_handler.cc](https://source.chromium.org/chromium/chromium/src/+/main:third_party/blink/renderer/modules/webaudio/audio_scheduled_source_handler.cc), WebKit [AudioScheduledSourceNode.cpp](https://github.com/WebKit/WebKit/blob/main/Source/WebCore/Modules/webaudio/AudioScheduledSourceNode.cpp)).
  - [V] Gecko rounds to the resampler's sub-sample grid (`llround(mStart * ratioNum)` in [AudioBufferSourceNode.cpp](https://github.com/mozilla-firefox/firefox/blob/main/dom/media/webaudio/AudioBufferSourceNode.cpp)).
- **A media element is not sample-accurate [V/I]:**
  - [V] There is no API to start it at an `AudioContext` time. `play()` means "as soon as possible".
  - [V] Loops are seeks in Chrome and WebKit.
  - [V] The spec steers sample-accurate streamed playback to `AudioWorkletNode`, not media elements.
  - [I] `getOutputTimestamp()` and `outputLatency` help correlate clocks (BCD: Safari 14.1 and 18.4). They do not make an element's start sample-accurate.
- **Pattern [I]:** schedule transitions ahead of `currentTime` by more than the worst main-thread stall, because a `when` in the past starts immediately. Keep all music and SFX on buffer sources to get sample-accurate transitions.

## What this means for `jssound`

- **Decode path:** `decodeAudioData` on a copy of the clip bytes, since the call detaches its buffer. At init, probe with an embedded micro-clip. Loop with `loopStart`/`loopEnd` taken from the granule position.
- **Streaming path:** demux in Go, decode incrementally (WebCodecs `vorbis` if `isConfigSupported`, else wasm), and schedule back-to-back `AudioBufferSourceNode` chunks, or use a worklet ring buffer. Media-element streaming is only for long, unsynced, non-looping audio. If used, each element needs its own gesture `play()` in Safari, and loops are not gapless in Chrome or Safari.
- **Fallback:** wasm `oggvorbis` for macOS < 15.4 and iOS < 18.4, preferably incremental or in a Worker. WebCodecs does not help those systems.
- **Sample-accurate scheduling answer:** yes for `AudioBufferSourceNode.start(when, offset)` in all three engines. No for media elements.
- **Still open [U]:** media element start latency, the current Chrome Vorbis tail padding, the iOS memory ceiling, and chunk-seam quality. All need a prototype on real devices.
