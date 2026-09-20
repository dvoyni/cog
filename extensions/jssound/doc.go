// Package jssound declares the browser Extension of sound: a plugin that fills
// sound's BackendPort with Web Audio nodes, so the browser decodes and mixes on
// its own audio thread rather than on the one that starves.
//
// This is the structural fix rather than a preference. Nothing gets a PCM mixer
// off the browser's main thread: Go's wasm target is single-threaded, a
// Worker-hosted instance forks the runtime and puts Go's garbage collector on a
// real-time thread, and a SharedArrayBuffer still leaves the producer on the
// main thread. otosound measured 1.880 s of audio produced for a 4.000 s window
// at a 20 ms buffer on an idle page. So the browser's own node graph is the
// answer, and a web build composing otosound fails to compile rather than
// shipping a Mixer that starves.
//
// # What a Voice is here
//
// One AudioBufferSourceNode per playing Voice, through a channel splitter, four
// gain nodes and a channel merger, to the context's destination. The four gains
// are sound's own 2x2 matrix, Gains[src][out], driven at tick rate with
// setTargetAtTime so the browser smooths between them over its own 128-sample
// quantum.
//
// There is deliberately no PannerNode. The arithmetic is sound's - the W3C
// equations transcribed once, above the seam, in slots/sound/internal/types -
// and a Voice spatialized by the browser on web and by our Mixer on desktop
// would be two implementations of one equation that can disagree about the same
// Clip. What crosses the seam is a gain matrix, and this Adapter multiplies.
//
// # The device thread rule is vacuous, and that is the correct outcome
//
// sound.md's second obligation - what the device thread may touch, and that it
// may not allocate, lock, free, decode or call into sound - is satisfied here by
// having no device thread at all. The browser owns the audio thread, it runs in
// another process' worth of isolation from Go's heap, and nothing in this
// package can reach it. That is the rule holding vacuously rather than an
// exemption from it, and the whole of what this Adapter buys.
//
// # Ogg Vorbis, and Safari
//
// A Clip is decoded by the browser through decodeAudioData when the browser can
// decode Ogg Vorbis, and by jfreymuth/oggvorbis in wasm, into an AudioBuffer
// filled with copyToChannel, when it cannot.
//
// Which it is, is probed once at init by decoding an embedded micro-clip, and is
// never sniffed from a user agent and never configured. Safari is the wrinkle
// and it is an operating system wrinkle rather than a browser version one: Ogg
// Vorbis needs macOS 15.4+ or iOS 18.4+, and Safari 18.4 on Sonoma or Ventura
// still cannot decode it, so no version test can be written. WebCodecs adds no
// coverage either, because Safari's AudioDecoder reaches the same OS codec.
//
// That probe settles the resident tier. The streamed tier has a second one of
// its own, and a second decoder behind it. decodeAudioData takes a whole file
// and gives back a whole buffer, so it cannot stream; WebCodecs
// AudioDecoder("vorbis") can, and where a browser's takes the configuration a
// streamed Clip is demuxed in Go and decoded on a thread of the browser's own
// rather than in wasm on the thread the game's tick is also on. Where it does
// not - which is every Safari, because its AudioDecoder reaches the same
// operating system codec that has no Ogg - jfreymuth/oggvorbis decodes in Go
// exactly as before.
//
// Which it is, is AudioDecoder.isConfigSupported asked once at init with the
// micro-clip's own setup headers, so the question asked is the one the route
// will ask rather than a codec string. To force the Go decoder in a browser
// that has the codec, which is how the two are compared in one page, delete
// AudioDecoder from the global object before the Engine starts.
//
// # Two tiers, and which is invisible
//
// A Clip whose decoded size fits under Config.DecodedClipLimit is decoded whole
// into one AudioBuffer and its Voices start from it. A longer one streams: it is
// demuxed in Go and decoded a few thousand frames at a time - by the browser's
// AudioDecoder where there is one, and in wasm where there is not - converted to
// the context's rate through one resampler that spans every chunk, and the
// chunks are scheduled back to back with start(when, offset) on the context
// clock. The limit is the same field with the same sentinels and the same
// 512 KiB default that otosound.Config carries.
//
// Which tier a Clip landed in is invisible. Both report the same duration, the
// same channels, the same rate and the same Loop Region, and a game cannot ask -
// which is the command surface's rule holding rather than being reopened.
//
// # Not a media element, and the Blob rule that went with it
//
// The streamed tier was expected to be a MediaElementAudioSourceNode over a
// Blob URL, and it is not. A media element was checked and refused for a Clip:
// Chromium and WebKit implement its loop as a seek, so a looping track through
// one is not gapless and sound.md's gapless promise could not be kept; WebKit's
// autoplay gesture gate is per element, so every track would need a gesture of
// its own rather than the one the context already has; and the element runs on
// its own clock rather than the AudioContext's, so a chunk could not be landed
// on a context time and two Voices could not be started together.
//
// Nothing in this Extension constructs one, and a test counts it. With no media
// path there is no Blob of encoded audio either, so the rule that such a Blob
// must carry type "audio/ogg" - written down against the media route that was
// then refused - has nothing left to govern and is retired rather than left
// standing as a rule about code that does not exist. It would come back with
// WebCodecs only if that route ever needed a Blob, which it does not: an
// EncodedAudioChunk is bytes and a timestamp.
//
// jssound is an Extension. Its root declares only Name, Config, its one Adapter,
// SoundBackend, and its errors, and is untagged so the declarations build
// everywhere; the plugin, built by jssoundplugin.New, is in its internal/, and
// that is what carries the js tag. Composing jssound off the web therefore fails
// to compile on jssoundplugin.New rather than at run time. Its Config is
// supplied under Name.
//
//	config := map[kernel.PluginName]any{
//	    sound.Name:   sound.Config{},
//	    jssound.Name: jssound.Config{},
//	}
//	plugins := []kernel.Plugin{
//	    soundplugin.New(),
//	    jssoundplugin.New(),
//	    …
//	}
package jssound
