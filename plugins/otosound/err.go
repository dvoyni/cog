//go:build !js

package otosound

import "errors"

// errNotOurClip is Install handed a prepared clip otosound did not make. sfx
// installs only what otosound's own Prepare produced, so it guards against a
// future caller rather than a failure any composition can have.
var errNotOurClip = errors.New("otosound: Install was handed a prepared clip otosound did not make")
