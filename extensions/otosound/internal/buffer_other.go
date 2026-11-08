//go:build !darwin && !js

package internal

import "time"

// defaultBufferSize is how much audio the device buffers when Config names
// none. The spike ran 10ms for 12s with zero underruns - 1200 reads, exactly
// 12.000s produced - and it is also the declick ramp's length, because the ramp
// is one block. macOS needs more; see buffer_darwin.go.
const defaultBufferSize = 10 * time.Millisecond
