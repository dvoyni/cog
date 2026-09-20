//go:build js

package internal

import (
	"encoding/binary"
	"errors"
	"math"
	"syscall/js"
	"time"
)

// This is every call into syscall/js that jssound makes, and it is one file on
// purpose: what this Adapter asks of a browser should be readable in one place,
// and everything else in the package should be arithmetic a reviewer can check
// without knowing Web Audio.
//
// Nothing here runs on the audio thread. Every one of these calls is made from
// sound's flush or from a JS callback, both of which are the page's main thread;
// the browser's audio thread is on the other side of the node graph and cannot
// be reached from Go at all. That is sound.md's device-thread obligation holding
// vacuously rather than being waived.

// gestureEvents are the events a browser accepts as the user gesture that lets a
// suspended AudioContext resume. Every browser starts a context suspended under
// its autoplay policy, and nothing above this Adapter can resume it: sound's
// Port has no verb for it and a game is given Device.Ready and nothing else. So
// the Adapter listens for the gesture itself, resumes once, and lets Ready going
// true be the whole of what the game sees.
var gestureEvents = [...]string{"pointerdown", "keydown", "touchend"}

// errNoWebAudio is a page with no AudioContext constructor at all: a very old
// browser, or a non-browser host. It is what makes jssound behave as nosound
// does rather than fail a composition.
var errNoWebAudio = errors.New("this page has no AudioContext")

// webAudio is one AudioContext and the handles this Adapter keeps on it. One
// per Engine: a context is the browser's whole audio device, and two Engines in
// one page each get their own, which is the limit otosound has on desktop and
// does not have here.
type webAudio struct {
	ctx         js.Value
	destination js.Value
	// listeners are the JS functions this Adapter has installed on the context
	// and must give back: a js.Func is the one thing in syscall/js that is not
	// garbage-collectable, so every one made here is tracked to its Release -
	// and taken off the context first, because closing a context fires
	// statechange and a released function called is a panic in the page.
	listeners []installed
	// gesture is the one-shot listener that resumes the context, kept so it can
	// be taken off the page whether it fired or the Engine stopped first.
	gesture  js.Func
	gestured bool
}

// installed is one listener on the context: the event it is on, and the function
// that has to be taken off it and released together.
type installed struct {
	event string
	fn    js.Func
}

// openContext constructs the Engine's AudioContext. It returns an error rather
// than panicking on a page that has no Web Audio, because registration never
// fails on a Device: the Adapter reports the condition once and plays nothing.
func openContext(hint time.Duration) (ctx *webAudio, err error) {
	constructor := js.Global().Get("AudioContext")
	if constructor.IsUndefined() || constructor.IsNull() {
		// The prefixed name is Safari's, and old enough that the Ogg question
		// below is the more interesting one on the same browsers.
		constructor = js.Global().Get("webkitAudioContext")
	}
	if constructor.IsUndefined() || constructor.IsNull() {
		return nil, errNoWebAudio
	}
	defer func() {
		if failure := recover(); failure != nil {
			ctx, err = nil, jsError(failure)
		}
	}()

	options := js.Global().Get("Object").New()
	if hint <= 0 {
		options.Set("latencyHint", "interactive")
	} else {
		options.Set("latencyHint", hint.Seconds())
	}
	value := constructor.New(options)
	return &webAudio{ctx: value, destination: value.Get("destination")}, nil
}

// now is the context's clock, in seconds. It is read once per batch, so that
// every operation one tick states lands at one instant in audio time - which is
// the browser's own spelling of otosound's "a tick is handed over atomically".
func (w *webAudio) now() float64 { return w.ctx.Get("currentTime").Float() }

// running reports whether the context is past the browser's autoplay gate and
// is actually producing audio. It is the whole of Device.Ready.
func (w *webAudio) running() bool { return w.ctx.Get("state").String() == "running" }

// sampleRate is the rate the browser chose, which is the output device's. No
// Config names it: decodeAudioData resamples into it, and an Adapter that asked
// for a rate would be asking for something no browser promises.
func (w *webAudio) sampleRate() int { return int(w.ctx.Get("sampleRate").Float()) }

// quantum is a render quantum in seconds, 128 frames at the context's rate. It
// is the block rate this Adapter declicks over, exactly as otosound declicks
// over its 10 ms buffer: the rate is the browser's and only the Adapter knows
// it, which is why declicking is the Adapter's job and not sound's.
func (w *webAudio) quantum() float64 {
	rate := w.ctx.Get("sampleRate").Float()
	if rate <= 0 {
		return 0
	}
	return renderQuantum / rate
}

// latency is what the context actually costs, base plus output, and never a
// figure from a Config. The contract promises ordering and causality and never a
// latency figure, so this is the only place one is honest.
func (w *webAudio) latency() time.Duration {
	seconds := numberOr(w.ctx.Get("baseLatency"), 0) + numberOr(w.ctx.Get("outputLatency"), 0)
	return time.Duration(seconds * float64(time.Second))
}

// watchState calls changed whenever the context's state moves, which is how
// Ready becomes true without polling a JS property every flush. Device() stays a
// field read, which is what lets sound poll it once per tick.
func (w *webAudio) watchState(changed func()) {
	handler := js.FuncOf(func(js.Value, []js.Value) any {
		changed()
		return js.Undefined()
	})
	w.listeners = append(w.listeners, installed{event: "statechange", fn: handler})
	w.ctx.Call("addEventListener", "statechange", handler)
}

// resumeOnGesture installs the one-shot listener that takes the context past the
// browser's autoplay gate, and calls resumed once it has asked.
//
// It asks on the first gesture and then takes itself off the page. A game that
// draws "click to enable sound" on Device.Ready being false therefore needs to
// do nothing else: the click that dismisses its own prompt is the gesture.
func (w *webAudio) resumeOnGesture(target js.Value, resumed func()) {
	if target.IsUndefined() || target.IsNull() || target.Get("addEventListener").IsUndefined() {
		return
	}
	w.gesture = js.FuncOf(func(js.Value, []js.Value) any {
		w.removeGesture(target)
		w.resume()
		resumed()
		return js.Undefined()
	})
	w.gestured = true
	for _, event := range gestureEvents {
		target.Call("addEventListener", event, w.gesture)
	}
}

// removeGesture takes the gesture listener off the page and gives its js.Func
// back. It is called from inside the listener itself, which is safe - the call
// in flight already holds the function - and again at Stop, which is why it
// guards on having installed one.
func (w *webAudio) removeGesture(target js.Value) {
	if !w.gestured {
		return
	}
	w.gestured = false
	for _, event := range gestureEvents {
		target.Call("removeEventListener", event, w.gesture)
	}
	w.gesture.Release()
}

// resume asks the browser to start the context. It is safe to call on a context
// that is already running, and a browser that refuses because no gesture has
// happened yet simply leaves the state where it was.
func (w *webAudio) resume() {
	defer func() { _ = recover() }()
	w.ctx.Call("resume")
}

// close tears the whole graph down with the context that owns it, and gives back
// every js.Func this Adapter installed. A js.Value needs nothing: syscall/js
// drops the JS-side reference when the Go value is collected, which is the
// Port's garbage-collectability rule holding for the prepared AudioBuffers too.
func (w *webAudio) close(gestureTarget js.Value) {
	w.removeGesture(gestureTarget)
	// Off the context before it is closed, because closing one fires a last
	// statechange and a released js.Func called from JS is a panic in the page
	// rather than an error anything can catch.
	for _, listener := range w.listeners {
		w.ctx.Call("removeEventListener", listener.event, listener.fn)
	}
	func() {
		defer func() { _ = recover() }()
		w.ctx.Call("close")
	}()
	for _, listener := range w.listeners {
		listener.fn.Release()
	}
	w.listeners = nil
}

// decode hands encoded bytes to the browser's own decoder. It is the callback
// form rather than the promise, because the callback form is what every browser
// that needs the wasm fallback also understands.
//
// Exactly one of ok and failed runs, and the two js.Funcs are released by
// whichever it was. They are the only thing in this Adapter that needs releasing
// at all: a js.Func is not garbage-collectable, and an AudioBuffer is.
func (w *webAudio) decode(encoded []byte, ok func(js.Value), failed func(string)) {
	buffer := js.Global().Get("Uint8Array").New(len(encoded))
	js.CopyBytesToJS(buffer, encoded)

	var success, failure js.Func
	var done bool
	release := func() {
		if done {
			return
		}
		done = true
		// Released from inside the call the browser is making: syscall/js has
		// already resolved the function it is invoking, so giving the handle
		// back here is the earliest moment it can be given back at all.
		success.Release()
		failure.Release()
	}
	success = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer release()
		if len(args) == 0 {
			failed("the browser reported a decode with no AudioBuffer")
			return js.Undefined()
		}
		ok(args[0])
		return js.Undefined()
	})
	failure = js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer release()
		failed(errorMessage(args))
		return js.Undefined()
	})

	defer func() {
		if caught := recover(); caught != nil {
			release()
			failed(jsError(caught).Error())
		}
	}()
	w.ctx.Call("decodeAudioData", buffer.Get("buffer"), success, failure)
}

// newBuffer makes an AudioBuffer at the source's own rate, never the context's.
// That is sound.md's fourth obligation: the Device rate is not baked into what
// Prepare returns, so a Clip costs the cache nothing if the output device
// changes - the browser resamples at playback instead.
func (w *webAudio) newBuffer(channels, frames, rate int) (buffer js.Value, err error) {
	defer func() {
		if caught := recover(); caught != nil {
			buffer, err = js.Undefined(), jsError(caught)
		}
	}()
	return w.ctx.Call("createBuffer", channels, frames, rate), nil
}

// copyToChannel fills one channel of an AudioBuffer from interleaved Go samples,
// de-interleaving on the way. It is how the wasm decoder's output reaches the
// browser at all: there is no other route from a []float32 to an AudioBuffer.
//
// The bytes go across as a Uint8Array and are re-viewed as a Float32Array over
// the same ArrayBuffer, because js.CopyBytesToJS is defined for byte-shaped
// typed arrays alone.
func copyToChannel(buffer js.Value, channel, channels int, samples []float32) {
	frames := len(samples) / channels
	raw := make([]byte, frames*4)
	for f := range frames {
		binary.LittleEndian.PutUint32(raw[f*4:], math.Float32bits(samples[f*channels+channel]))
	}
	bytes := js.Global().Get("Uint8Array").New(len(raw))
	js.CopyBytesToJS(bytes, raw)
	view := js.Global().Get("Float32Array").New(bytes.Get("buffer"), 0, frames)
	buffer.Call("copyToChannel", view, channel)
}

// newSource is one playing Voice's AudioBufferSourceNode. Web Audio gives no way
// to restart one, so a start - and a Seek, which is a start with an offset - is
// always a fresh node.
func (w *webAudio) newSource(buffer js.Value) js.Value {
	source := w.ctx.Call("createBufferSource")
	source.Set("buffer", buffer)
	return source
}

func (w *webAudio) newSplitter() js.Value {
	return w.ctx.Call("createChannelSplitter", outChannels)
}

func (w *webAudio) newMerger() js.Value {
	return w.ctx.Call("createChannelMerger", outChannels)
}

func (w *webAudio) newGain() js.Value { return w.ctx.Call("createGain") }

// setGain moves one entry of the gain matrix towards its target over the
// browser's own render quantum. This is the declick, and setTargetAtTime is the
// shape of it: sound emits target values once per flush and the Adapter ramps to
// them across its own blocks, because only the Adapter knows its block rate.
//
// An exponential approach rather than a linear ramp because the browser has one
// and a linear ramp would need an end time this Adapter does not know: the next
// flush may be 16.7 ms away or may be a slow frame away, and a ramp that ended
// early would be the step it was meant to remove.
func setGain(gain js.Value, value float32, at, timeConstant float64) {
	gain.Get("gain").Call("setTargetAtTime", float64(value), at, timeConstant)
}

// setGainNow puts a gain where it belongs with no ramp at all. A start begins at
// its target rather than fading in - a one-shot that faded in over a block would
// lose the transient that is the reason it was played - and a stop of a Voice
// that has not been heard yet cuts outright, so a play and a stop in one tick is
// audible for zero samples.
//
// It is cancelScheduledValues and setValueAtTime rather than an assignment to
// .value, because assigning to an AudioParam that is under automation is ignored
// until the automation ends: a gain still ramping from a pause would swallow the
// step this is asking for.
func setGainNow(gain js.Value, value float32, at float64) {
	param := gain.Get("gain")
	param.Call("cancelScheduledValues", at)
	param.Call("setValueAtTime", float64(value), at)
}

// numberOr reads a JS number, answering fallback where the property is absent -
// which outputLatency is on browsers that never implemented it.
func numberOr(value js.Value, fallback float64) float64 {
	if value.Type() != js.TypeNumber {
		return fallback
	}
	number := value.Float()
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return fallback
	}
	return number
}

// errorMessage reads whatever a browser handed an error callback. decodeAudioData
// is specified to pass a DOMException and old implementations pass nothing at
// all, so this reports something either way.
func errorMessage(args []js.Value) string {
	if len(args) == 0 {
		return "the browser reported no reason"
	}
	if message := args[0].Get("message"); message.Type() == js.TypeString {
		return message.String()
	}
	return args[0].String()
}

// jsError turns a recovered syscall/js panic into an ordinary error. Every call
// into JS can throw, and a thrown exception crossing a Backend method as a panic
// would take the game's tick with it.
func jsError(caught any) error {
	if thrown, ok := caught.(js.Error); ok {
		return errors.New(thrown.Error())
	}
	if failure, ok := caught.(error); ok {
		return failure
	}
	return errors.New("the browser threw a value that is not an error")
}
