package descriptors

import "hash/maphash"

// fingerprintSeed is fixed for the process, so a fingerprint compares only
// against others taken in the same process - which is all a per-frame intern
// needs.
var fingerprintSeed = maphash.MakeSeed()

// FingerprintParams hashes a parameter slice in order by name, kind and value:
// one seed, one encoding per kind, and inline texture and buffer bytes by
// identity, pointer and length, rather than by content, so two slices around
// different runs count as different even when the bytes agree. That is the
// identity the caches key on, which is the point: missing a merge costs a
// batch, and merging two keys that are two cache entries would draw the wrong
// one.
//
// It exists because a recorder that keys a batch on a draw's parameters cannot
// write the comparison itself: ParameterDescr exposes an accessor for some
// kinds and none for others, so a hand-written type switch would silently
// mis-key every kind it forgot - and mis-keying merges two draws that differ,
// which draws the wrong picture rather than costing a batch.
//
// It lives here rather than beside its consumers because the fields it covers
// are unexported, and a fingerprint that a new field can silently fall out of
// is worse than none.
func FingerprintParams(params []ParameterDescr) uint64 {
	var h maphash.Hash
	h.SetSeed(fingerprintSeed)
	for i := range params {
		params[i].fingerprint(&h)
	}
	return h.Sum64()
}
