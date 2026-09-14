package input

import "github.com/dvoyni/cog/bundles/input/internal/types"

// ErrUnknownKey reports a key name that is neither in the name table nor the
// "#<n>" printed form of an unnamed key. It carries the whole hint, because the
// place it usually surfaces is a JSON decode whose own message says only that a
// string did not fit.
type ErrUnknownKey = types.ErrUnknownKey
