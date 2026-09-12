package ecs

import (
	"hash/fnv"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// clipHash and modelHash are two callers' own named types, which is the shape
// the whole mechanism exists for: a clip name cannot be assigned where a model
// name belongs, and neither one needs unwrapping.
type clipHash uint64

type modelHash uint64

func TestHashOfIsTheCallersOwnType(t *testing.T) {
	var clip clipHash = HashOf[clipHash]("Walk")
	var model modelHash = HashOf[modelHash]("Walk")
	if uint64(clip) != uint64(model) {
		t.Fatalf("one string hashed two ways: %d and %d", clip, model)
	}
	if clip == HashOf[clipHash]("Idle") {
		t.Fatalf("Walk and Idle hashed alike: %d", clip)
	}
}

func TestHashOfIsStableAndNeverNoHash(t *testing.T) {
	if HashOf[clipHash]("Walk") != HashOf[clipHash]("Walk") {
		t.Fatal("the same string hashed twice gave two answers")
	}
	if HashOf[clipHash]("") == NoHash {
		t.Fatal("the empty string hashed to NoHash, so the zero value is not free to mean none")
	}
	for _, text := range []string{"", "Walk", "Idle", "models/hero.glb", "opaque"} {
		if HashOf[modelHash](text) == NoHash {
			t.Fatalf("%q hashed to NoHash", text)
		}
	}
}

// model stands in for whatever a resolving plugin actually holds — a loaded
// mesh, a clip, a handle into its own dense array.
type model struct {
	path string
	mesh int
}

func TestNamesResolvesWhatWasRegisteredAndNothingElse(t *testing.T) {
	var models Names[modelHash, model]
	if err := models.Register("hero", model{path: "models/hero.glb", mesh: 3}); err != nil {
		t.Fatalf("Register(hero) = %v, want nil", err)
	}

	got, ok := models.Lookup(HashOf[modelHash]("hero"))
	if !ok {
		t.Fatal("Lookup of a registered name missed")
	}
	if got.mesh != 3 {
		t.Fatalf("Lookup gave mesh %d, want 3", got.mesh)
	}

	text, ok := models.TextOf(HashOf[modelHash]("hero"))
	if !ok || text != "hero" {
		t.Fatalf("TextOf = %q, %v; want \"hero\", true", text, ok)
	}

	if _, ok := models.Lookup(HashOf[modelHash]("villain")); ok {
		t.Fatal("a name nobody registered resolved")
	}
	if _, ok := models.TextOf(NoHash); ok {
		t.Fatal("NoHash resolved to a name")
	}
}

// TestRegisterCatchesACollision is the reason Register exists at all. Sixty-four
// bits makes a collision vanishingly unlikely — about 3×10⁻¹² at ten thousand
// distinct names — but vanishingly unlikely is not impossible, and an
// undetected one silently draws the wrong model. No pair of strings colliding
// under FNV-1a 64 is known, so the collision is forced at the seam Register
// computes its key at.
func TestRegisterCatchesACollision(t *testing.T) {
	var models Names[modelHash, model]
	const key modelHash = 12345

	if err := models.register(key, "hero", model{mesh: 1}); err != nil {
		t.Fatalf("the first registration failed: %v", err)
	}
	err := models.register(key, "villain", model{mesh: 2})
	if err == nil {
		t.Fatal("two names on one hash registered without complaint")
	}
	for _, want := range []string{"hero", "villain", "collide"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the collision error %q does not name %q", err, want)
		}
	}

	if got, _ := models.Lookup(key); got.mesh != 1 {
		t.Fatalf("the loser of the collision overwrote the winner: mesh %d", got.mesh)
	}
}

// TestRegisterRefusesANameThatIsNoHash keeps the zero value free to mean none.
// A registered name hashing to NoHash would make an unset Component field
// resolve to a real model, which is worse than a startup error by exactly the
// margin between a crash and a wrong picture.
func TestRegisterRefusesANameThatIsNoHash(t *testing.T) {
	var models Names[modelHash, model]
	err := models.register(NoHash, "impossible", model{})
	if err == nil {
		t.Fatal("a name hashing to NoHash registered")
	}
	if !strings.Contains(err.Error(), "impossible") {
		t.Fatalf("the error %q does not name the offending text", err)
	}
	if _, ok := models.Lookup(NoHash); ok {
		t.Fatal("NoHash resolves to something after a refused registration")
	}
}

// TestAHashIsLegalInAComponent is the whole point of the width and the
// constraint: naming an engine-side thing from inside a Component without
// storing a Go pointer in it. The same struct with the name as a string is what
// the rule forbids, and it is here so the contrast is checked rather than
// described.
func TestAHashIsLegalInAComponent(t *testing.T) {
	type drawable struct {
		Model modelHash
		Clip  clipHash
	}
	if err := PointerFree(reflect.TypeFor[drawable]()); err != nil {
		t.Fatalf("a Component naming two things by hash was rejected: %v", err)
	}

	type carriesTheName struct {
		Model string
	}
	if err := PointerFree(reflect.TypeFor[carriesTheName]()); err == nil {
		t.Fatal("a Component holding the name itself was accepted")
	}
}

// interner is the alternative this design rejects, written out so the rejection
// can be a test rather than an assertion: a table that *assigns* an id in
// arrival order. It is deliberately the friendliest possible version — no lock,
// no engine, no allocation past the map — and it still fails the one property
// that matters.
type interner struct {
	ids map[string]int
}

func (i *interner) intern(text string) int {
	if id, ok := i.ids[text]; ok {
		return id
	}
	if i.ids == nil {
		i.ids = make(map[string]int)
	}
	id := len(i.ids)
	i.ids[text] = id
	return id
}

// TestANameIsTheSameEverywhereAndAnIndexIsNot is the property that decides a
// hash against an interned index, and the reason a name may be a package-level
// var. Two tables — two Engines, two runs, two processes, it is the same
// argument — see the same two paths in opposite orders. The hash does not care;
// the index is a different number in each.
func TestANameIsTheSameEverywhereAndAnIndexIsNot(t *testing.T) {
	const hero, villain = "models/hero.glb", "models/villain.glb"

	var first, second interner
	first.intern(hero)
	first.intern(villain)
	second.intern(villain)
	second.intern(hero)

	if first.intern(hero) == second.intern(hero) {
		t.Fatalf("the interner survived opposite orders, giving %q id %d in both tables",
			hero, first.intern(hero))
	}
	if first.intern(hero) != 0 || second.intern(hero) != 1 {
		t.Fatalf("%q got ids %d and %d, want 0 and 1", hero, first.intern(hero), second.intern(hero))
	}

	if HashOf[modelHash](hero) != HashOf[modelHash](hero) {
		t.Fatal("one string hashed two ways")
	}
	if HashOf[modelHash](hero) == HashOf[modelHash](villain) {
		t.Fatal("two strings hashed alike")
	}
}

// TestHashingAccumulatesNothing is what makes there being no cleanup story
// correct rather than merely convenient. A million names nobody declared cost a
// million failed probes and not one byte: the table holds a manifest, and a
// manifest is what the consumer registered.
func TestHashingAccumulatesNothing(t *testing.T) {
	const declared = 8
	var models Names[modelHash, model]
	for i := range declared {
		text := "models/declared" + strconv.Itoa(i) + ".glb"
		if err := models.Register(text, model{path: text, mesh: i}); err != nil {
			t.Fatalf("Register(%q) = %v, want nil", text, err)
		}
	}

	for i := range 1_000_000 {
		key := HashOf[modelHash]("procedural/" + strconv.Itoa(i))
		if _, ok := models.Lookup(key); ok {
			t.Fatalf("a procedural name resolved at %d", i)
		}
		if _, ok := models.TextOf(key); ok {
			t.Fatalf("a procedural name had a text at %d", i)
		}
	}

	if len(models.byHash) != declared {
		t.Fatalf("the table holds %d entries after a million lookups, want %d", len(models.byHash), declared)
	}
	for i := range declared {
		text := "models/declared" + strconv.Itoa(i) + ".glb"
		if got, ok := models.Lookup(HashOf[modelHash](text)); !ok || got.mesh != i {
			t.Fatalf("%q resolved to %v, %v after the sweep", text, got, ok)
		}
	}
}

// TestTheHashIsFNV1a64 pins the wire format against an implementation that is
// not this one. A hash that is stable across processes and runs is worth
// nothing if a later edit quietly renumbers every name a save file already
// holds, so the oracle is the standard library's FNV-1a 64 and the basis is the
// published constant.
func TestTheHashIsFNV1a64(t *testing.T) {
	const publishedOffsetBasis uint64 = 0xcbf29ce484222325
	if got := uint64(HashOf[clipHash]("")); got != publishedOffsetBasis {
		t.Fatalf("the offset basis is %#x, want the published %#x", got, publishedOffsetBasis)
	}
	texts := []string{
		"", "a", "foobar", "Walk", "Idle",
		"models/hero.glb", "opaque", "shadow",
		"a name long enough to run the loop past a single machine word",
	}
	for _, text := range texts {
		oracle := fnv.New64a()
		_, _ = oracle.Write([]byte(text))
		if got := uint64(HashOf[clipHash](text)); got != oracle.Sum64() {
			t.Fatalf("HashOf(%q) = %#x, hash/fnv says %#x", text, got, oracle.Sum64())
		}
	}
}
