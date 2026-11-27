package sfx

// ClipState is how far a clip has come, answered beside its ClipInfo by
// ClipTable.Info, so a duration of zero is never read as an answer: zero
// beside ClipLoading is "not yet".
type ClipState uint8

const (
	// ClipLoading is a clip whose prepare is in flight, and also a path
	// nothing has named: nothing failed and nothing is resident. Asking never
	// starts a load; Preload is the verb that changes the answer.
	ClipLoading ClipState = iota
	// ClipReady is a clip installed, its facts known and its voices started
	// from it.
	ClipReady
	// ClipFailed is a clip that could not be read, prepared or installed. It
	// is terminal, and only a release clears it.
	ClipFailed
)
