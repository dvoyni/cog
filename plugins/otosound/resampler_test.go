//go:build !js

package otosound

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The resampler's tests synthesize their clips rather than reading one: the
// repository holds no 96 kHz clip, and a generated tone is a far better
// aliasing fixture than music, since what is measured is the absence of
// energy at one frequency, and a synthetic source has nothing else there.

// makeTone answers frames of mono samples at rate, the sum of a sine wave of
// amplitude at each of hertz.
func makeTone(rate, frames int, amplitude float64, hertz ...float64) []float32 {
	samples := make([]float32, frames)
	for frame := range samples {
		var sum float64
		for _, frequency := range hertz {
			sum += amplitude * math.Sin(2*math.Pi*frequency*float64(frame)/float64(rate))
		}
		samples[frame] = float32(sum)
	}
	return samples
}

// measureAmplitude measures the amplitude of one frequency in mono samples
// at rate, under a Hann window, so that a frequency the samples hold nothing
// at reads as what is there rather than as what leaked in from the
// frequencies they do hold.
func measureAmplitude(samples []float32, rate int, hertz float64) float64 {
	var real, imaginary, windowSum float64
	length := float64(len(samples))
	for index, sample := range samples {
		window := 0.5 - 0.5*math.Cos(2*math.Pi*float64(index)/length)
		angle := 2 * math.Pi * hertz * float64(index) / float64(rate)
		real += window * float64(sample) * math.Cos(angle)
		imaginary -= window * float64(sample) * math.Sin(angle)
		windowSum += window
	}
	return 2 * math.Hypot(real, imaginary) / windowSum
}

// convertToDecibels expresses one amplitude against another, which is how a
// stopband is read: -60 dB is a thousandth of the tone that folded into it.
func convertToDecibels(amplitude, reference float64) float64 {
	if amplitude <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(amplitude/reference)
}

// A 96 kHz clip against a 48 kHz device holds a whole octave above the
// device's Nyquist, and a conversion that does not lowpass folds all of it
// back into the band a listener hears best: a 30 kHz partial arrives at
// 18 kHz at full amplitude. Linear interpolation fails this outright rather
// than narrowly: at an exact 2:1 ratio every output frame lands on an input
// frame, so the interpolation never runs and the conversion is plain
// decimation.
func TestTheOfflineResamplerDecimatesWithoutFoldingTheBandAboveNyquist(t *testing.T) {
	const sourceRate, outputRate, frames = 96000, 48000, 24000
	const passband, stopband, alias = 1000.0, 30000.0, 18000.0
	converted := convertRate(makeTone(sourceRate, frames, 0.5, passband, stopband), 1, sourceRate, outputRate)
	if want := frames * outputRate / sourceRate; len(converted) != want {
		t.Fatalf("decimating %d frames produced %d, want %d", frames, len(converted), want)
	}

	// The filter's own edges are the head and the tail, where the window
	// reads silence past the clip's ends; what is measured is the middle.
	measured := converted[1000 : len(converted)-1000]
	kept := convertToDecibels(measureAmplitude(measured, outputRate, passband), 0.5)
	folded := convertToDecibels(measureAmplitude(measured, outputRate, alias), 0.5)
	t.Logf("the %.0f Hz tone is kept at %.2f dB, and the %.0f Hz one folds to %.0f Hz at %.2f dB", passband, kept, stopband, alias, folded)
	if kept < -0.5 {
		t.Fatalf("the %.0f Hz tone came through at %.2f dB, and the passband must pass", passband, kept)
	}
	if folded > -60 {
		t.Fatalf("the %.0f Hz tone folded to %.0f Hz at %.2f dB, and the band above the new Nyquist must be empty", stopband, alias, folded)
	}
}

// The case the decoded tier meets every run: the fixture is 44.1 kHz and the
// default device 48 kHz, so it converts upwards. Nothing folds when
// upsampling, there being nothing above the source's Nyquist to fold, so what
// is asserted is that the passband keeps its own amplitude.
func TestTheOfflineResamplerUpsamplesWithoutChangingTheBandItCarries(t *testing.T) {
	const sourceRate, outputRate, frames = 44100, 48000, 22050
	const passband = 1000.0
	converted := convertRate(makeTone(sourceRate, frames, 0.5, passband), 1, sourceRate, outputRate)

	kept := measureAmplitude(converted[1000:len(converted)-1000], outputRate, passband)
	if level := convertToDecibels(kept, 0.5); level < -0.5 || level > 0.5 {
		t.Fatalf("a %.0f Hz tone upsampled to %.4f, which is %.2f dB of what it was", passband, kept, level)
	}
}

// A streamed voice's read-ahead converts its clip a few thousand frames at a
// time, and a clip must not sound different for being long enough to stream.
// The resampler is therefore one incremental filter, and fed in chunks of
// any size, Prepare's among them, it drains the same frames as fed whole,
// not merely similar ones.
func TestAChunkedConversionIsSampleForSampleTheWholeOne(t *testing.T) {
	const sourceRate, outputRate, frames = 44100, 48000, 8000
	source := makeTone(sourceRate, frames, 0.4, 440, 3000)
	// convertInChunks feeds source chunkFrames at a time, draining all it
	// can after each, through a buffer smaller than a chunk converts to.
	convertInChunks := func(chunkFrames int) []float32 {
		converter := newResampler(newSincFilter(sourceRate, outputRate), 1)
		var converted []float32
		buffer := make([]float32, 512)
		drainAll := func(inputEnded bool) {
			for {
				writtenFrames := converter.drain(buffer, inputEnded)
				converted = append(converted, buffer[:writtenFrames]...)
				if writtenFrames < len(buffer) {
					return
				}
			}
		}
		for chunkStart := 0; chunkStart < frames; chunkStart += chunkFrames {
			converter.feed(source[chunkStart:min(chunkStart+chunkFrames, frames)])
			drainAll(false)
		}
		drainAll(true)
		return converted
	}

	whole := convertInChunks(frames)
	for name, converted := range map[string][]float32{
		"in 333-frame chunks": convertInChunks(333),
		"by convertRate":      convertRate(source, 1, sourceRate, outputRate),
	} {
		if len(converted) != len(whole) {
			t.Fatalf("the conversion %s produced %d frames and the whole one %d", name, len(converted), len(whole))
		}
		for frame := range whole {
			if whole[frame] != converted[frame] {
				t.Fatalf("frame %d is %v converted %s and %v whole, and a clip must not sound different for being streamed",
					frame, converted[frame], name, whole[frame])
			}
		}
	}
}

// The expensive resampler runs only off the device thread: outside its own
// file, nothing names any of resampler.go's declarations but prepareClip and
// retainClip, which run on a prepare goroutine, the prepared clip, which
// holds the filter a streamed clip's voices share, and a streamed voice's
// read-ahead, which runs on a goroutine of its own.
func TestTheResamplerIsReachableOnlyFromPrepareAndTheReadAhead(t *testing.T) {
	allowed := []string{"prepareClip", "retainClip", "preparedClip", "readAhead.stream"}
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if files[name], err = parser.ParseFile(fileSet, name, nil, 0); err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
	}

	declared := map[string]bool{}
	for _, declaration := range files["resampler.go"].Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			// A method is reached only through a value of its type, which
			// the type's own name covers.
			if declaration.Recv == nil {
				declared[declaration.Name.Name] = true
			}
		case *ast.GenDecl:
			for _, spec := range declaration.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					declared[spec.Name.Name] = true
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						declared[name.Name] = true
					}
				}
			}
		}
	}
	if !declared["convertRate"] {
		t.Fatal("resampler.go declares no convertRate, which Prepare converts a clip with")
	}

	reachedFromPrepare := false
	for name, file := range files {
		if name == "resampler.go" {
			continue
		}
		for _, declaration := range file.Decls {
			// A method is named as its receiver's type and its own name,
			// such as readAhead.stream, so one can be allowed too, and a type
			// as its own name, each being declared on its own.
			function := "a declaration"
			if typeDeclaration, isGeneral := declaration.(*ast.GenDecl); isGeneral && typeDeclaration.Tok == token.TYPE {
				function = typeDeclaration.Specs[0].(*ast.TypeSpec).Name.Name
			}
			if functionDeclaration, isFunction := declaration.(*ast.FuncDecl); isFunction {
				function = functionDeclaration.Name.Name
				if functionDeclaration.Recv != nil {
					receiver := functionDeclaration.Recv.List[0].Type
					if pointer, isPointer := receiver.(*ast.StarExpr); isPointer {
						receiver = pointer.X
					}
					function = fmt.Sprintf("%v.%s", receiver, function)
				}
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				identifier, isIdentifier := node.(*ast.Ident)
				if !isIdentifier || !declared[identifier.Name] {
					return true
				}
				if !slices.Contains(allowed, function) {
					t.Errorf("%v: %s names the resampler's %s, and only Prepare and the read-ahead may reach it",
						fileSet.Position(identifier.Pos()), function, identifier.Name)
				} else {
					reachedFromPrepare = true
				}
				return true
			})
		}
	}
	if !reachedFromPrepare {
		t.Error("prepareClip names nothing of the resampler's, so no clip is converted")
	}
}
