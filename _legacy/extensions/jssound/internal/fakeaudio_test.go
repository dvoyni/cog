//go:build js

package internal

import (
	"syscall/js"
	"testing"
	"time"
)

// This is the Web Audio a test gets. node has none, so the suite installs its
// own AudioContext on the global object for the length of a test - which is the
// shape jsstorage's fakeLocalStorage already established one Extension over, and
// the only way a package whose whole subject is a browser API is testable
// without a browser.
//
// It is a recording fake rather than a stub. Every node it makes remembers what
// it was connected to and what was asked of it, so a test can assert that the
// graph jssound built is the graph the spec describes: four gains fed from a
// splitter's two outputs and feeding a merger's two inputs, carrying sound's own
// matrix, with no PannerNode anywhere.
//
// # Everything calls back asynchronously, on purpose
//
// A real browser resolves decodeAudioData and fires statechange from the event
// loop, never inside the call that provoked them. The fake does the same, with
// queueMicrotask, and for a harder reason than fidelity: a JS callback invoked
// synchronously from inside a Go-to-JS call re-enters the wasm instance while Go
// is already running it, which is a hazard the real API never presents. So a
// test triggers something and then calls yield, exactly as a frame would.
const fakeAudioSource = `
globalThis.__cogAudio = {
  events: [], contexts: [], pending: [], gains: [], sources: [], buffers: [],
  panners: 0, mediaElements: 0, nextId: 1, initialState: "suspended",
};
(function () {
  var A = globalThis.__cogAudio;
  function log(line) { A.events.push(line); }
  function newId(kind) { return kind + (A.nextId++); }

  function param(owner, label, value) {
    return {
      _value: value,
      get value() { return this._value; },
      set value(v) { this._value = v; log(owner + "." + label + "=" + v); },
      setTargetAtTime: function (target, at, tc) {
        this._value = target;
        log(owner + "." + label + ".setTargetAtTime " + target + " " + at + " " + tc);
        A.lastTimeConstant = tc;
        return this;
      },
      setValueAtTime: function (value, at) {
        this._value = value;
        log(owner + "." + label + ".setValueAtTime " + value + " " + at);
        return this;
      },
      cancelScheduledValues: function (at) { log(owner + "." + label + ".cancel " + at); return this; },
    };
  }

  function node(kind) {
    var n = { id: newId(kind), kind: kind, connections: 0, edges: [] };
    n.connect = function (to, out, inp) {
      out = out === undefined ? 0 : out;
      inp = inp === undefined ? 0 : inp;
      log("connect " + n.id + " -> " + (to && to.id ? to.id : "?") + " [" + out + "," + inp + "]");
      if (n.kind === "splitter" && to && to.kind === "gain") { to.src = out; }
      if (n.kind === "gain" && to && to.kind === "merger") { n.out = inp; }
      n.edges.push({ to: to, out: out, inp: inp });
      n.connections++;
      return to;
    };
    n.disconnect = function () { log("disconnect " + n.id); n.disconnected = true; };
    return n;
  }

  function buffer(channels, frames, rate) {
    var b = node("buffer");
    b.numberOfChannels = channels;
    b.length = frames;
    b.sampleRate = rate;
    b.duration = frames / rate;
    b.filled = [];
    // The samples are kept, not just counted. A buffer that remembered only how
    // many frames it was handed could not be rendered, and the loop-gapless
    // assertion is an assertion about frames.
    b.data = [];
    for (var c = 0; c < channels; c++) { b.data.push(new Float32Array(frames)); }
    b.copyToChannel = function (data, channel) {
      b.filled.push({ channel: channel, frames: data.length, first: data.length ? data[0] : 0 });
      if (b.data[channel]) { b.data[channel].set(data.subarray(0, Math.min(data.length, frames))); }
      log("copyToChannel " + b.id + " ch" + channel + " x" + data.length);
    };
    A.buffers.push(b);
    return b;
  }
  A.makeBuffer = buffer;

  globalThis.AudioContext = function (options) {
    var ctx = this;
    A.contexts.push(ctx);
    ctx.options = options;
    ctx.state = A.initialState;
    ctx.currentTime = 0;
    ctx.sampleRate = 48000;
    ctx.baseLatency = 0.01;
    ctx.outputLatency = 0.02;
    ctx.destination = node("destination");
    ctx.listeners = {};
    ctx.addEventListener = function (name, fn) {
      (ctx.listeners[name] = ctx.listeners[name] || []).push(fn);
    };
    ctx.removeEventListener = function (name, fn) {
      var kept = (ctx.listeners[name] || []).filter(function (f) { return f !== fn; });
      ctx.listeners[name] = kept;
    };
    ctx.fire = function (name) {
      (ctx.listeners[name] || []).slice().forEach(function (fn) {
        queueMicrotask(function () { fn({ type: name }); });
      });
    };
    ctx.setState = function (state) { ctx.state = state; ctx.fire("statechange"); };
    ctx.resume = function () { log("resume"); if (ctx.state === "suspended") { ctx.setState("running"); } };
    ctx.close = function () { log("close"); ctx.setState("closed"); };
    ctx.createGain = function () {
      var n = node("gain");
      n.gain = param(n.id, "gain", 1);
      A.gains.push(n);
      return n;
    };
    ctx.createChannelSplitter = function (outputs) { var n = node("splitter"); n.outputs = outputs; return n; };
    ctx.createChannelMerger = function (inputs) { var n = node("merger"); n.inputs = inputs; return n; };
    ctx.createBufferSource = function () {
      var n = node("source");
      n.playbackRate = param(n.id, "playbackRate", 1);
      n.loop = false;
      n.loopStart = 0;
      n.loopEnd = 0;
      n.start = function (when, offset) {
        n.started = { when: when, offset: offset === undefined ? 0 : offset };
        log("start " + n.id + " when=" + when + " offset=" + n.started.offset);
      };
      n.stop = function (when) { n.stopped = { when: when }; log("stop " + n.id + " when=" + when); };
      A.sources.push(n);
      return n;
    };
    ctx.createBuffer = function (channels, frames, rate) { return buffer(channels, frames, rate); };
    ctx.createPanner = function () { A.panners++; return node("panner"); };
    ctx.createStereoPanner = function () { A.panners++; return node("stereoPanner"); };
    // The two ways a Clip could have reached the graph through an
    // HTMLMediaElement, present so that "no media element is used for a Clip"
    // is a count a test reads rather than a claim a reviewer takes on trust.
    ctx.createMediaElementSource = function () { A.mediaElements++; return node("mediaElementSource"); };
    ctx.decodeAudioData = function (data, ok, fail) {
      A.pending.push({ bytes: data.byteLength, ok: ok, fail: fail });
      log("decodeAudioData " + data.byteLength);
    };
  };

  // settle resolves every decode the Adapter has asked for, as the browser
  // would: from the event loop, never inside the call that asked.
  A.settle = function (succeed) {
    var pending = A.pending;
    A.pending = [];
    pending.forEach(function (one) {
      queueMicrotask(function () {
        if (succeed) { one.ok(buffer(2, 48000, 48000)); }
        else { one.fail({ message: "this fake browser has no vorbis" }); }
      });
    });
    return pending.length;
  };

  A.advance = function (seconds) {
    A.contexts.forEach(function (ctx) { ctx.currentTime += seconds; });
  };

  // matrixOf reconstructs one source's 2x2 gain matrix by walking the graph it
  // was connected through: source -> splitter -> gain[src][out] -> merger. It is
  // the wiring that is read, never a logged value, so a gain on the wrong pair
  // renders to the wrong side rather than quietly passing.
  function matrixOf(source) {
    var m = [[0, 0], [0, 0]];
    source.edges.forEach(function (e) {
      if (!e.to || e.to.kind !== "splitter") { return; }
      e.to.edges.forEach(function (s) {
        var gain = s.to;
        if (!gain || gain.kind !== "gain") { return; }
        gain.edges.forEach(function (g) {
          if (!g.to || g.to.kind !== "merger") { return; }
          m[s.out][g.inp] = gain.gain.value;
        });
      });
    });
    return m;
  }

  // renderOffline is this suite's OfflineAudioContext. node has no Web Audio at
  // all, so there is no real one to render through; what there is instead is the
  // graph the Adapter actually built, with every node's wiring, every buffer's
  // samples and every start(when, offset) and stop(when) recorded - and summing
  // that on the context's own frame grid is the same arithmetic an offline
  // render is, over the same inputs.
  //
  // It is deliberately nearest-frame and never interpolating. A seam that only
  // lined up because the renderer smoothed it would be a seam this suite could
  // not see, and the whole point of the assertion is that chunk boundaries land
  // on whole context frames.
  A.renderOffline = function (seconds) {
    var ctx = A.contexts[0];
    var sr = ctx.sampleRate;
    var frames = Math.round(seconds * sr);
    var out = [new Float32Array(frames), new Float32Array(frames)];
    A.sources.forEach(function (n) {
      if (!n.started || !n.buffer) { return; }
      var m = matrixOf(n);
      var rate = n.playbackRate.value;
      var from = Math.round(n.started.when * sr);
      var head = Math.round(n.started.offset * n.buffer.sampleRate);
      var until = n.stopped ? Math.round(n.stopped.when * sr) : frames;
      if (until > frames) { until = frames; }
      for (var i = Math.max(from, 0); i < until; i++) {
        var pos = head + Math.round((i - from) * rate);
        if (pos < 0 || pos >= n.buffer.length) { break; }
        for (var c = 0; c < n.buffer.numberOfChannels && c < 2; c++) {
          var sample = n.buffer.data[c][pos];
          out[0][i] += sample * m[c][0];
          out[1][i] += sample * m[c][1];
        }
      }
    });
    return [Array.prototype.slice.call(out[0]), Array.prototype.slice.call(out[1])];
  };

  globalThis.Audio = function () { A.mediaElements++; };

  globalThis.document = {
    listeners: {},
    createElement: function (name) {
      if (String(name).toLowerCase() === "audio" || String(name).toLowerCase() === "video") {
        A.mediaElements++;
      }
      return { tagName: String(name).toUpperCase() };
    },
    addEventListener: function (name, fn) {
      (this.listeners[name] = this.listeners[name] || []).push(fn);
    },
    removeEventListener: function (name, fn) {
      this.listeners[name] = (this.listeners[name] || []).filter(function (f) { return f !== fn; });
    },
    gesture: function (name) {
      (this.listeners[name] || []).slice().forEach(function (fn) {
        queueMicrotask(function () { fn({ type: name }); });
      });
    },
  };
})();
`

// fakeAudio installs the fake AudioContext and a fake document for the length of
// the test, and hands back a handle to the recording behind them.
func fakeAudio(t *testing.T) *audioFake {
	t.Helper()
	js.Global().Call("eval", fakeAudioSource)
	t.Cleanup(func() {
		js.Global().Delete("AudioContext")
		js.Global().Delete("Audio")
		js.Global().Delete("document")
		js.Global().Delete("__cogAudio")
	})
	return &audioFake{t: t, value: js.Global().Get("__cogAudio")}
}

// noWebAudio installs the fake document but no AudioContext at all, which is the
// page jssound has to behave as nosound on.
func noWebAudio(t *testing.T) {
	t.Helper()
	js.Global().Call("eval", fakeAudioSource)
	js.Global().Delete("AudioContext")
	t.Cleanup(func() {
		js.Global().Delete("Audio")
		js.Global().Delete("document")
		js.Global().Delete("__cogAudio")
	})
}

type audioFake struct {
	t     *testing.T
	value js.Value
}

// yield hands the thread back to the JS event loop so the microtasks the fake
// queued - a settled decode, a statechange, a gesture - reach Go. A frame does
// the same thing by ending; a test has to say so.
func (f *audioFake) yield() {
	f.t.Helper()
	for range 4 {
		time.Sleep(time.Millisecond)
	}
}

// settle resolves every decode outstanding, succeeding or failing them all, and
// then waits for the callbacks to land. It is how a test says "the browser
// answered", and whether it answered yes is how a test picks which decoder
// jssound uses.
func (f *audioFake) settle(succeed bool) int {
	f.t.Helper()
	settled := f.value.Call("settle", succeed).Int()
	f.yield()
	return settled
}

// advance moves the context clock on, which is what makes a later batch's
// operations land later than an earlier one's.
func (f *audioFake) advance(seconds float64) {
	f.value.Call("advance", seconds)
}

// gesture is the user's first click, delivered the way a browser delivers it.
func (f *audioFake) gesture(event string) {
	f.t.Helper()
	js.Global().Get("document").Call("gesture", event)
	f.yield()
}

// context is the AudioContext the Engine opened. A test asserting on a second
// one would be asserting that an Engine opened two, which it must not.
func (f *audioFake) context() js.Value {
	f.t.Helper()
	contexts := f.value.Get("contexts")
	if contexts.Length() != 1 {
		f.t.Fatalf("the Engine opened %d AudioContexts, want exactly one", contexts.Length())
	}
	return contexts.Index(0)
}

// sources is every AudioBufferSourceNode ever made, in the order they were made.
func (f *audioFake) sources() []js.Value {
	return each(f.value.Get("sources"))
}

// gains is every GainNode ever made, in the order they were made.
func (f *audioFake) gains() []js.Value {
	return each(f.value.Get("gains"))
}

// buffers is every AudioBuffer ever made, in the order they were made. A
// streamed Voice makes one per chunk, and a prepare that made any at all is a
// prepare that decoded something.
func (f *audioFake) buffers() []js.Value {
	return each(f.value.Get("buffers"))
}

// matrix reads the last four gains back as sound's own Gains[src][out], by the
// splitter output each one is fed from and the merger input each one feeds. That
// mapping is the assertion: a gain wired to the wrong pair would pan a Voice to
// the wrong side while every value in the log still looked right.
func (f *audioFake) matrix() [2][2]float32 {
	f.t.Helper()
	all := f.gains()
	if len(all) < outChannels*outChannels {
		f.t.Fatalf("the graph has %d gain nodes, want at least four", len(all))
	}
	var got [2][2]float32
	var seen [2][2]bool
	for _, gain := range all[len(all)-outChannels*outChannels:] {
		src, out := gain.Get("src"), gain.Get("out")
		if src.Type() != js.TypeNumber || out.Type() != js.TypeNumber {
			f.t.Fatalf("a gain node is not wired between the splitter and the merger: src %v, out %v", src, out)
		}
		got[src.Int()][out.Int()] = float32(gain.Get("gain").Get("value").Float())
		seen[src.Int()][out.Int()] = true
	}
	for src := range outChannels {
		for out := range outChannels {
			if !seen[src][out] {
				f.t.Fatalf("no gain node carries Gains[%d][%d]", src, out)
			}
		}
	}
	return got
}

// panners is how many PannerNodes were ever asked for. It is asserted to be zero
// wherever a Voice is positioned, because the arithmetic is sound's.
func (f *audioFake) panners() int { return f.value.Get("panners").Int() }

// mediaElements is how many HTMLMediaElements, and how many sources made from
// one, this Adapter ever asked the page for. It is asserted to be zero wherever
// a Clip plays: a media element implements its loop as a seek, gates on a
// gesture per element, and runs on its own clock rather than the context's, so
// the gapless promise could not be kept through one.
func (f *audioFake) mediaElements() int { return f.value.Get("mediaElements").Int() }

// renderOffline renders the graph the Adapter built to a stereo pair of frames
// at the context's rate. It is this suite's OfflineAudioContext, for the reason
// the JS beside it gives: node has no Web Audio to render through, and what a
// render needs - the wiring, the samples, and every scheduled start and stop -
// the fake has recorded.
func (f *audioFake) renderOffline(seconds float64) [2][]float32 {
	f.t.Helper()
	rendered := f.value.Call("renderOffline", seconds)
	var out [2][]float32
	for channel := range out {
		values := rendered.Index(channel)
		out[channel] = make([]float32, values.Length())
		for i := range out[channel] {
			out[channel][i] = float32(values.Index(i).Float())
		}
	}
	return out
}

// events is the whole recording, for a failure message that says what did happen
// beside what was wanted.
func (f *audioFake) events() []string {
	values := each(f.value.Get("events"))
	lines := make([]string, len(values))
	for i, value := range values {
		lines[i] = value.String()
	}
	return lines
}

func each(array js.Value) []js.Value {
	values := make([]js.Value, array.Length())
	for i := range values {
		values[i] = array.Index(i)
	}
	return values
}
