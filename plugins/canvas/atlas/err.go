package atlas

import "errors"

// ErrTooLarge is Pack's answer for an image whose cell no page holds: wider or
// taller than a page once rounded up to the atlas grid and its gutter added.
var ErrTooLarge = errors.New("atlas: no page holds the image's cell")

// ErrFull is Pack's answer for an image no page has room for, the atlas at its
// ceiling of pages: its user frees regions it can spare and packs again.
var ErrFull = errors.New("atlas: full at its ceiling of pages")
