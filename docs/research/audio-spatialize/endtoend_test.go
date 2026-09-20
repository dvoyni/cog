package main

// THROWAWAY PROTOTYPE - not production code. See doc.go.
//
// The pipeline end to end, on a real clip, with nothing synthetic in it: decode
// the Ogg, prepare it at the device rate, drive the orbit, and look at what
// comes out. Silence and full-scale clipping both sound like a bug that has
// nothing to do with spatialization, so they are ruled out before the
// headphones go on.

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dvoyni/cog/libs/m"
)

func clipsDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "clips")
}

func TestPreparingEveryClipLandsItAtTheDeviceRate(t *testing.T) {
	dir := clipsDir(t)
	for _, c := range clipChoices {
		t.Run(c.file, func(t *testing.T) {
			clip, err := PrepareOgg(filepath.Join(dir, c.file), c.name, deviceRate)
			if err != nil {
				t.Fatal(err)
			}
			if clip.Frames == 0 {
				t.Fatal("decoded to nothing")
			}
			var peak float32
			for _, s := range clip.Samples {
				if a := absf(s); a > peak {
					peak = a
				}
			}
			if peak < 0.01 {
				t.Errorf("decoded near-silence: peak %.5f", peak)
			}
			if peak > 1.5 {
				t.Errorf("decoded well past full scale: peak %.5f", peak)
			}
			t.Logf("%-16s %dch  %6d Hz -> %d Hz  %6.2fs  peak %.3f",
				c.file, clip.Channels, clip.SourceRate, deviceRate, clip.Duration, peak)
		})
	}
}

// TestAnOrbitOfARealClipSweepsTheStereoFieldWithoutClipping is the whole path,
// and the numbers it logs are the ones the listening test is listening for.
func TestAnOrbitOfARealClipSweepsTheStereoFieldWithoutClipping(t *testing.T) {
	// The bell, not the Stirling engine: that recording's first four seconds are
	// genuinely near-silent (RMS 0.0013) because the engine is still winding up,
	// and an invariant measured over silence measures nothing.
	clip, err := PrepareOgg(filepath.Join(clipsDir(t), "bellsmall.ogg"), "bell", deviceRate)
	if err != nil {
		t.Fatal(err)
	}

	mx := NewMixer(deviceRate, deviceChans)
	mx.Start(0, clip, true, 0)

	const (
		blockFrames = deviceRate * bufferMillis / 1000
		blocks      = 400 // 4 seconds, one full orbit
		radius      = 4
	)
	buf := make([]byte, blockFrames*4*deviceChans)
	listener := DefaultListener()

	var peak, minAud, maxAud float32
	minAud = math.MaxFloat32
	var sumL, sumR float64
	var clipped int
	// azimuthAt records where the arithmetic put the source at a few landmarks.
	landmarks := map[string]float32{}

	for b := 0; b < blocks; b++ {
		theta := 2 * math.Pi * float64(b) / blocks
		pos := m.Vec3{
			X: float32(math.Sin(theta)) * radius,
			Z: -float32(math.Cos(theta)) * radius,
		}
		s := Spatialize(Emitter{Positional: true, Position: pos, Falloff: DefaultFalloff(),
			Cone: DefaultCone(), Volume: 0.8}, listener, clip.Channels)

		switch b {
		case 0:
			landmarks["ahead"] = s.Azimuth
		case blocks / 4:
			landmarks["right"] = s.Azimuth
		case blocks / 2:
			landmarks["behind"] = s.Azimuth
		case 3 * blocks / 4:
			landmarks["left"] = s.Azimuth
		}
		if s.Audibility < minAud {
			minAud = s.Audibility
		}
		if s.Audibility > maxAud {
			maxAud = s.Audibility
		}

		mx.SetParams(0, slotParams{Matrix: s.Matrix, Rate: 1})
		mx.Emit()
		if _, err := mx.Read(buf); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < blockFrames; f++ {
			l := math.Float32frombits(binary.LittleEndian.Uint32(buf[f*8:]))
			r := math.Float32frombits(binary.LittleEndian.Uint32(buf[f*8+4:]))
			sumL += float64(l) * float64(l)
			sumR += float64(r) * float64(r)
			if absf(l) > peak {
				peak = absf(l)
			}
			if absf(r) > peak {
				peak = absf(r)
			}
			if absf(l) >= 1 || absf(r) >= 1 {
				clipped++
			}
		}
	}

	n := float64(blocks * blockFrames)
	rmsL, rmsR := math.Sqrt(sumL/n), math.Sqrt(sumR/n)
	t.Logf("orbit: peak %.3f  rmsL %.4f  rmsR %.4f  clipped %d samples", peak, rmsL, rmsR, clipped)
	t.Logf("orbit: audibility ranged %.4f .. %.4f (radius %d, refDistance 1)", minAud, maxAud, radius)
	t.Logf("azimuth landmarks: %+v", landmarks)

	if peak < 0.01 {
		t.Fatal("the orbit produced silence")
	}
	if clipped > 0 {
		t.Errorf("the orbit clipped %d samples at volume 0.8", clipped)
	}
	// At a constant radius, inverse falloff is constant and equal-power panning
	// preserves power, so audibility must not move at all over the orbit. This
	// is the assertion that caught audibility being read off the matrix as
	// max(L, R), which made a centred voice score 0.1414 against a hard-panned
	// one's 0.2000 at the same distance. See Spatialized.Audibility.
	if maxAud-minAud > 1e-6 {
		t.Errorf("audibility moved over a constant-radius orbit: %.4f .. %.4f", minAud, maxAud)
	}
}

// TestAFullOrbitFavoursNeitherEar is the symmetry check, on a stationary dense
// signal rather than on a recording. A real clip's energy is not uniform in
// time, so over one revolution it lands unevenly around the circle and the two
// channels differ by tens of percent for reasons that have nothing to do with
// the arithmetic - which is exactly what happened when this was first written
// against the Stirling engine.
func TestAFullOrbitFavoursNeitherEar(t *testing.T) {
	clip := steadyClip(5, 1)
	mx := NewMixer(deviceRate, deviceChans)
	mx.Start(0, clip, true, 0)

	const (
		blockFrames = deviceRate * bufferMillis / 1000
		blocks      = 400
		radius      = 4
	)
	buf := make([]byte, blockFrames*4*deviceChans)
	var sumL, sumR float64
	for b := 0; b < blocks; b++ {
		theta := 2 * math.Pi * float64(b) / blocks
		pos := m.Vec3{X: float32(math.Sin(theta)) * radius, Z: -float32(math.Cos(theta)) * radius}
		s := Spatialize(Emitter{Positional: true, Position: pos, Falloff: DefaultFalloff(),
			Cone: DefaultCone(), Volume: 0.8}, DefaultListener(), 1)
		mx.SetParams(0, slotParams{Matrix: s.Matrix, Rate: 1})
		mx.Emit()
		mx.Read(buf)
		for f := 0; f < blockFrames; f++ {
			l := math.Float32frombits(binary.LittleEndian.Uint32(buf[f*8:]))
			r := math.Float32frombits(binary.LittleEndian.Uint32(buf[f*8+4:]))
			sumL += float64(l) * float64(l)
			sumR += float64(r) * float64(r)
		}
	}
	n := float64(blocks * blockFrames)
	rmsL, rmsR := math.Sqrt(sumL/n), math.Sqrt(sumR/n)
	t.Logf("orbit symmetry: rmsL %.6f  rmsR %.6f", rmsL, rmsR)
	if d := math.Abs(rmsL-rmsR) / math.Max(rmsL, rmsR); d > 0.01 {
		t.Errorf("a full orbit favoured one ear by %.2f%%: L %.6f R %.6f", d*100, rmsL, rmsR)
	}
}
