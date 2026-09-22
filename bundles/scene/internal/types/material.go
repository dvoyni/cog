package types

import (
	"encoding/binary"
	"hash/maphash"

	"github.com/dvoyni/cog/slots/gfx"
)

// MaterialTag binds one pass tag to the gfx material that serves it.
//
// A tag entry is a whole gfx.MaterialDescr rather than a shader, because two
// independent things vary per tag. Pipeline state is strictly per material with
// no pass or draw override, so a shadow pass takes its cull mode from its own
// entry; and a declared-but-unused WGSL binding is still reflected and must be
// bound, so the parameter set is tag-specific too — an alphaMode MASK shadow
// shader declares baseColorTexture and alphaCutoff, an opaque one declares
// neither.
type MaterialTag struct {
	Tag   PassTag // zero reads as TagForward
	Descr gfx.MaterialDescr
}

// Material is a scene material: the gfx materials it serves, one per pass tag.
// A pass whose tag has no entry skips every draw using this material, so tag
// participation is purely a material property — a draw gets no say in which
// passes it appears in.
//
// A nil Material is the bundled PBR, so every draw literal that omits the field
// is untouched, and the hand-written one-entry case is
// Material{{Descr: descr}}. In v1 the only tag is forward; when shadows land
// they add a shadow entry to that same value and every draw that passed nil
// gains shadow casting with no call-site change.
type Material []MaterialTag

// tag reads an unwritten entry tag as the forward pass, matching Pass.
func (t MaterialTag) tag() PassTag {
	if t.Tag == "" {
		return TagForward
	}
	return t.Tag
}

// MaterialKey identifies one Material by content: a fingerprint of each entry's
// tag and gfx material - shader source-or-path, pipeline state and parameter
// bytes. A caller-supplied gfx.MaterialDescr has no id of its own, and keying
// by the slice's backing instead would hand the spec's own idiom,
// Material{{Descr: descr}} built inline per draw, a fresh id every draw and so
// never sort two of them together.
//
// Zero is never a key. It is what a draw record carries when nothing keyed its
// material at record, which tells the flush to key it there.
type MaterialKey uint64

var materialSeed = maphash.MakeSeed()

// MaterialKeyOf keys material by content. A fingerprint that lands on zero is
// read as one, which folds it into the same one-in-2^64 collision class every
// other key already risks.
func MaterialKeyOf(material Material) MaterialKey {
	var h maphash.Hash
	h.SetSeed(materialSeed)
	var buf [8]byte
	for i := range material {
		h.WriteString(string(material[i].tag()))
		binary.LittleEndian.PutUint64(buf[:], material[i].Descr.Fingerprint())
		h.Write(buf[:])
	}
	if key := MaterialKey(h.Sum64()); key != 0 {
		return key
	}
	return 1
}
