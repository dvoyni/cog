// Package internal is the jssound plugin: New, the Backend it provides for
// sound's BackendPort, the Web Audio node graph one Voice is, and the two tiers
// a Clip's samples reach it through - decoded whole into one AudioBuffer, or
// decoded a chunk at a time and scheduled on the context clock. Composition
// roots and tests reach New through jssoundplugin; everything else reaches
// jssound through its root.
//
// The package is cut along what each file is responsible to:
//
//   - webaudio.go is every call into syscall/js. It is the only file that names
//     a js.Value, so what this Adapter asks of a browser can be read in one
//     place - and so the rest of the package is arithmetic a reviewer can check
//     without knowing Web Audio.
//   - backend.go is the seam: the slot table, one batch per tick applied in the
//     order the seam states, the clip table, and the two polls.
//   - chain.go is one Voice's node graph and the playhead bookkeeping a pause
//     needs, because Web Audio has no pause and a resumed source must be told
//     where to start.
//   - clipdata.go is a prepared Clip in either tier, the line
//     Config.DecodedClipLimit draws between them, and the two resident routes:
//     the browser's decodeAudioData, and jfreymuth/oggvorbis in wasm with
//     copyToChannel.
//   - stream.go is the streamed tier: a decoder and a goroutine per Voice,
//     producing AudioBuffers a chunk at a time for chain.go to schedule back to
//     back with start(when, offset) on the context clock. No HTMLMediaElement
//     is constructed anywhere in this package, and the file says why.
//   - resample.go is the conversion a streamed Voice's chunks go through, which
//     is what makes a chunk boundary and a loop wrap land on whole context
//     frames. It is deliberately the same filter otosound uses.
//   - loopregion.go is the Loop Region parse, deliberately the same arithmetic
//     otosound and nosound run over the same bytes.
//   - probe.go is the one-time question "can this browser decode Ogg Vorbis",
//     and the micro-clip it is asked with.
//
// There is no file for the device thread, and that is the point. The browser
// owns the audio thread; nothing here can reach it, so sound.md's obligation
// about what that thread may touch holds vacuously.
//
// The plugin is built only for GOOS=js. It requires no Adapter and provides one,
// jssound.SoundBackend.
package internal
