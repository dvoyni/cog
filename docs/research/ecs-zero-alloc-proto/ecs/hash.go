package ecs

import "fmt"

// Hash is a string reduced to 64 bits so that it can live in a Component.
//
// It is what a Component stores wherever the thing being named was declared as
// a string -- a model, an animation clip, a node, a pass tag, an input action.
// Everything cog declares goes through strings, so one answer serves all of
// them.
//
// The point that decides it over an interned dense index is that **producing a
// Hash needs nothing**. Hashing is a pure function of the string, so a System
// may write one mid-frame, holding no lock but the one it already has on the
// Component:
//
//	if speed > walkThreshold { it.A.Clip = walkClip } else { it.A.Clip = idleClip }
//
// An interned index cannot do that. Interning needs the table that assigns the
// index, so every System that assigns one would have to declare that table, and
// the table would have to be written under a lock -- which is how a naming
// scheme ends up bloating the lock set of every System that merely wants to
// change an animation. A Hash has no table on the writing side at all.
//
// It is also stable across processes and across runs, which a dense index is
// not, so a Hash survives replication and a save file unchanged.
//
// Domain is a phantom type parameter and contributes no field.
//
// It is deliberately *not* the type being hashed. The obvious spelling is
// Hash[string] -- one parameter saying what went in -- but everything cog
// declares goes through strings, so that parameter would have exactly one
// useful instantiation and carry no information at all. Spent on the domain
// instead, the same single parameter stops a clip name being assigned where a
// model name belongs: Hash[Clip] and Hash[Model] are distinct types with
// identical layout, which cog#245 already measured. It is the same complexity
// either way, and one of the two spellings pays for itself.
//
// It is also opt-out rather than opt-in. A codebase that does not want the
// distinction declares one tag and writes Hash[String] everywhere, which is
// exactly the simple form -- no second mechanism, nothing to learn.
type Hash[Domain any] struct{ h uint64 }

// HashOf hashes s. Call it once, into a package-level var, and the per-frame
// cost is a 64-bit copy:
//
//	var walkClip = ecs.HashOf[Clip]("Walk")
func HashOf[Domain any](s string) Hash[Domain] { return Hash[Domain]{h: fnv1a(s)} }

// Hash exposes the raw value, for a consumer keying its own table by it.
func (n Hash[Domain]) Hash() uint64 { return n.h }

// IsZero reports the absent Hash. The zero value is reserved: FNV-1a's offset
// basis is the hash of the empty string, so no string hashes to 0.
func (n Hash[Domain]) IsZero() bool { return n.h == 0 }

func (n Hash[Domain]) String() string { return fmt.Sprintf("hash:%016x", n.h) }

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

// Names is the *consumer's* half, and where it lives is the whole point: it
// belongs to the plugin that resolves names into things -- scene's model table,
// an animation plugin's clip table -- under that plugin's own lock, reached
// once per draw where a lock is held anyway.
//
// There is deliberately no process-wide interner. A shared table that every
// writer touches is shared mutable state needing synchronisation outside the
// scheduler, which is the thing cog's design exists to avoid; here each
// consumer owns its own, and nothing but that consumer ever reads it.
type Names[Domain any, V any] struct {
	byHash map[uint64]hashEntry[V]
}

type hashEntry[V any] struct {
	text  string
	value V
}

func NewNames[Domain any, V any]() *Names[Domain, V] {
	return &Names[Domain, V]{byHash: map[uint64]hashEntry[V]{}}
}

// Register binds a name to a value at registration time, and is also where a
// hash collision is caught.
//
// 64 bits makes a collision vanishingly unlikely -- with ten thousand distinct
// names the probability is about 3e-12 -- but vanishingly unlikely is not
// impossible, and an undetected one would silently draw the wrong model. Since
// every name a consumer can resolve passes through here, catching it costs one
// compare and turns the whole class of failure into a startup error.
func (t *Names[Domain, V]) Register(text string, value V) error {
	h := fnv1a(text)
	if prior, taken := t.byHash[h]; taken && prior.text != text {
		return fmt.Errorf("name hash collision: %q and %q both hash to %016x", prior.text, text, h)
	}
	t.byHash[h] = hashEntry[V]{text: text, value: value}
	return nil
}

// Lookup resolves a Hash to what it was registered for.
func (t *Names[Domain, V]) Lookup(n Hash[Domain]) (V, bool) {
	e, ok := t.byHash[n.h]
	return e.value, ok
}

// TextOf recovers the original string. A Hash is opaque in a debugger, and this
// is what makes it legible again -- for tooling and error messages, not for the
// hot path.
func (t *Names[Domain, V]) TextOf(n Hash[Domain]) (string, bool) {
	e, ok := t.byHash[n.h]
	return e.text, ok
}

func (t *Names[Domain, V]) Len() int { return len(t.byHash) }

// HashFromRaw builds a Hash from a value produced some other way. It exists for
// the comparison in the prototype -- an interner assigning ids has to put them
// in the same field -- and would not be part of a shipped API.
func HashFromRaw[Domain any](raw uint64) Hash[Domain] { return Hash[Domain]{h: raw} }
