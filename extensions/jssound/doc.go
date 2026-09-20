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
// # This slice is the resident tier
//
// A Clip is decoded whole into an AudioBuffer and its Voices start from it.
// Streaming - a MediaElementAudioSourceNode over a Blob URL, for a Clip too long
// to hold - is a later slice, and the one rule it inherits is written where the
// bytes become a JS object: a Blob built for any media path carries
// type "audio/ogg".
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
