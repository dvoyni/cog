package types

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dvoyni/cog/libs/assets"
)

// ShaderDescrParams is everything about a shader that is neither its path nor
// its inline text: the supply the preprocessor resolves the source against, and
// the message for a supply that has no legal spelling.
//
// Its fields are unexported on purpose, and here that is what keeps the one
// invariant that matters. ShaderDescr's own fields are exported, so anyone can
// write a descriptor down - but only with a zero Params, which is exactly
// ShaderWithResource(path) with no options: canonical, legal, harmless. What
// must not be forgeable is supply, because its canonical spelling is
// load-bearing - a hand-written one that spells two different supplies the same
// way puts them in one cache entry, and one of them then draws the wrong module.
type ShaderDescrParams struct {
	// supply is the canonical spelling of the descriptor's defines and consts:
	// entries sorted by name and joined with "\n", a define written NAME and a
	// const NAME=value. It is canonicalised at construction into one comparable
	// string so that ShaderDescr stays a plain value, stays a cache key, and
	// stays constructible before any Plugin or Backend exists.
	supply string
	// supplyMalformed carries the message for a supply that has no legal
	// spelling - an empty name, or a const value containing a newline - and is
	// empty otherwise. FlattenShader reports it; no constructor fails, because a
	// constructor that panicked would turn a typo in a material declaration into
	// a crash on a path that today cannot fail.
	//
	// It is a field rather than something the flattener recovers from supply,
	// because it cannot be recovered: ShaderConst("A", "x\nB") canonicalises
	// byte-identically to ShaderConst("A", "x") plus ShaderDefine("B"), and
	// keeping those two apart is what stops them sharing one cache entry.
	supplyMalformed string
}

// ShaderDescr describes a shader by inline source text (ShaderWithText) or a
// resource path (ShaderWithResource), resolved to bytes by the renderer.
//
// It is an assets.Descr: Name is the resource path, Blob is the inline text -
// wrapped, never copied - and Params is the supply. That is also what makes it
// the key of gfx's shader cache rather than merely the request handed to one.
//
// The two cases are disjoint and are told apart by which field carries the
// answer: a Name for a path, a Blob for inline text. There is no source enum,
// because it restated exactly that.
//
// A descriptor also carries its supply - the defines and const values the
// preprocessor resolves the source against. A root source plus one supply is
// one variant, and two supplies over one path are two shaders, so the supply is
// part of the descriptor's identity everywhere identity is decided.
//
// A defined type inherits no methods from the type it is defined over, so every
// accessor below is gfx's own and the descriptor offers precisely the surface it
// always did - the text in particular stays unofferable.
type ShaderDescr assets.Descr[ShaderDescrParams]

// ShaderOption is one entry of a shader's supply: a define or a const. Build it
// with ShaderDefine or ShaderConst.
type ShaderOption struct {
	name    string
	value   string
	isConst bool
}

// ShaderDefine supplies a valueless flag, readable by the source's #if
// conditionals and never reaching WGSL. Supplying a define a source does not
// mention is harmless; nothing can unset one.
func ShaderDefine(name string) ShaderOption {
	return ShaderOption{name: name}
}

// ShaderConst supplies a value for a #const the source declares, overriding
// that declaration's default. The value is WGSL text the preprocessor never
// interprets - the type rides in the value, so "16" and "16u" differ - and a
// name no source declares is silently ignored, which keeps one const map usable
// across a family of shaders.
func ShaderConst(name, value string) ShaderOption {
	return ShaderOption{name: name, value: value, isConst: true}
}

// ShaderWithText describes a shader from inline source bytes (e.g. WGSL).
//
// The text is wrapped rather than copied, so what identifies it is the run of
// bytes and not their spelling: a string literal, a const or a package var
// resolves to one address every evaluation and is therefore one cache entry
// however many times it is written down. A string computed afresh per call is a
// fresh allocation, a fresh identity and a fresh entry with it - the same rule
// assets.NewBlob already carries, and the reason this routes through the string
// rather than through []byte(text), which allocates a backing array per
// conversion and would make every call its own module.
func ShaderWithText(text string, opts ...ShaderOption) ShaderDescr {
	supply, malformed := canonicalSupply(opts)
	return ShaderDescr{
		Blob:   assets.NewBlobFromString(text),
		Params: ShaderDescrParams{supply: supply, supplyMalformed: malformed},
	}
}

// ShaderWithResource describes a shader loaded from storage.FileSystem.
func ShaderWithResource(path string, opts ...ShaderOption) ShaderDescr {
	supply, malformed := canonicalSupply(opts)
	return ShaderDescr{
		Name:   path,
		Params: ShaderDescrParams{supply: supply, supplyMalformed: malformed},
	}
}

// canonicalSupply renders one option list as the comparable supply string, and
// reports the first entry that has no legal spelling.
//
// The separator is "\n" rather than a printable byte because no legal value can
// contain one - a const's value is injected on its declaration's own line, so a
// newline breaks the mechanism however the key is spelled. "&" would be unsound
// and silently so: a const A whose value is "x&B" renders byte-identically to
// the define pair A=x, B, and since the key is only ever compared for equality
// the collision would not error - two supplies would share one cache entry and
// one of them would draw the wrong module.
//
// Defines and consts share one namespace, so sorting by name is a total order
// with no tie-break to invent, and a duplicate name resolves last-wins here
// rather than at flatten: two option lists differing only in the losing
// duplicate must not produce different keys for the same effective supply.
func canonicalSupply(opts []ShaderOption) (supply, malformed string) {
	if len(opts) == 0 {
		return "", ""
	}
	entries := make([]ShaderOption, 0, len(opts))
	for _, opt := range opts {
		if i := slices.IndexFunc(entries, func(e ShaderOption) bool { return e.name == opt.name }); i >= 0 {
			entries[i] = opt
			continue
		}
		entries = append(entries, opt)
	}
	slices.SortFunc(entries, func(a, b ShaderOption) int { return strings.Compare(a.name, b.name) })

	var b strings.Builder
	for i, entry := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(entry.name)
		if entry.isConst {
			b.WriteByte('=')
			b.WriteString(entry.value)
		}
		if malformed == "" {
			malformed = malformedSupplyEntry(entry)
		}
	}
	return b.String(), malformed
}

func malformedSupplyEntry(entry ShaderOption) string {
	switch {
	case entry.name == "":
		return "a supply entry has an empty name"
	case entry.isConst && strings.ContainsRune(entry.value, '\n'):
		return fmt.Sprintf("supplied const %q has a value containing a newline", entry.name)
	}
	return ""
}

// With describes the same source under its supply extended by opts. It is
// construction continued: an option naming an entry the supply already holds
// replaces it, last-wins, so the result is the descriptor that naming every
// option up front would have built - one cache entry, not two. It is how a
// renderer adds the defines a draw's geometry needs to a shader it was handed.
//
// A supply that was malformed stays malformed, under its first bad entry's
// message: adding a good entry does not launder a bad one.
func (d ShaderDescr) With(opts ...ShaderOption) ShaderDescr {
	if len(opts) == 0 {
		return d
	}
	supply, malformed := canonicalSupply(append(d.supplyEntries(), opts...))
	if d.Params.supplyMalformed != "" {
		malformed = d.Params.supplyMalformed
	}
	d.Params = ShaderDescrParams{supply: supply, supplyMalformed: malformed}
	return d
}

// Path reports the storage path a ShaderWithResource descriptor names, and is
// empty for one built from inline text. The text itself is not offered back:
// a whole shader source is not an identity, and nothing outside gfx can do
// with it what FlattenShader already does.
func (d ShaderDescr) Path() string { return d.Name }

// Supply reports the canonical spelling of the descriptor's defines and
// consts - entries sorted by name and joined with newlines, a define written
// NAME and a const NAME=value. It is part of the shader's identity: one path
// under two supplies is two shaders.
func (d ShaderDescr) Supply() string { return d.Params.supply }

// supplyEntries splits the canonical supply back into its defines and consts.
// It is meaningful only for a supply that is not malformed.
func (d ShaderDescr) supplyEntries() []ShaderOption {
	if d.Params.supply == "" {
		return nil
	}
	lines := strings.Split(d.Params.supply, "\n")
	entries := make([]ShaderOption, 0, len(lines))
	for _, line := range lines {
		if name, value, ok := strings.Cut(line, "="); ok {
			entries = append(entries, ShaderOption{name: name, value: value, isConst: true})
			continue
		}
		entries = append(entries, ShaderOption{name: line})
	}
	return entries
}

// ShaderLabel names a shader in a report: its root path, or gfx.shader for
// inline text, followed by its supply.
func ShaderLabel(descr ShaderDescr) string {
	root := descr.Name
	if root == "" {
		root = inlineShaderName
	}
	if descr.Params.supply == "" {
		return root
	}
	return root + " [" + strings.ReplaceAll(descr.Params.supply, "\n", " ") + "]"
}
