package render

import "github.com/dvoyni/cog/gfx"

// ReadbackStatus is how a readback ended, carried by ReadbackEvent, each
// status gfx's map status of the same meaning.
type ReadbackStatus uint8

const (
	// ReadbackStatusReady is a readback whose bytes Frame.ReadbackData
	// answers.
	ReadbackStatusReady = ReadbackStatus(gfx.MapStatusMapped)
	// ReadbackStatusFailed is a readback holding no bytes: its staging
	// buffer's map failed, or the device was lost before it ended; the error
	// was logged.
	ReadbackStatusFailed = ReadbackStatus(gfx.MapStatusFailed)
)
