package input

import "fmt"

// ErrUnknownKey reports a key name that is neither in the name table nor the
// "#<n>" printed form of an unnamed key. It carries the whole hint, because the
// place it usually surfaces is a JSON decode whose own message says only that a
// string did not fit.
type ErrUnknownKey struct{ Name string }

func (e ErrUnknownKey) Error() string {
	return fmt.Sprintf("input: %q is not a key; use a name such as w, escape or mouse_left, or "+
		"#<n> with cog's own key code — which is not ASCII and not a browser keyCode", e.Name)
}
