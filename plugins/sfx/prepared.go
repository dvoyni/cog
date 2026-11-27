package sfx

// Prepared is one finished prepare, drained at the top of a flush through
// BackendSocket.TakePrepared.
type Prepared struct {
	// Token is the value sfx handed to Prepare, and is how a completion finds
	// the entry it belongs to, or fails to when that entry was released while
	// the prepare was in flight.
	Token any
	// Clip is the prepared clip, nil when Err is set.
	Clip PreparedClip
	// Err is why the prepare failed, which fails the clip for good.
	Err error
}
