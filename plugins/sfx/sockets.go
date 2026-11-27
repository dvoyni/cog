package sfx

// BackendSocket is the backend seam: a dumb, allocation-free sink with a fixed
// table of slots. Exactly one plugin or adapter binds it in an Init, with
// configurator.BindSocket[sfx.BackendSocket]: otosound, or the headless
// backend. A kernel with none panics in Spawn.
//
// sfx calls it from its Start and from the flush alone, one call at a time, so
// a backend needs no lock of its own against sfx's calls; whatever else it
// runs, such as a mixer on the device's thread, is its own to guard. Nothing
// is pushed: every answer is polled at the flush, and nothing is polled about
// voices, since sfx computes every ending from a prepared clip's facts.
type BackendSocket interface {
	// Voices tells the backend how many slots exist. Called once, in sfx's
	// Start, before any Emit. Slots are numbered [0, n), and sfx stops a slot
	// before it reuses one.
	Voices(n int)

	// Emit applies one flush's operations. changes is owned by sfx and is
	// valid only for the duration of the call.
	Emit(changes *FlushChanges)

	// Device reports the device as it is now, Ready included. Polled once per
	// flush; it is a field read, not a query.
	Device() Device

	// Prepare turns encoded Ogg bytes into something voices can be started
	// from: samples for a short clip, the bytes and a way to stream them for a
	// long one. Which is the backend's business. The bytes are the backend's
	// from the call on; sfx keeps none. done=true means prepared is valid now;
	// done=false means it will appear in TakePrepared under the same token. The
	// prepared value must be garbage-collectable.
	Prepare(token any, encoded []byte) (prepared PreparedClip, done bool, err error)

	// TakePrepared answers the prepares that finished since the last call, and
	// clears them.
	TakePrepared() []Prepared

	// Install mints the ID of a prepared clip. It is Install rather than
	// Prepare that mints, so the ID comes into existence at sfx's flush, where
	// the destroy that pairs with it is guaranteed to be statable.
	Install(clip PreparedClip) (ClipID, error)
}
