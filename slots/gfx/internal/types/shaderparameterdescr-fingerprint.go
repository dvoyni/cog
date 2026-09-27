package types

import (
	"encoding/binary"
	"hash/maphash"
)

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
// It exists because a recorder that keys a batch on a draw's parameters would
// otherwise write the comparison itself, and a hand-written type switch would
// silently mis-key every kind or field it forgot - and mis-keying merges two
// draws that differ, which draws the wrong picture rather than costing a batch.
func FingerprintParams(params []ShaderParameterDescr) uint64 {
	var h maphash.Hash
	h.SetSeed(fingerprintSeed)
	for i := range params {
		params[i].fingerprint(&h)
	}
	return h.Sum64()
}

func (p *ShaderParameterDescr) fingerprint(h *maphash.Hash) {
	h.WriteString(p.Name)
	writeUint(h, uint64(p.Kind))
	switch p.Kind {
	case ShaderParameterKindTexture:
		// The three cases are disjoint, so the id, the path and the blob say
		// between them which one this is: there is no source term to fold in.
		t := &p.Texture
		writeUint(h, uint64(t.Params.ID))
		h.WriteString(t.Name)
		writeUint(h, uint64(t.Params.Width)|uint64(t.Params.Height)<<32)
		writeUint(h, uint64(t.Params.Format)|boolBit(t.Params.Mipmaps)<<8)
		maphash.WriteComparable(h, t.Blob)
	case ShaderParameterKindBuffer:
		b := &p.Buffer
		writeUint(h, uint64(b.ID))
		writeUint(h, uint64(b.Size))
		maphash.WriteComparable(h, b.Bytes)
		writeUint(h, uint64(p.BufferOffset)|uint64(p.BufferSize)<<32)
	case ShaderParameterKindRaw, ShaderParameterKindFloat, ShaderParameterKindVec4,
		ShaderParameterKindMat4, ShaderParameterKindColor:
		// The kind is already hashed above, which is what keeps a color and a
		// vec4 of the same components apart: they are the same bytes, and a
		// fingerprint that merged them would merge two batches that inspect
		// differently.
		if p.Raw.Len() > 0 {
			// Bytes held out of line hash by identity, as every inline run
			// does.
			maphash.WriteComparable(h, p.Raw)
		} else {
			h.Write(p.Small[:p.SmallLen])
		}
	case ShaderParameterKindSampler:
		s := &p.Sampler
		writeUint(h, uint64(s.AddressU)|uint64(s.AddressV)<<8|uint64(s.Mag)<<16|
			uint64(s.Min)<<24|uint64(s.Mip)<<32|uint64(s.Anisotropy)<<40|
			boolBit(s.Comparison)<<48|uint64(s.Compare)<<56)
	}
}

func writeUint(h *maphash.Hash, value uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], value)
	h.Write(buf[:])
}

func boolBit(value bool) uint64 {
	if value {
		return 1
	}
	return 0
}
