package ecs

import "fmt"

// HashKey is the constraint on the named type a hash is stored as.
//
// The shape came out of review and is better than the wrapper struct it
// replaces. A caller declares the type in one line and the hash *is* that type:
//
//	type ClipHash uint64
//	var walkClip = ecs.HashOf[ClipHash]("Walk")
//
// So there is no wrapper to unwrap and no accessor to call -- a ClipHash is a
// plain named integer, directly comparable, printable, usable as a map key, and
// pointer-free by construction because the constraint admits nothing else.
//
// It also carries the domain without a second type parameter. ClipHash and
// ModelHash are distinct types, so a clip name cannot be assigned where a model
// name belongs, and the type that narrows is the type that is stored. A
// codebase that does not want the distinction declares one such type and uses
// it everywhere, which is the simple form with nothing extra to learn.
//
// ~uint64 rather than ~int: a hash is a bit pattern rather than a number, and
// int is 32 bits on some platforms, which would make the same string hash
// differently depending on where the game was built -- and the whole value of a
// hash over an interned index is that it is the same everywhere.
type HashKey interface{ ~uint64 }

// HashOf hashes s into the named type K. Call it once, into a package-level
// var, and the per-frame cost is a 64-bit copy.
//
// The property that decides a hash over an interned index is that **producing
// one needs nothing**: hashing is a pure function, so a System may write a name
// mid-frame holding no lock but the one it already has on the Component. An
// interned index cannot, because interning is what *assigns* the index, so the
// table would join the lock set of every System that ever changed an animation.
func HashOf[K HashKey](s string) K { return K(fnv1a(s)) }

// HashBytesOf is HashOf over bytes, for a name that did not arrive as a string.
func HashBytesOf[K HashKey](b []byte) K { return K(fnv1aBytes(b)) }

// NoHash is the absent value. FNV-1a's offset basis is the hash of the empty
// string, so no input hashes to 0 and the zero value is free to mean "none".
const NoHash = 0

const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

// fnv1a is FNV-1a. Any stable 64-bit hash would serve; this one needs no
// dependency and is short enough to audit.
func fnv1a(s string) uint64 {
	h := uint64(fnvOffset)
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= fnvPrime
	}
	return h
}

func fnv1aBytes(b []byte) uint64 {
	h := uint64(fnvOffset)
	for _, c := range b {
		h ^= uint64(c)
		h *= fnvPrime
	}
	return h
}

// Names is the *consumer's* half, and where it lives is the whole point: it
// belongs to the plugin that resolves names into things -- scene's model table,
// an animation plugin's clip table -- under that plugin's own lock, reached
// once per draw where a lock is held anyway.
//
// There is deliberately no process-wide interner. A shared table every writer
// touches is shared mutable state needing synchronisation outside the
// scheduler, which is the thing cog's design exists to avoid; here each
// consumer owns its own and nothing else ever reads it.
//
// Nothing cleans this table because nothing accumulates in it: it holds what
// the consumer *registered*, which is its asset manifest, and asking it a
// question never adds to it. That is a property of a manifest, not of hashing
// in general -- per-entity *data* is a different problem with a different
// answer, and it is in sidestore.go.
type Names[K HashKey, V any] struct {
	byHash map[K]hashEntry[V]
}

type hashEntry[V any] struct {
	text  string
	value V
}

func NewNames[K HashKey, V any]() *Names[K, V] {
	return &Names[K, V]{byHash: map[K]hashEntry[V]{}}
}

// Register binds a name to a value at registration time, and is also where a
// hash collision is caught.
//
// 64 bits makes a collision vanishingly unlikely -- with ten thousand distinct
// names the probability is about 3e-12 -- but vanishingly unlikely is not
// impossible, and an undetected one would silently draw the wrong model. Since
// every name a consumer can resolve passes through here, catching it costs one
// compare and turns the whole class of failure into a startup error.
func (t *Names[K, V]) Register(text string, value V) error {
	h := HashOf[K](text)
	if prior, taken := t.byHash[h]; taken && prior.text != text {
		return fmt.Errorf("name hash collision: %q and %q both hash to %016x", prior.text, text, uint64(h))
	}
	t.byHash[h] = hashEntry[V]{text: text, value: value}
	return nil
}

// Lookup resolves a hash to what it was registered for.
func (t *Names[K, V]) Lookup(h K) (V, bool) {
	e, ok := t.byHash[h]
	return e.value, ok
}

// TextOf recovers the original string. A hash is opaque in a debugger, and this
// is what makes it legible again -- for tooling and error messages, not for the
// hot path.
func (t *Names[K, V]) TextOf(h K) (string, bool) {
	e, ok := t.byHash[h]
	return e.text, ok
}

func (t *Names[K, V]) Len() int { return len(t.byHash) }
