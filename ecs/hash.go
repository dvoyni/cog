package ecs

import "fmt"

// A Component names an engine-side thing — a model, a clip, a node, a pass tag
// — by the 64-bit hash of its name, never by the name itself and never by an
// assigned index. The name is a string and a Component holds no pointers; a
// hash is a plain number, so it may.
//
//	type ClipHash uint64                        // the caller's own named type
//	var walkClip = HashOf[ClipHash]("Walk")     // package level, at init
//
//	func animate(q *ecs.Query[AnimQ]) {
//	    for _, it := range q.All() {
//	        if it.M.Speed > 0.1 {
//	            it.A.Clip = walkClip
//	        }
//	    }
//	}
//
// The property that decides this against interning is that producing a hash
// needs nothing. Hashing is pure, so a System changes what an Entity names
// while holding only the lock it already has on the Component. Interning is
// what *assigns* the id, so the table would join the lock set of every System
// that ever changed a clip — and the common case here is mutation, a clip
// changing from "Idle" to "Walk" while the game runs, which a registration-time
// conversion never reaches. An assigned id also cannot be a package-level var:
// there is no table at package initialisation, and two Engines mean two tables.
//
// The same string hashes the same in every process and every run, which is what
// makes a hash the form that survives being written to a save file or sent over
// a wire. TestANameIsTheSameEverywhereAndAnIndexIsNot is that property stated
// as a test, against an interner that fails it.
//
// Names is the reverse half, and it belongs to the plugin that resolves names.
//
// HashKey is what a name hashes to: the caller's own type, constrained to
// ~uint64 so that the hash *is* that type. There is nothing to unwrap, it is
// directly comparable, printable and usable as a map key, and it is pointer-free
// because the constraint admits nothing else — so a Component may hold one.
//
// The point of the type parameter is that ClipHash and ModelHash stay distinct
// without a second one: a clip name cannot be assigned where a model name
// belongs. A codebase that does not want the distinction declares one such type
// and is done.
//
// ~uint64 rather than ~int because int is 32 bits on some platforms, and a hash
// that differs by where the game was built has given up its one advantage over
// an assigned index.
type HashKey interface {
	~uint64
}

// NoHash is the hash that names nothing, and it is the zero value of every
// HashKey — so a Component's unset name field already means "none" with no
// sentinel to remember and no second field to carry.
//
// It is untyped, so it compares against any HashKey without a conversion:
//
//	if it.Anim.Clip == ecs.NoHash { continue }
//
// No name a game writes down hashes to it. The offset basis of the hash is the
// hash of the empty string, which is not zero, and reaching zero from there
// requires the state after the second-to-last byte to be a value below 256 —
// about 2⁻⁵⁶ per name, and nothing constructs one by accident. Names.Register
// rejects the residue outright, so no *resolvable* name is NoHash at all.
const NoHash = 0

// The FNV-1a 64 parameters, which are the published ones. The hash is written
// out here rather than taken from hash/fnv because hash/fnv is an interface
// over a []byte Write: it allocates on a string and it cannot inline. This is
// the same five lines with neither cost, and it is short enough to audit
// against the definition — TestTheHashIsFNV1a64 audits it against the standard
// library rather than against itself.
const (
	hashOffsetBasis uint64 = 14695981039346656037 // 0xcbf29ce484222325
	hashPrime       uint64 = 1099511628211        // 0x100000001b3
)

// HashOf is the whole of the forward half: the name in, the caller's key out,
// no state, no lock, no allocation and no error case. It is pure, so it is as
// legal in a package-level var as it is in the body of a System.
//
// It hashes a string and nothing else, deliberately. Per-entity variable-length
// data is a different problem with a different answer; see the note in
// ecs/README.md.
func HashOf[K HashKey](text string) K {
	hash := hashOffsetBasis
	for index := 0; index < len(text); index++ {
		hash ^= uint64(text[index])
		hash *= hashPrime
	}
	return K(hash)
}

// Names is the reverse half — a hash back to the thing it names — and where it
// lives is the whole point: it belongs to the plugin that resolves names into
// things. A model table, a clip table, a pass-tag table: the consumer's own
// manifest, held as that plugin's own resource, read once per draw where a lock
// is held anyway.
//
//	type Scene struct {
//	    models ecs.Names[ModelHash, loadedModel]
//	}
//
//	func recordDraws(q *ecs.Query[DrawQ], scene *ecs.Read[*Scene]) {
//	    models := scene.Get().models
//	    for _, it := range q.All() {
//	        model, ok := models.Lookup(it.Draw.Model)
//	        ...
//	    }
//	}
//
// One System declares it, rather than every System that ever assigns a name —
// which is the asymmetry the whole scheme is built on, and the reason there is
// deliberately no process-wide interner. A Names has no lock of its own and
// wants none: it is an ordinary value inside the resource that holds it, so its
// lock set is that resource's, taken by the kernel at registration like any
// other. A mutex here would be the global lock this design exists to avoid,
// paid on every draw.
//
// Its zero value is ready to use, and it is registered into during the owning
// plugin's Registration or Startup — before any System reads it.
//
// Nothing cleans it because nothing accumulates in it. It holds what was
// registered and nothing else: asking about a name nobody declared answers that
// there is no such thing and leaves the table the size it was, which is a
// property of a manifest rather than of hashing in general.
type Names[K HashKey, V any] struct {
	byHash map[K]named[V]
}

// named is one row: what a hash resolves to, and the text it was registered
// under. The text is kept because a hash is opaque in a debugger, and one
// string per manifest entry is a price a manifest can pay.
type named[V any] struct {
	text  string
	value V
}

// Register declares that text names value, and is the one place a collision can
// be caught. Sixty-four bits makes one vanishingly unlikely — about 3×10⁻¹² at
// ten thousand distinct names — but vanishingly unlikely is not impossible, and
// an undetected one silently draws the wrong model. Every resolvable name
// passes through here, so one compare turns the class into a startup error
// instead of a wrong picture.
//
// It also refuses a text hashing to NoHash, which is what makes an unset
// Component field mean "none" rather than "whatever registered first".
//
// Registering a text already registered replaces its value and is not an error:
// the table is keyed by name, and the later registration is the one the
// manifest meant.
func (n *Names[K, V]) Register(text string, value V) error {
	return n.register(HashOf[K](text), text, value)
}

// register is Register with the key supplied, which is how the collision branch
// is reachable from a test: no pair of strings colliding under FNV-1a 64 is
// known, so the only honest way to exercise the compare is to hand it the
// collision.
func (n *Names[K, V]) register(key K, text string, value V) error {
	if key == NoHash {
		return fmt.Errorf("ecs: the name %q hashes to NoHash, which means no name at all; rename it", text)
	}
	if existing, taken := n.byHash[key]; taken && existing.text != text {
		return fmt.Errorf("ecs: the names %q and %q collide on hash %#x; rename one of them", existing.text, text, uint64(key))
	}
	if n.byHash == nil {
		n.byHash = make(map[K]named[V])
	}
	n.byHash[key] = named[V]{text: text, value: value}
	return nil
}

// Lookup resolves a hash to what it names, and reports whether anything was
// registered under it. A hash nobody declared misses, and misses without
// leaving a trace: nothing is inserted, so a million procedural names cost a
// million failed probes and no memory at all.
func (n *Names[K, V]) Lookup(key K) (V, bool) {
	entry, ok := n.byHash[key]
	return entry.value, ok
}

// TextOf recovers the string a hash was registered under, so that a hash stays
// legible in a debugger, a log line or an inspector. It is the reason a row
// keeps its text.
func (n *Names[K, V]) TextOf(key K) (string, bool) {
	entry, ok := n.byHash[key]
	return entry.text, ok
}
