package ecs

import "fmt"

// Name is a string reduced to 64 bits so that it can live in a Component.
//
// It is what a Component stores wherever the thing being named was declared as
// a string -- a model, an animation clip, a node, a pass tag, an input action.
// Everything cog declares goes through strings, so one answer serves all of
// them.
//
// The point that decides it over an interned dense index is that **producing a
// Name needs nothing**. Hashing is a pure function of the string, so a System
// may write one mid-frame, holding no lock but the one it already has on the
// Component:
//
//	if speed > walkThreshold { it.A.Clip = walkClip } else { it.A.Clip = idleClip }
//
// An interned index cannot do that. Interning needs the table that assigns the
// index, so every System that assigns one would have to declare that table, and
// the table would have to be written under a lock -- which is how a naming
// scheme ends up bloating the lock set of every System that merely wants to
// change an animation. A Name has no table on the writing side at all.
//
// It is also stable across processes and across runs, which a dense index is
// not, so a Name survives replication and a save file unchanged.
//
// Domain is a phantom type parameter and contributes no field. It is what stops
// a clip name being passed where a model name belongs: Name[Clip] and
// Name[Model] are different types with the same layout, and cog#245 measured
// that a generic wrapper's instantiations are genuinely distinct.
type Name[Domain any] struct{ h uint64 }

// NameOf hashes s. Call it once, into a package-level var, and the per-frame
// cost is a 64-bit copy:
//
//	var walkClip = ecs.NameOf[Clip]("Walk")
func NameOf[Domain any](s string) Name[Domain] { return Name[Domain]{h: fnv1a(s)} }

// Hash exposes the raw value, for a consumer keying its own table by it.
func (n Name[Domain]) Hash() uint64 { return n.h }

// IsZero reports the absent Name. The zero value is reserved: FNV-1a's offset
// basis is the hash of the empty string, so no string hashes to 0.
func (n Name[Domain]) IsZero() bool { return n.h == 0 }

func (n Name[Domain]) String() string { return fmt.Sprintf("name:%016x", n.h) }

// fnv1a is FNV-1a over the string's bytes. Any stable 64-bit hash would serve;
// this one needs no dependency and is short enough to audit.
func fnv1a(s string) uint64 {
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	h := uint64(offset)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

// NameTable is the *consumer's* half, and where it lives is the whole point: it
// belongs to the plugin that resolves names into things -- scene's model table,
// an animation plugin's clip table -- under that plugin's own lock, reached
// once per draw where a lock is held anyway.
//
// There is deliberately no process-wide interner. A shared table that every
// writer touches is shared mutable state needing synchronisation outside the
// scheduler, which is the thing cog's design exists to avoid; here each
// consumer owns its own, and nothing but that consumer ever reads it.
type NameTable[Domain any, V any] struct {
	byHash map[uint64]nameEntry[V]
}

type nameEntry[V any] struct {
	text  string
	value V
}

func NewNameTable[Domain any, V any]() *NameTable[Domain, V] {
	return &NameTable[Domain, V]{byHash: map[uint64]nameEntry[V]{}}
}

// Register binds a name to a value at registration time, and is also where a
// hash collision is caught.
//
// 64 bits makes a collision vanishingly unlikely -- with ten thousand distinct
// names the probability is about 3e-12 -- but vanishingly unlikely is not
// impossible, and an undetected one would silently draw the wrong model. Since
// every name a consumer can resolve passes through here, catching it costs one
// compare and turns the whole class of failure into a startup error.
func (t *NameTable[Domain, V]) Register(text string, value V) error {
	h := fnv1a(text)
	if prior, taken := t.byHash[h]; taken && prior.text != text {
		return fmt.Errorf("name hash collision: %q and %q both hash to %016x", prior.text, text, h)
	}
	t.byHash[h] = nameEntry[V]{text: text, value: value}
	return nil
}

// Lookup resolves a Name to what it was registered for.
func (t *NameTable[Domain, V]) Lookup(n Name[Domain]) (V, bool) {
	e, ok := t.byHash[n.h]
	return e.value, ok
}

// TextOf recovers the original string. A Name is opaque in a debugger, and this
// is what makes it legible again -- for tooling and error messages, not for the
// hot path.
func (t *NameTable[Domain, V]) TextOf(n Name[Domain]) (string, bool) {
	e, ok := t.byHash[n.h]
	return e.text, ok
}

func (t *NameTable[Domain, V]) Len() int { return len(t.byHash) }

// NameFromRaw builds a Name from a value produced some other way. It exists for
// the comparison in the prototype -- an interner assigning ids has to put them
// in the same field -- and would not be part of a shipped API.
func NameFromRaw[Domain any](raw uint64) Name[Domain] { return Name[Domain]{h: raw} }
