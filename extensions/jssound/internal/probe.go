//go:build js

package internal

import _ "embed"

// oggSupport is the answer to the one question jssound asks the browser about
// itself: can it decode Ogg Vorbis?
type oggSupport int

const (
	// oggUnknown is before the probe has settled. Prepares that arrive here
	// wait, because a Clip decoded down the wrong route would be the probe
	// doing nothing.
	oggUnknown oggSupport = iota
	// oggNative is a browser whose decodeAudioData decodes Ogg Vorbis.
	oggNative
	// oggWasm is a browser whose does not, so jfreymuth/oggvorbis decodes in
	// Go and the samples reach an AudioBuffer through copyToChannel.
	oggWasm
)

// probeClip is the micro-clip the question is asked with: it is decoded once, at
// init, and whether the browser can decode it is the whole of the answer.
//
// It is a decode rather than a user-agent string, and rather than a Config,
// because the condition is not a browser's version: Ogg Vorbis in Safari needs
// macOS 15.4+ or iOS 18.4+, and Safari 18.4 on Sonoma or Ventura still cannot
// decode it, so the same browser build answers differently on two machines and
// no string a page can read distinguishes them. WebCodecs would not settle it
// either - Safari's AudioDecoder reaches the same operating system codec, so it
// reports exactly the same gap.
//
// canPlayType was rejected too: it is specified to answer "maybe", it is about
// media elements rather than about decodeAudioData, and Safari has answered
// "maybe" for formats it then refused. A decode that either produces an
// AudioBuffer or does not is the only question with a yes and a no.
//
//go:embed probe.ogg
var probeClip []byte
