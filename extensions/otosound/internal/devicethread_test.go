//go:build !js

package internal

import (
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"testing"
)

// deviceThread is the half of this package the device thread runs. It is named
// here, in a test, because the rule it keeps is the kind of invariant that
// erodes one convenient call at a time and nothing in the language enforces it.
//
// clipdata.go is deliberately not on the list. What the device thread touches
// there is the prepared Clip data it was given - fields, and one method that is
// arithmetic over them - which is exactly what the rule allows; the decode
// beside it is reached only from the goroutine Prepare spawns.
//
// streamring.go is on it, and stream.go beside it is not. That is the streamed
// tier's whole shape in one line: the device thread may touch a read-ahead
// ring, which is a buffer and two atomic counters, and may not touch the
// decoder, the goroutine or the resampler that fill it. A Voice is handed the
// ring and never the stream for exactly this reason.
var deviceThread = []string{"mixer.go", "voice.go", "ring.go", "batch.go", "streamring.go"}

// deviceThreadImports is everything those files may import. sound is on it for
// its types alone - VoiceSlot, VoiceParams, ClipID - and what the rule forbids
// is a call into sound, which no import list can see. Everything an import list
// can see is here: no decoder, no oto, no kernel, and no sync but sync/atomic,
// because the device thread may reach the ring through atomics and may take no
// lock at all.
var deviceThreadImports = []string{
	"encoding/binary",
	"math",
	"sync/atomic",
	"time",
	"github.com/dvoyni/cog/slots/sound",
}

// Nothing decodes on the thread that fills the device buffer, and that thread
// allocates nothing, takes no lock, frees nothing and calls into neither sound
// nor kernel. The allocation half is measured by AllocsPerRun over a full
// block; this is the half a measurement cannot reach, so it is asserted
// structurally: the decode path has no call site in these files because the
// packages that could decode are not imported by them.
func TestTheDeviceThreadReachesNoDecoderNoLockAndNoKernel(t *testing.T) {
	fileSet := token.NewFileSet()
	for _, name := range deviceThread {
		parsed, err := parser.ParseFile(fileSet, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("%s imports %s, which is not a path", name, imported.Path.Value)
			}
			if !slices.Contains(deviceThreadImports, path) {
				t.Fatalf("%s imports %q, and the device thread may not allocate, "+
					"lock, free, decode or call into sound or kernel", name, path)
			}
		}
	}
}
