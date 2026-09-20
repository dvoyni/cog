package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// This file is otosound's half: the sink beneath #376's seam. It is told a slot
// count once, it is handed one Batch per tick across #302's SPSC ring, and it
// knows nothing about positions, listeners, buses or the opaque Voice handle -
// only slots, a 2x2 gain matrix, a rate, and a paused flag.
//
// The device-thread rule #302 states is obeyed here on purpose, because
// breaking it is how a prototype flatters a design: Read allocates nothing,
// locks nothing, frees nothing and calls nothing above the seam.

import (
	"encoding/binary"
	"io"
	"math"
	"sync/atomic"
)

const (
	// maxVoices is the slot count the seam is told once. #301 makes this
	// sound.Config.MaxVoices; the prototype never gets near it.
	maxVoices = 16
	// ringDepth is #302's, and about 130 ms at 60 Hz.
	ringDepth = 8
)

// Interp is the mixer's pitch interpolator, exposed as a knob because item 4
// asks whether pitch by resampling is acceptable and "acceptable" depends
// entirely on which one you picked. The base rate conversion is not done here;
// see clip.go.
type Interp int32

const (
	InterpLinear Interp = iota
	InterpCubic
)

func (i Interp) String() string {
	if i == InterpCubic {
		return "catmull-rom"
	}
	return "linear"
}

// slotParams is everything about one voice that crosses the seam. Positions
// are conspicuously absent, which is the whole of #376.
type slotParams struct {
	Matrix  Matrix
	Rate    float32
	Paused  bool
	Downmix bool // A/B only: sum a stereo source to mono before the matrix
	Set     bool
}

type opKind uint8

const (
	opStart opKind = iota
	opStop
)

// op is a discrete thing that must not be lost. #302's whole argument for a
// delta ring over gfx's latest-wins triple buffer is that dropping one of these
// silently swallows a Play.
type op struct {
	kind  opKind
	slot  int
	clip  *PreparedClip
	loop  bool
	start float64 // starting frame
}

// Batch is one tick's writes: ops in order, parameters coalesced to the last
// value. It is preallocated and reused; #302 makes it owned by sound and dead
// when Emit returns, which here means dead once the mixer has drained it.
type Batch struct {
	ops    []op
	params [maxVoices]slotParams
}

func (b *Batch) reset() {
	b.ops = b.ops[:0]
	for i := range b.params {
		b.params[i] = slotParams{}
	}
}

// merge folds another batch into this one: ops append in order, parameters take
// the later value. This is what a full ring does instead of blocking or
// dropping.
func (b *Batch) merge(other *Batch) {
	b.ops = append(b.ops, other.ops...)
	for i := range other.params {
		if other.params[i].Set {
			b.params[i] = other.params[i]
		}
	}
}

// voice is the mixer's own slot. cur trails tgt by one block, which is the
// declick.
type voice struct {
	clip   *PreparedClip
	pos    float64
	loop   bool
	active bool
	paused bool

	cur     Matrix // what the last block ended at
	tgt     Matrix // what the tick asked for
	rate    float32
	downmix bool

	// fading marks a slot that was stopped this block: it keeps mixing to the
	// end of the block with its matrix ramped to zero, so a stop is a ramp
	// rather than a step. Without this, every Stop is a click, and hearing that
	// is one of the things the prototype is for.
	fading bool
}

// Mixer is the io.Reader oto pulls. One instance per device.
type Mixer struct {
	rate     int
	channels int

	voices [maxVoices]voice

	// The ring. write is published with an atomic store after the batch is
	// filled; read is the mixer's own and is stored atomically only so the tick
	// can see how far the device has got.
	ring    [ringDepth]*Batch
	write   atomic.Uint64
	read    atomic.Uint64
	staging *Batch // the tick's own, never touched by the device thread

	// applied is #302's counter: the tick may free what the mixer has passed.
	applied atomic.Uint64

	// Knobs and readouts, all atomic because they are read on the device thread.
	interp   atomic.Int32
	declick  atomic.Bool
	reads    atomic.Uint64
	frames   atomic.Uint64
	maxBlock atomic.Uint64
	peak     atomic.Uint64 // float32 bits of the loudest output sample seen
	clipped  atomic.Uint64
	// active is a bitmask of the slots that were still sounding at the end of
	// the last block, so the tick can say whether anything is playing without
	// reaching into the device thread's voice table.
	active atomic.Uint32
}

func NewMixer(rate, channels int) *Mixer {
	mx := &Mixer{rate: rate, channels: channels}
	for i := range mx.ring {
		mx.ring[i] = &Batch{ops: make([]op, 0, 32)}
	}
	mx.staging = &Batch{ops: make([]op, 0, 32)}
	mx.declick.Store(true)
	return mx
}

func (mx *Mixer) SetInterp(i Interp) { mx.interp.Store(int32(i)) }
func (mx *Mixer) Interp() Interp     { return Interp(mx.interp.Load()) }
func (mx *Mixer) SetDeclick(on bool) { mx.declick.Store(on) }
func (mx *Mixer) Declick() bool      { return mx.declick.Load() }
func (mx *Mixer) Reads() uint64      { return mx.reads.Load() }
func (mx *Mixer) FramesOut() uint64  { return mx.frames.Load() }
func (mx *Mixer) MaxBlock() uint64   { return mx.maxBlock.Load() }
func (mx *Mixer) Clipped() uint64    { return mx.clipped.Load() }
func (mx *Mixer) Peak() float32      { return math.Float32frombits(uint32(mx.peak.Load())) }
func (mx *Mixer) ResetPeak()         { mx.peak.Store(0); mx.clipped.Store(0) }
func (mx *Mixer) Pending() int       { return int(mx.write.Load() - mx.read.Load()) }

// Active reports whether a slot was still sounding at the end of the last
// block.
func (mx *Mixer) Active(slot int) bool { return mx.active.Load()&(1<<uint(slot)) != 0 }

// ---- the tick side of the seam -------------------------------------------

// Start records a play into the staging batch.
func (mx *Mixer) Start(slot int, clip *PreparedClip, loop bool, startFrame float64) {
	mx.staging.ops = append(mx.staging.ops, op{kind: opStart, slot: slot, clip: clip, loop: loop, start: startFrame})
}

// Stop records a stop.
func (mx *Mixer) Stop(slot int) {
	mx.staging.ops = append(mx.staging.ops, op{kind: opStop, slot: slot})
}

// SetParams coalesces one slot's parameters to the last value this tick.
func (mx *Mixer) SetParams(slot int, p slotParams) {
	p.Set = true
	mx.staging.params[slot] = p
}

// Emit publishes the staging batch. #376's one call per tick.
//
// A full ring merges into staging rather than blocking or dropping: the tick
// keeps accumulating and publishes when there is room. That path is the dying
// device, not a hot one.
func (mx *Mixer) Emit() {
	w, r := mx.write.Load(), mx.read.Load()
	if w-r >= ringDepth {
		return // staging keeps accumulating; nothing is lost
	}
	slot := mx.ring[w%ringDepth]
	slot.reset()
	slot.merge(mx.staging)
	mx.staging.reset()
	mx.write.Store(w + 1) // the publish
}

// ---- the device side of the seam ------------------------------------------

// Read is the device thread. Nothing in here allocates, locks, frees, or calls
// anything above the seam.
func (mx *Mixer) Read(buf []byte) (int, error) {
	frames := len(buf) / (4 * mx.channels)
	if frames == 0 {
		return 0, nil
	}

	mx.drain()

	declick := mx.declick.Load()
	interp := Interp(mx.interp.Load())
	inv := float32(1) / float32(frames)

	var peak float32
	var clipped uint64

	for f := 0; f < frames; f++ {
		var outL, outR float32
		// t is where in the block we are, which is the declick ramp's parameter.
		// It runs (f+1)/frames rather than f/frames so the block ends exactly on
		// the target, which is what makes the end-of-block cur = tgt below true
		// rather than nearly true - and what makes a stop reach real silence.
		t := float32(f+1) * inv
		if !declick {
			t = 1
		}

		for v := range mx.voices {
			vo := &mx.voices[v]
			if !vo.active || vo.paused {
				continue
			}
			ch := vo.clip.Channels
			var sl, sr float32
			if ch == 1 {
				s := vo.sample(0, 1, interp)
				sl, sr = s, s
			} else {
				sl = vo.sample(0, 2, interp)
				sr = vo.sample(1, 2, interp)
				if vo.downmix {
					mono := (sl + sr) * 0.5
					sl, sr = mono, mono
				}
			}

			// The ramp. cur -> tgt across the block, which is the only thing
			// standing between a per-tick parameter change and a click.
			m00 := vo.cur[0][0] + (vo.tgt[0][0]-vo.cur[0][0])*t
			m01 := vo.cur[0][1] + (vo.tgt[0][1]-vo.cur[0][1])*t
			if ch == 1 || vo.downmix {
				outL += sl * m00
				outR += sr * m01
			} else {
				m10 := vo.cur[1][0] + (vo.tgt[1][0]-vo.cur[1][0])*t
				m11 := vo.cur[1][1] + (vo.tgt[1][1]-vo.cur[1][1])*t
				outL += sl*m00 + sr*m10
				outR += sl*m01 + sr*m11
			}

			vo.pos += float64(vo.rate)
			end := float64(vo.clip.Frames)
			if vo.pos >= end {
				if vo.loop {
					for vo.pos >= end {
						vo.pos -= end
					}
				} else {
					vo.active = false
				}
			}
		}

		if a := absf(outL); a > peak {
			peak = a
		}
		if a := absf(outR); a > peak {
			peak = a
		}
		if outL > 1 || outL < -1 || outR > 1 || outR < -1 {
			clipped++
		}
		outL = clamp32(outL, -1, 1)
		outR = clamp32(outR, -1, 1)

		o := f * 4 * mx.channels
		binary.LittleEndian.PutUint32(buf[o:], math.Float32bits(outL))
		binary.LittleEndian.PutUint32(buf[o+4:], math.Float32bits(outR))
	}

	// End of block: the ramp has arrived, and a slot that was fading is done.
	var active uint32
	for v := range mx.voices {
		vo := &mx.voices[v]
		vo.cur = vo.tgt
		if vo.fading {
			vo.active, vo.fading, vo.clip = false, false, nil
		}
		if vo.active {
			active |= 1 << uint(v)
		}
	}
	mx.active.Store(active)

	mx.reads.Add(1)
	mx.frames.Add(uint64(frames))
	if uint64(frames) > mx.maxBlock.Load() {
		mx.maxBlock.Store(uint64(frames))
	}
	if b := uint64(math.Float32bits(peak)); peak > mx.Peak() {
		mx.peak.Store(b)
	}
	mx.clipped.Add(clipped)
	return frames * 4 * mx.channels, nil
}

// drain applies every published batch, whole, at this block's boundary. #302's
// "applied whole at the next block start" is this loop.
func (mx *Mixer) drain() {
	w := mx.write.Load()
	r := mx.read.Load()
	for ; r < w; r++ {
		b := mx.ring[r%ringDepth]
		for _, o := range b.ops {
			vo := &mx.voices[o.slot]
			switch o.kind {
			case opStart:
				vo.clip, vo.pos, vo.loop = o.clip, o.start, o.loop
				vo.active, vo.paused, vo.fading = true, false, false
				// A start begins at its target matrix rather than ramping up
				// from silence: a one-shot that fades in over 10 ms loses its
				// transient, which is the thing you were playing it for.
				vo.cur = vo.tgt
			case opStop:
				if vo.active {
					// Ramp to silence over this block instead of cutting.
					vo.tgt = Matrix{}
					vo.fading = true
				}
			}
		}
		for i := range b.params {
			if !b.params[i].Set {
				continue
			}
			vo := &mx.voices[i]
			if vo.fading {
				continue // a stop already claimed this block
			}
			vo.tgt = b.params[i].Matrix
			vo.rate = b.params[i].Rate
			vo.paused = b.params[i].Paused
			vo.downmix = b.params[i].Downmix
		}
		mx.applied.Add(1)
	}
	mx.read.Store(r)
}

// sample reads one channel at the voice's fractional position.
func (vo *voice) sample(ch, stride int, interp Interp) float32 {
	s := vo.clip.Samples
	n := vo.clip.Frames
	i := int(vo.pos)
	frac := float32(vo.pos - float64(i))

	at := func(k int) float32 {
		if k < 0 {
			if vo.loop {
				k += n
			} else {
				k = 0
			}
		} else if k >= n {
			if vo.loop {
				k -= n
			} else {
				k = n - 1
			}
		}
		return s[k*stride+ch]
	}

	if interp == InterpCubic {
		p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
		// Catmull-Rom.
		return p1 + 0.5*frac*((p2-p0)+frac*((2*p0-5*p1+4*p2-p3)+frac*(3*(p1-p2)+p3-p0)))
	}
	return at(i) + (at(i+1)-at(i))*frac
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

var _ io.Reader = (*Mixer)(nil)
