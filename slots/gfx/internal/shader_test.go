package internal

import (
	"strings"
	"testing"
)

func TestSupplyCanonicalisesSortedByName(t *testing.T) {
	descr := ShaderWithResource("s.wgsl",
		ShaderConst("SCENE_MAX_LIGHTS", "16"),
		ShaderDefine("SCENE_SKIN"),
		ShaderDefine("SCENE_MORPH"),
	)
	want := "SCENE_MAX_LIGHTS=16\nSCENE_MORPH\nSCENE_SKIN"
	if descr.Params.supply != want {
		t.Fatalf("supply = %q, want %q", descr.Params.supply, want)
	}
}

// Order at the call site is not part of a supply's identity: the same set
// written two ways is one variant and must be one cache entry.
func TestSupplyIsOrderIndependent(t *testing.T) {
	a := ShaderWithResource("s.wgsl", ShaderDefine("B"), ShaderConst("A", "1"))
	b := ShaderWithResource("s.wgsl", ShaderConst("A", "1"), ShaderDefine("B"))
	if a != b {
		t.Fatalf("%q and %q differ", a.Params.supply, b.Params.supply)
	}
}

// Duplicates resolve while the key is being built rather than at flatten,
// because two option lists differing only in the losing duplicate describe one
// effective supply and must not produce two keys for it.
func TestSupplyDuplicatesResolveLastWins(t *testing.T) {
	descr := ShaderWithResource("s.wgsl", ShaderConst("N", "4"), ShaderConst("N", "16"))
	if descr.Params.supply != "N=16" {
		t.Fatalf("supply = %q, want %q", descr.Params.supply, "N=16")
	}
	other := ShaderWithResource("s.wgsl", ShaderDefine("N"), ShaderConst("N", "16"))
	if other != descr {
		t.Fatalf("supplies differing only in the losing duplicate produced %q and %q", other.Params.supply, descr.Params.supply)
	}
}

// The separator is "\n" because no legal value can contain one. "&" would be
// unsound and silently so, which this records: the collision is byte-identical,
// and since the key is only ever compared for equality nothing would ever
// report it - two different supplies would share one cache entry and one of them
// would draw the wrong module.
func TestAmpersandWouldCollideAsASeparator(t *testing.T) {
	constWithAmpersand := []ShaderOption{ShaderConst("A", "x&B")}
	definePair := []ShaderOption{ShaderConst("A", "x"), ShaderDefine("B")}

	joinWith := func(sep string, opts []ShaderOption) string {
		canonical, _ := canonicalSupply(opts)
		return strings.ReplaceAll(canonical, "\n", sep)
	}
	if joinWith("&", constWithAmpersand) != joinWith("&", definePair) {
		t.Fatal("the collision this test records no longer reproduces; the reason for \\n has changed")
	}

	a, _ := canonicalSupply(constWithAmpersand)
	b, _ := canonicalSupply(definePair)
	if a == b {
		t.Fatalf("the canonical separator collides too: both render %q", a)
	}
}

// A supply with no legal spelling is not a construction failure: gfx's
// convention is that a bad descriptor surfaces at translate time, and a
// constructor that panicked would turn a typo in a material declaration into a
// crash on a path that today cannot fail.
func TestMalformedSupplyIsRecordedNotPanicked(t *testing.T) {
	tests := map[string]ShaderDescr{
		"empty define name": ShaderWithResource("s.wgsl", ShaderDefine("")),
		"empty const name":  ShaderWithResource("s.wgsl", ShaderConst("", "1")),
		"newline in value":  ShaderWithResource("s.wgsl", ShaderConst("A", "x\ny")),
	}
	for name, descr := range tests {
		if descr.Params.supplyMalformed == "" {
			t.Errorf("%s: constructed a malformed supply with no complaint recorded", name)
		}
	}
	if ok := ShaderWithResource("s.wgsl", ShaderConst("A", "x")); ok.Params.supplyMalformed != "" {
		t.Fatalf("a well-formed supply was reported malformed: %q", ok.Params.supplyMalformed)
	}
}

// A const value carrying a newline canonicalises byte-identically to a
// const-plus-define pair, so the malformation is kept beside the key rather
// than recovered from it - which is also what keeps the two apart as cache
// entries.
func TestNewlineValueStaysDistinctFromTheDefinePairItSpells(t *testing.T) {
	bad := ShaderWithResource("s.wgsl", ShaderConst("A", "x\nB"))
	good := ShaderWithResource("s.wgsl", ShaderConst("A", "x"), ShaderDefine("B"))
	if bad.Params.supply != good.Params.supply {
		t.Fatalf("the two no longer canonicalise alike: %q and %q", bad.Params.supply, good.Params.supply)
	}
	if bad == good {
		t.Fatal("a malformed supply shares a cache key with the well-formed pair it spells")
	}
}

// Every descriptor is a map key, and nothing about building one needs a Plugin
// or a Backend - which is what lets scene build its descriptors at model-load
// time and every test here build one inline.
func TestShaderDescrStaysAComparableMapKey(t *testing.T) {
	shaders := map[ShaderDescr]int{}
	shaders[ShaderWithResource("s.wgsl")] = 1
	shaders[ShaderWithResource("s.wgsl", ShaderDefine("SKIN"))] = 2
	shaders[ShaderWithText("fn main() {}")] = 3
	if len(shaders) != 3 {
		t.Fatalf("three distinct descriptors keyed %d entries", len(shaders))
	}
	if shaders[ShaderWithResource("s.wgsl", ShaderDefine("SKIN"))] != 2 {
		t.Fatal("an identically-built descriptor missed its own entry")
	}
}

func TestShaderLabelCarriesTheSupply(t *testing.T) {
	tests := map[string]struct {
		descr ShaderDescr
		want  string
	}{
		"no supply": {ShaderWithResource("builtin/model/scene.wgsl"), "builtin/model/scene.wgsl"},
		"defines": {
			ShaderWithResource("builtin/model/scene.wgsl", ShaderDefine("SCENE_SKIN"), ShaderDefine("SCENE_MORPH")),
			"builtin/model/scene.wgsl [SCENE_MORPH SCENE_SKIN]",
		},
		"const": {
			ShaderWithResource("builtin/model/scene.wgsl", ShaderConst("SCENE_MAX_LIGHTS", "4")),
			"builtin/model/scene.wgsl [SCENE_MAX_LIGHTS=4]",
		},
		"inline text": {ShaderWithText("fn main() {}", ShaderDefine("HQ")), "gfx.shader [HQ]"},
	}
	for name, test := range tests {
		if got := ShaderLabel(test.descr); got != test.want {
			t.Errorf("%s: shaderLabel = %q, want %q", name, got, test.want)
		}
	}
}

// Two materials differing only in their defines must not merge into one batch,
// because one of them would then draw the other's module.
func TestFingerprintDistinguishesTheSupply(t *testing.T) {
	base := Material(ShaderWithResource("s.wgsl"), FloatParam("roughness", 0.5))
	variants := map[string]MaterialDescr{
		"a define":      Material(ShaderWithResource("s.wgsl", ShaderDefine("SKIN")), FloatParam("roughness", 0.5)),
		"a const":       Material(ShaderWithResource("s.wgsl", ShaderConst("N", "16")), FloatParam("roughness", 0.5)),
		"a const value": Material(ShaderWithResource("s.wgsl", ShaderConst("N", "4")), FloatParam("roughness", 0.5)),
		"a second define": Material(ShaderWithResource("s.wgsl", ShaderDefine("SKIN"), ShaderDefine("MORPH")),
			FloatParam("roughness", 0.5)),
	}
	seen := map[uint64]string{base.Fingerprint(): "no supply"}
	for name, variant := range variants {
		fingerprint := variant.Fingerprint()
		if other, ok := seen[fingerprint]; ok {
			t.Errorf("%s fingerprints the same as %s", name, other)
			continue
		}
		seen[fingerprint] = name
	}
}

// With is construction continued: extending a descriptor's supply lands on the
// same descriptor as naming every option up front, text and path alike, so a
// renderer adding its variant defines to a caller's shader shares a cache entry
// with the caller naming them itself.
func TestWithExtendsTheSupplyAsConstructionWould(t *testing.T) {
	path := ShaderWithResource("s.wgsl", ShaderConst("N", "4")).With(ShaderDefine("SKIN"), ShaderConst("N", "16"))
	if want := ShaderWithResource("s.wgsl", ShaderDefine("SKIN"), ShaderConst("N", "16")); path != want {
		t.Fatalf("path supply = %q, want %q", path.Params.supply, want.Params.supply)
	}
	const text = "fn f() {}"
	inline := ShaderWithText(text).With(ShaderDefine("MORPH"))
	if want := ShaderWithText(text, ShaderDefine("MORPH")); inline != want {
		t.Fatalf("text supply = %q, want %q", inline.Params.supply, want.Params.supply)
	}
	if bare := ShaderWithText(text); bare.With() != bare {
		t.Fatalf("With() changed the descriptor")
	}
}

// A supply that was malformed stays malformed whatever is added to it: the
// report names the first bad entry, and adding a good one cannot launder it.
func TestWithKeepsAMalformedSupplyMalformed(t *testing.T) {
	descr := ShaderWithResource("s.wgsl", ShaderDefine("")).With(ShaderDefine("SKIN"))
	if descr.Params.supplyMalformed == "" {
		t.Fatalf("a malformed supply came back clean: %q", descr.Params.supply)
	}
}
