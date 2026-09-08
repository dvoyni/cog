# gfx shader preprocessor — specification

`github.com/dvoyni/cog/gfx` resolves a shader's text before it reaches
`Backend.NewShader`. This document specifies that text pass: a small,
line-preserving preprocessor over WGSL that resolves `#include`, collects
`#define` and `#const` declarations, evaluates `#if` conditionals, hoists WGSL's
§4 directives, and hands one flattened string to the backend.

The mechanism is **backend-agnostic**. It runs entirely in gfx, above the
`Backend` interface, so the pure-Go, Rust-FFI and browser paths all receive
preprocessed source and none of them knows the preprocessor exists.

It exists for one reason, and the reason is correctness rather than tidiness.
Every `@group` / `@binding` a shader declares is reflected and must be bound at
draw time; a miss makes `CreateBindGroup` fail, gfx swallows the error, and
**the whole frame's command buffer vanishes silently** — the failure recorded in
[scene: custom shader contract and prelude](https://github.com/dvoyni/cog/issues/48).
Today the only way to have a binding that some draws do not need is to declare
it always and bind a dummy. Conditional declaration removes the dummy, and with
it the class of failure.

This document is the specification the implementation is judged against. It is
assembled from the resolved tickets of
[gfx: a WGSL shader preprocessor](https://github.com/dvoyni/cog/issues/118);
every section cites the tickets it came from. Nothing is decided here — where a
claim rests on something unverified, it is marked **Gap** and says what would
settle it.

The preprocessor is implemented in `gfx`, in `directive.go`, `condition.go`,
`flatten.go` and `hoist.go`, behind the one exported entry point
`FlattenShader`. The changes it needed elsewhere in gfx are listed in
[Required gfx changes](#required-gfx-changes).

---

## Contents

- [Vocabulary](#vocabulary) · [The shape of a run](#the-shape-of-a-run)
- [Directive syntax](#directive-syntax) · [Include resolution](#include-resolution)
- [Defines and consts](#defines-and-consts) · [Conditional compilation](#conditional-compilation)
- [WGSL §4 directives](#wgsl-4-directives) · [The Go-side supply API](#the-go-side-supply-api)
- [Caching, identity, and eviction](#caching-identity-and-eviction) · [Diagnostics](#diagnostics)
- [Worked migration: scene.wgsl](#worked-migration-scenewgsl)
- [Required gfx changes](#required-gfx-changes) · [Out of scope](#out-of-scope)

---

## Vocabulary

These five words are used precisely throughout, and they are in `CONTEXT.md`.
Getting them interchangeable is what makes every resolution-order rule
ambiguous, which is why they were pinned before any rule was written.

- **Shader source** — one `.wgsl` file. Sources are what `#include` composes.
- **Shader module** — the compiled WGSL unit behind one `ShaderID`. Many
  sources flatten into one module, so *source* and *module* are never
  interchangeable. Calling an included file a "module" is the specific mistake
  to avoid.
- **Root source** — the source a `ShaderDescr` names, whether by path
  (`ShaderWithResource`) or by text (`ShaderWithText`). It is the entry point of
  one flatten.
- **Define** — a valueless flag, `#define NAME`, readable only by `#if`. It
  never reaches WGSL.
- **Const** — a named value, `#const NAME=VALUE`, which becomes a WGSL `const`.
  It is never readable by `#if`.
- **Supply** — the set of defines and const values Go provides to one shader,
  fixed when its `ShaderDescr` is constructed.
- **Variant** — the module a root source plus one supply produces. One root path
  with two supplies is **two shaders**, not one shader with two states.

The define/const split is deliberate and load-bearing: `#define` never carries a
value and `#const` always does, which is what keeps `#if` free of value logic
([The #if grammar](https://github.com/dvoyni/cog/issues/122)).

---

## The shape of a run

The preprocessor runs in **two phases** over the source tree rooted at one
`ShaderDescr`.

1. **Flatten.** Blank comments to spaces, resolve and splice every `#include`,
   collect every `#define` and `#const` in the whole tree, hoist every WGSL §4
   directive.
2. **Select.** Evaluate `#if` conditionals over the flattened text, blanking the
   branches that lose.

Phase 1 completes before phase 2 begins. That ordering is not an implementation
convenience; it is the rule from which several others fall out, and it is
**chosen deliberately** rather than discovered during a build
([Define and const resolution order](https://github.com/dvoyni/cog/issues/124)):

- `#include`, `#define` and `#const` may **not** appear inside a conditional
  branch. Conditionals gate WGSL text and nothing else.
- Declarations are therefore never conditionally present, so no rule anywhere
  needs a notion of a declaration that might or might not exist.
- The include graph is a pure function of the root, **independent of the
  supply**, so the set of sources a module was built from does not vary by
  define set.
- `#define` is **file-scoped, not position-scoped**. A `#define FOO` at the
  bottom of a source governs an `#if FOO` at its top; there is no
  define-before-use rule, because there is no sequential pass in which "before"
  would mean anything.
- The flattener must hold the whole tree before it resolves anything.

The output is **line-preserving**. Comments blank to spaces rather than being
deleted, skipped branches blank rather than being deleted, consumed directive
lines blank, and a `#const` injects its `const` on the directive's own line. The
only construct that shifts output lines relative to source is the hoisted §4
prologue, which prepends. Measured on the `scene.wgsl` split: **938 source lines
flatten to 938 output lines**, with a zero-line prologue
([prototype: split scene.wgsl with the language](https://github.com/dvoyni/cog/issues/127)).
That property is what makes the source map a small table rather than a per-line
array, and it is the property to preserve — not the data structure.

---

## Directive syntax

From [The directive sigil](https://github.com/dvoyni/cog/issues/129).

### Two sigils, one meaning

`#include` and `//#include` are the **same directive**, and every directive
accepts both forms. There is no space between `//` and `#`.

The `//#` form is what buys the tooling: a source written entirely in that form
is **valid WGSL**, so wgsl-analyzer, formatters and highlighters keep working on
it — which matters most on the one file that will carry the most directives.
Accepting both lets the author decide per line whether editor support matters,
so neither the familiar spelling nor tooling-safety is taxed. This is
wgsl-linker's actual design; its own test fixture mixes the two in one file.

`@if`-style attributes were rejected. WGSL §3.8.1 enumerates a **closed**
attribute set, so an `@`-sigil source still fails to parse
(`UnknownAttribute`) — it buys nothing cog needs while committing to a syntax
whose payoff is WESL's module system, which cog is not building.

The bare `#` form is safe from collision but not from failure: `#` is
untokenizable in WGSL, so a directive that survives into the backend fails in
the lexer with no recovery
([research: are # directives safe in a .wgsl file?](https://github.com/dvoyni/cog/issues/119)).
That is precisely why the sigils are reserved, below.

### Both forms are line-anchored, and both are reserved there

A directive is recognised only when `#` or `//#` is the **first non-whitespace
character on the line**. Leading indentation is allowed.

- **Away from that position**, `//` starts an ordinary comment whatever follows
  it, so `let x = 1; //#todo` is a comment, not an error. A stray mid-line `#`
  is not the preprocessor's business; it is not valid WGSL either way.
- **At that position both sigils are reserved.** A line-start `#` or `//#` not
  followed immediately by a known directive keyword is a **preprocessor error
  naming the word**. So `#pragma once`, `#includ ./x.wgsl`, `//#TODO` and
  `# include` (the space) all fail loudly rather than passing through to become
  a WGSL lexer error in text nobody wrote.

The reservation costs no migration: there are **zero** line-start `#` or `//#`
occurrences across all five shaders in the tree today.

Line position is measured **after** comment blanking, so `/* c */ #if X` is a
live directive. The alternative — measuring on the original text — would make
that line silently pass through into the WGSL lexer, which is the failure mode
this mechanism exists to remove.

### Comments

One left-to-right pass; the first opener wins.

- `/* … */` is **always** a comment and is never unwrapped, including when it
  contains a line-start `#if`. Block comments are the reliable way to disable a
  region wholesale.
- `//` opens a comment **unless** it is `//#` at the first non-whitespace of the
  line.
- Comments are **blanked to spaces, not deleted**, so every downstream line
  number stays accurate.
- Commenting a directive out is exactly what an editor's comment-toggle
  produces: `#if` becomes `// #if` (space — inert), `//#if` becomes `// //#if`
  (inert).

**WGSL has no string literals at all**, so the comment scan is exact and needs
no lexer. This is the difference from C, where `"/*"` inside a string defeats a
naive scanner.

### The directive set and its lexis

`#include`, `#define`, `#const`, `#if`, `#elif`, `#else`, `#endif`.

- The keyword follows the sigil immediately — no space.
- One directive per line, terminated by the newline. No line continuations.
- `#include` takes an **unquoted** path: everything after the keyword to end of
  line, whitespace trimmed. No quotes, no angle brackets — reading to the
  newline makes a delimiter redundant. This composes with comment blanking:
  `#include ./pbr.wgsl // shared` yields `./pbr.wgsl`. A path can never contain
  `//` (`fs.ValidPath` forbids empty elements), so the two rules cannot collide.
- `#define NAME` takes a bare name. Trailing text is an error — a define never
  carries a value.
- `#const NAME=VALUE` takes a name, `=`, and a value read verbatim to end of
  line. Only the **first** `=` is significant.

The spellings are `#elif` / `#endif`, not `#elseif` / `#end`, matching C, GLSL,
HLSL, naga_oil and wgsl-linker.

### The file extension stays `.wgsl`

No `.wgsl.in`. A different extension buys honesty in the filename and nothing
else — an editor that does not recognise it gives *no* highlighting rather than
partial, and mapping it back to WGSL to regain highlighting regains the errors
with it. With `//#` available, the problem a rename addressed is solvable in the
source itself. `//go:embed builtin/scene/*.wgsl` and every storage path are
untouched.

---

## Include resolution

From [Include resolution](https://github.com/dvoyni/cog/issues/123).

### Two path forms

A path beginning `./` or `../` is **relative** to the including source's
directory. Anything else is **absolute**: a `storage.FileSystem` name, used
verbatim. The `./` prefix *is* the marker, so the two forms can never be
confused and the storage root has exactly one spelling.

There is no leading-`/` form. Storage names are `fs.ValidPath` — unrooted,
slash-separated (`storage/disk.go:64`) — so a rooted name is unopenable, and
inventing one would give the root a second spelling.

`fs.ValidPath` also forbids `.` and `..` **elements outright**, so
`"./common.wgsl"` is not a name storage can open. The preprocessor resolves and
normalises the path itself and only ever hands `Open` a clean absolute name.

`..` climbs, and normalises away before `Open`. The only boundary is the storage
root: a path that climbs above it is an error, never a silent clamp. There is no
mount boundary and no package boundary, because storage has no notion of either
and such a rule would be enforced in one place and bypassable everywhere else.

### A root source with no directory

`ShaderWithText` carries source text, not a path, so an inline source has no
directory. **A relative include in inline text is an error**, naming the
directive and saying so. Absolute includes work normally from inline text, so
nothing is lost.

Inline text is deliberately *not* treated as sitting at the storage root, and
`ShaderWithText` does not grow an optional base path. Either would make the same
snippet resolve differently depending on whether it was pasted or loaded — a
difference invisible in a diff.

### Include-once, by resolved absolute path

A source already spliced into this module is **not spliced again**. No
`#pragma once`, no `#ifndef` guard idiom, nothing for the author to write.

Two sources including a shared prelude is the expected case the moment anything
splits, and this language has no macros — so a second textual copy is never
*wanted*. WGSL rejects duplicate top-level declarations, which means C semantics
would turn every diamond into a compile error the author has to hand-guard.

Include-once is **load-bearing**, not an optimisation: it is what makes a shared
source's `#const` exactly one declaration under the uniqueness rule below.
Without it, every shared include would trip the duplicate-const error.

### Cycles are an error

Under include-once a cycle terminates on its own — the second visit is a no-op —
but it is **reported as an error carrying the include chain** anyway. A cycle
among sources is always an authoring mistake, and proceeding silently yields a
module missing whichever half the cycle cut. Given #48's history of an entire
frame's command buffer vanishing without a word, the default here is loud.

### Storage layering: unpinned, and that is the feature

An included path resolves through the **full mount overlay**, exactly like the
root source (`storage/resourcesimpl.go:24` searches mounts by descending
priority, per file). A game that mounts its own `builtin/scene/pbr.wgsl` at
higher priority replaces that one source inside cog's module and keeps the rest.

That is cog's customization mechanism working as designed, and one-file override
of a prelude is the good version of it. Pinning to the includer's mount would
need a mount-scoped `Open` that `FileSystem` does not expose.

**Gap.** Which mount a source came from is not observable today, so a surprise
override is invisible. The include chain a diagnostic reports should name what
was actually opened; making the mount itself visible is a `storage` change
nobody has asked for yet.

---

## Defines and consts

From [Define and const resolution order](https://github.com/dvoyni/cog/issues/124)
and [What a #const compiles to](https://github.com/dvoyni/cog/issues/121).

The ticket that owned this asked for an algorithm. The answer is that **there is
almost no algorithm left**: the order collapses to two rules that need no
traversal of the include graph.

### Consts are declared exactly once across the flattened tree

A `#const NAME` may be declared **once** in the whole tree that flattens into
one module. A second declaration of the same name — in the same source, in an
included source, in an unrelated sibling — is an **error**, unconditionally.

There is no shadowing, no precedence between sources, no ancestry relation, no
depth. An includer does **not** override its includee. That rule was charted and
is deleted; the prior-art survey found that **no preprocessor implements
includer-overrides-includee** — C, GLSL, HLSL, Slang and naga_oil all make the
host a prelude the source overrides
([research: prior art in shader preprocessors](https://github.com/dvoyni/cog/issues/120)).

The error fires **whether or not the name is ever referenced**. Deciding
"unreferenced" would mean reading WGSL, which is out of scope, and both
declarations are in hand either way.

The repair for two sources wanting one const is to **hoist it into a source both
include**. Include-once makes that exactly one declaration, so the idiom the
rule pushes toward is a shared source of consts — the same shape the rest of the
language already assumes.

### Go configures the one declaration; it never introduces a name

A supplied value is not a declaration and does not participate in the uniqueness
rule. It **overrides the value** of the single declaration the tree provides,
and it can never introduce a name, because an undeclared name has **no line to
inject at**. An unmatched Go key is **silently ignored**, which keeps one
const map usable across a family of shaders where only some declare each name.

So an includee's `#const` **is** its default, in the Sass `@use ... with` sense
— the analogue the prior-art survey identified — with no separate spelling
needed to say so.

### Defines are a union that nothing subtracts from

A define has no value, so "override" collapses to "set", and setting an
already-set flag is a no-op. The define environment is a plain **union** over
every `#define` in the tree plus every define in the supply: one global set for
the whole module, with **no precedence at all**.

- **Duplicate `#define` is allowed and silent.** The set has a defined answer;
  there is nothing to report but tidiness. (Contrast consts, where a duplicate
  leaves no single injection site.)
- **Nothing can unset a define.** There is no `#undef`, and Go cannot force a
  flag off. A source that unconditionally `#define`s a name has *decided*, and
  that reads as a decision. The variant knob is a supplied define on a source
  that does **not** self-define.
- **Go may add a define freely.** No declared set exists to check a key against.
- **Defines do not leak in any scoped sense, because there is no scope.**
  Collection completes before selection, so what an earlier include declares is
  visible to a later one *and to an earlier one*, identically, regardless of
  `#include` order.

### One namespace for both kinds

A name declared as both a define and a const is an **error**, wherever the two
declarations come from, including a supply key whose kind disagrees with the
source's declaration.

Nothing forced this — defines never reach WGSL, so two namespaces were
implementable — but the split between the kinds is load-bearing, and a name that
is both makes "what is `FOO`" unanswerable at a glance. The supply carries both
kinds under one key string, where two namespaces would mean one key legitimately
meaning two different things.

### Position is irrelevant, in both directions

A `#const` below the `#include`s still counts; a `#define` below an `#if` still
counts. Resolution is **not** a single textual pass.

### What a `#const` compiles to

A `#const NAME=VALUE` becomes a **WGSL `const` declaration, injected in place**.
The line `#const MAX_LIGHTS=16` becomes the line `const MAX_LIGHTS = 16;` in the
flattened module. Nothing else in the text is touched.

- **The line count is preserved exactly.** No synthetic prologue for the source
  map to account for.
- **Module-scope order is not significant** (measured against `naga.Parse` +
  `wgsl.Lower` from `github.com/gogpu/naga`, the pair `wgpu/gfxreflect.go`
  uses): a `const` declared *after* the `array<vec4<f32>, N>` that uses it
  lowers fine, and a `const` used in a function body above its declaration
  lowers fine. So a `const` emitted near the bottom of the flattened text serves
  a use at the top.
- **The value is text**, read verbatim to end of line and **never interpreted**
  — the same rule `#include`'s unquoted path already follows. The preprocessor
  emits `const NAME = <text>;` and lets WGSL type-check it. This removes any
  need for a type annotation in the directive: `#const N=16u` is u32,
  `#const F0=vec3<f32>(0.04, 0.04, 0.04)` is a vector, and the preprocessor
  knows what neither means.

**Injection, not substitution.** Textual substitution reaches further — it is
the only mechanism that could put a value in `@binding(...)` — and it is still
the wrong trade. It requires lexing identifiers so `MAX_LIGHTS_EXTRA` does not
match, which is a step toward reading WGSL; every substitution **shifts columns
on its line**, and column fidelity is what the diagnostics preserve
(`wgsl.ParseError` reports 1-based line and column with no byte offsets); and
the reach it buys is not needed, because the variability this serves is a
binding's **presence**, not its **index**, and presence is `#if`'s job. WGSL
binding numbers need not be contiguous, so deleting one never renumbers the
rest.

Rewriting the value of a real WGSL `const` in place was also rejected: it is the
deepest reach into WGSL of the three (find the declaration, edit between `=` and
`;`, survive a multi-line or commented spelling), and the standalone
type-checkability it protects is already forfeit — a source that uses a symbol
from an `#include` does not type-check alone either. Injection keeps the `//#`
promise (parseable, formattable) intact.

### The `@group` / `@binding` boundary

A `#const` name **cannot** be used as a `@group` or `@binding` index. WGSL takes
integer literals there; measured, `@binding(B)` with `const B: u32 = 1u` is
rejected (`global var 'u': @group requires @binding attribute`), and `@binding(0+1)`
is rejected identically, while `@location(L)`, `@location(L+1)`,
`@workgroup_size(W)` and `@align(A)` all lower fine.

The spec states this as a restriction; the preprocessor does **not** check it.
The line that draws is general: **the preprocessor diagnoses what it caused, not
what WGSL rejects.** An attribute the toolchain will not take is an author
writing WGSL, the same class as any type error, and policing attribute arguments
would mean reading WGSL.

### A `#const` in a switched-off source is still declared and still emitted

A source that guards its whole body in `#if FEATURE` cannot guard its own
`#const`, because a `#const` may not sit inside a conditional branch. So its
`const` survives into the output while the function beside it is cut. Measured
on the split, and harmless — WGSL does not mind an unused `const`. It is a real
property of the language and is stated outright rather than left to be derived.

---

## Conditional compilation

From [The #if grammar](https://github.com/dvoyni/cog/issues/122).

### The condition grammar

Operands are **defines**. Operators are `!`, `&`, `|` and parentheses. No
comparison, no arithmetic, no consts.

- **`!` is in.** Without it `#if !A & B` has no spelling at all — it becomes
  `#if B` wrapping a nested `#if A` / `#else`, which duplicates the B-branch
  body, and duplicated bodies drift.
- **There is no precedence between `&` and `|`.** Mixing them at the same level
  without parentheses is an error telling the author to parenthesise, so
  `#if A | B & C` is not a puzzle — it does not compile. `!` is unary and binds
  to a single name or a parenthesised group. The whole grammar is three
  productions with no ambiguity to remember.
- An **empty condition** is an error.

### An undefined name is false

Always, with no attempt to detect a typo — the author's mistake, not the
preprocessor's business.

What this accepts, recorded so nobody relitigates it believing it was
overlooked: a typo'd `#if SKN` around the skinning bindings deletes them, and a
binding that is expected-and-undeclared is exactly the chain that swallows a
frame's command buffer. The alternative — erroring on unknown names — is not
reachable in this grammar anyway: there is no `#undef` and no off-declaration,
so *absent* is the only spelling of "flag is off", and Go supplies a name only
when it is on. Erroring would have required a new declaration directive. A
known-define set derived from the `#if`s, offered as a way to catch a Go-side
typo, was considered and **declined**; the supply API does not inherit it.

### Nesting

`#if` nests, with no fixed depth limit — bounded by input size alone.

Inside a **skipped** branch, nesting structure is still tracked so that matching
works, and the sigil reservation still applies: a line-start `#` or `//#`
followed by an unknown keyword is an error there too, because reservation is
lexical and does not depend on liveness. Skipped text is **blanked, not
deleted**.

Every conditional opens and closes in one source. The "`#endif` in a different
source than its `#if`" case is unreachable, because an `#include` cannot sit
inside a conditional.

### A conditional may cut anywhere

An `#if` region is a pure text span. It may cut mid-declaration, straddle a
brace, or remove a single struct field.

This is settled empirically rather than by preference. The motivating case is a
cut **inside** a struct — `SceneVertexIn`'s two skinning attributes — and
`scene.wgsl:229` records the cost this removes: *"Every draw binds them, skinned
or not — a declared binding must be bound."* A restriction to declaration
boundaries would forbid the one thing the mechanism was chartered for. WESL
restricts `@if` precisely to keep a single syntax tree for its tooling; cog is a
text preprocessor that has already ruled out parsing WGSL, so it does not pay
that cost and would get nothing for the restriction.

If the surviving text is not valid WGSL, the backend says so — which is why
comments blank to spaces and lines are preserved, so that error lands on a real
line.

### The expressiveness cost of declarations-outside-conditionals is small

WGSL has a real `const`, so a conditional constant is plain conditional *text*:

```wgsl
//#if HQ
const SAMPLES = 16;
//#else
const SAMPLES = 4;
//#endif
```

and needs no `#const` at all. `#const` exists for values an includer or Go can
override, which is a different job.

A conditional `#include` is always rewritable as an unconditional include of a
source that wraps its own body in `#if`. That rewrite has a good consequence,
found on the split: **a feature source guards its own body, and the include
graph is a pure function of the root**, independent of the defines.

---

## WGSL §4 directives

From [enable/requires/diagnostic in an included source](https://github.com/dvoyni/cog/issues/130).

WGSL requires `enable`, `requires` and `diagnostic` to precede all declarations
in a module. Flattening breaks that by construction, so the preprocessor owns
the repair: it **hoists** them.

The alternative — forbidding them outside the root source — lost on rule count.
Forbidding needs **two** rules (no directives in an included source, and no
`#const` above a directive) and still leaves the transitive case unaddressed:
root includes `A` includes `B`, `B` needs `f16`, and the root must somehow know.
Hoisting needs **zero** rules; both problems stop existing. It also keeps the
`//#` standalone-validity property intact, which forbidding would have voided
for any source needing an extension.

### What is recognised, and where

Three keywords, matched as the first non-whitespace token of a line and read to
the terminating `;`.

They are recognised **only in a source's prologue** — the run of blank lines,
comments, and preprocessor directive lines at the top of the file, ending at the
first line of real WGSL content. This is WGSL's own placement rule restated, so
it needs no grammar, and it draws a clean line: a source that is valid
standalone WGSL keeps its directives in its own prologue by definition.

- **A `#const` line does not end the prologue.** It is a preprocessor directive,
  not WGSL content, at the time the prologue is scanned.
- **A directive sitting after real content in its own source is not
  recognised**, not hoisted, and left to the backend. That source was already
  invalid standalone WGSL; the preprocessor does not rescue it.
- **`@diagnostic(...)` as a statement or function attribute never matches**,
  because it starts with `@`. Prologue scoping makes this doubly safe.

### What hoisting emits

The hoisted prologue sits at the very top of the flattened module, above
everything, injected `const`s included.

- **`enable` and `requires`** each take a comma-separated list. Split it, union
  the names across all sources, emit one canonical directive per keyword. The
  union was chosen over text-level de-duplication because whether WGSL permits
  repeating an `enable` for one extension was **not** verified; unioning makes
  the question moot for one `strings.Split`, which is cheaper than answering it
  and cannot rot if the answer changes.
- **`diagnostic`** lines de-duplicate by normalised text; each distinct one is
  emitted once.
- **Each hoisted line blanks to spaces at its original site.**

### One new error, and one keyword that does not work

**Two `diagnostic` directives naming the same rule with different severities**
is an error. WGSL makes it one; the preprocessor *manufactures* it, since
unflattened the two sources are separate modules with no conflict at all — which
is the same reasoning that makes the §4 placement violation ours rather than the
backend's. Detection needs the **rule name only**: identical lines are already
de-duplicated, so any two survivors naming one rule differ, and differ only in
severity. No interpretation of `off` / `info` / `warning` / `error` is needed.
This is the only place the preprocessor reads inside a §4 directive.

`requires` is a **parse error** in `gogpu/naga` even correctly placed on line 1
— it is not implemented. It is hoisted exactly like the other two anyway.
Rejecting it with a helpful message reads like a kindness and is a trap: it
would encode one backend's current gap into this repo's language spec, and make
the language's own grammar depend on which keywords a backend happens to
implement.

### This is the one exception to "no parsing WGSL"

It is bounded: three fixed keywords, line-start position, prologue only, and the
line body is opaque text apart from a `diagnostic` line's rule name. It is also
paid under every option except silence — forbidding needs exactly the same
recognition in order to report the error.

### What was measured

Against `gogpu/naga` v0.19.0, this repo's pure-Go backend:

| probe | result |
| --- | --- |
| `enable f16;` once / twice / comma list | all accepted |
| `enable f16;` after a `const`, after a `fn` | accepted — position not enforced |
| `enable totally_not_real;` | accepted — extension names not checked |
| `f16` used with **no** `enable` at all | accepted |
| `requires <anything>;` on line 1 | **parse error** — not implemented |
| `diagnostic(off, …)` + `diagnostic(error, …)`, same rule | **accepted** — the WGSL error is not reported |

`gogpu/wgpu` ships three backends — pure Go, Rust FFI, browser WASM. Two of the
three are conformant and will enforce §4. The lenient one is the development
default, so a violation is invisible exactly where the work happens and fatal
exactly where it ships. That asymmetry is the argument for hoisting rather than
for trusting the backend.

---

## The Go-side supply API

From [The Go-side supply API and shader cache identity](https://github.com/dvoyni/cog/issues/125).

### The shape

```go
type ShaderDescr struct {
	source     shaderSource
	textOrPath string
	supply     string // canonical: entries sorted by name, "\n"-separated
}

func ShaderWithText(text string, opts ...ShaderOption) ShaderDescr
func ShaderWithResource(path string, opts ...ShaderOption) ShaderDescr

func ShaderDefine(name string) ShaderOption       // NAME
func ShaderConst(name, value string) ShaderOption // NAME=text
```

Both constructors grow `...ShaderOption`. A variadic parameter breaks none of
the thirty-odd existing call sites.

### One canonical string, not an interned id

The supply is canonicalised **at construction** into a single comparable field,
so `ShaderDescr` stays a plain value, stays a map key, and stays constructible
anywhere — before any `Plugin` or `Backend` exists. Every test in `gfx` and
`canvas` does exactly that, and scene builds its descriptors at model-load time.

Interning behind a `uint32` was measured and rejected. The concern was real —
`materialKey` re-hashes the key **per draw per frame** (`gfx/material.go:87`),
so a long key is a recurring cost — and the number is **1.2 ns** for a 41-byte
supply against a fingerprint already spending ~150 ns on ten parameters. Ten
thousand draws pay 12 µs a frame. Far too small to buy a design with, against a
registry that is process-global state, never shrinks, and has to outlive plugin
teardown.

### The canonical grammar, and the collision that forces it

Entries are sorted by name and joined with `\n`. A define is `NAME`; a const is
`NAME=value`, first `=` significant. Because defines and consts share **one
namespace**, sorting by name is a total order with no tie-break to invent.

`&` as the separator is unsound, and silently so: a const `A` whose value is
`x&B` canonicalises to `A=x&B`, byte-identical to the define pair `A=x`, `B`.
The key is only ever compared for equality, so a collision does not error — two
different supplies share one cache entry and one of them draws the wrong module.

`\n` needs no escaping because **no legal value can contain one**, and that
restriction is forced elsewhere rather than invented here: a const's value is
injected in place on the declaration's own line, so a newline breaks the
mechanism however the key is spelled.

### The query spelling is dropped

`gfx.ShaderWithResource("shader.wgsl?ENABLE_X&MAX_LIGHTS=8")` does not survive,
on facts rather than taste. A const value is text never interpreted, so a legal
value contains commas, parens, and can contain `&` or `?` —
`vec3<f32>(1.0, 0.0, 0.0)`, `1u & 3u` — and any in-band separator therefore
needs an escape rule the spec carries forever. And `#include` reads a path
unquoted to end of line, so `?` is an ordinary path character *inside* the tree:
the query form would give root paths and include paths different grammars over
one storage namespace, and would put path parsing in a second place.

### A const value is text, and there are no typed constructors

`ShaderConst(name, value string)`. The type rides in the value — `16` and `16u`
are different WGSL — so a helper formatting a Go `int` picks i32 where the
author may have needed u32, silently, and the preprocessor cannot catch it
because it never interprets the value. That is the one mistake this API could
make that WGSL punishes and nothing here detects. `ShaderConstInt` and friends
stay available to a second customer that argues for them; the whole `scene.wgsl`
split wanted **one** const, so there is no weight of usage behind the
convenience yet.

### Device facts are not an input

From [Can a const value be sourced from the device?](https://github.com/dvoyni/cog/issues/132).

A supply value is **author-chosen text fixed at `ShaderDescr` construction**.
The preprocessor never reads device state, and identity stays `(root, supply)`.

This is not merely a timing problem. gfx already refuses device numbers as an
input, in writing: `DefaultLimits` (`gfx/contract.go`) is the comparison target
*on purpose* — "a desktop adapter reports its hardware limits, where 200 storage
buffers is ordinary, so checking a shader against the device it happens to run
on passes builds that cannot run in a browser" — and `Limits()` exists only to
say, **in the report**, what the device this build ran on allowed. A
device-sourced `#const` is that rejected comparison relocated into shader text
where nothing checks it: the module would differ per machine, legally, with no
diagnostic.

A caller who wants a portable limit already has one:

```go
gfx.ShaderConst("MAX_LIGHTS", strconv.Itoa(gfx.DefaultLimits.MaxStorageBuffersPerShaderStage))
```

`gfx.DefaultLimits` is exported, machine-independent, and available at
descriptor-construction time with no `Plugin` and no `Backend`. It is named here
explicitly because nothing outside gfx references it today, so a bare "no" would
read as "the limits are unreachable" and send a reader off to build a query gfx
deliberately does not have.

### `MaterialDescr.Fingerprint` must hash the supply

Not optional. Without it, two materials differing only in their defines
fingerprint the same, merge into one batch, and one of them draws the wrong
module — the exact failure the doc comment on `Fingerprint` warns about.

---

## Caching, identity, and eviction

### Identity is `(root, supply)`

A module's cache identity is the `ShaderDescr` — root path or text, plus the
canonical supply string. **Flattened text is never observable**, which permits
but does not mandate a phase-1 cache keyed on the root alone. Such a cache is
genuinely available (phase 1 is supply-independent, and the include set was
measured define-independent), but a spec that *mandates* it is specifying an
optimisation.

### Preprocessing runs where shader loading runs today

Nothing moves. Flatten happens inside `ensureShader`, on the render thread,
**on a cache miss only** — the first draw of a given `(root, supply)`. The cost
changes from one file read to N, which for the scene split is ten small reads
once per variant. That is the same shape as today's hitch, not a new class of
problem; if it ever bites, it bites the first frame a material appears, which is
already true.

Moving preprocessing off the render thread was declined: it is a scheduling
change needing a "shader not ready yet" draw state gfx has no representation
for.

### Eviction scans the forward map

The translator **records each cached module's resolved include set** at flatten
time — on failure as well as success — and `releaseCachedResource`
(`gfx/translate.go:511`) evicts every module whose set contains the released
path.

Three things break today's single probe under the preprocessor: a path may root
several variants; a path may be an *included* source of modules rooted
elsewhere; and a `ShaderWithText` shader can include resources, so a text shader
is now evictable by a path.

The index is **forward** — module to its participating source paths — and
eviction scans it. A reverse `path → modules` index is O(1) instead of O(n), but
it is a second structure to keep in sync on every release, for a lookup nobody
waits on: eviction is a developer-loop command (`ReleaseCachedResourceCmd`,
`gfx/commands.go:25`), `t.shaders` holds single digits, and the forward
direction is what flatten already produces.

This also settles re-preprocessing when an included source changes: it does not
wait on filesystem watching, because the explicit eviction command already
exists.

### A variant needs a name

`shaderLabel` carries the supply:

```
scene/builtin/scene/scene.wgsl [SCENE_MORPH SCENE_SKIN]
```

with the brackets omitted when the supply is empty. Without it, four modules
from `scene.wgsl` would all be labelled identically, and the storage-buffer
limit report — the diagnostic this whole mechanism exists to make unnecessary —
could not say which variant tripped it. The label reaches both
`Backend.NewShader` and `checkWebLimits` (`gfx/translate.go:505`), and it is
what a backend validation error quotes back. It is computed once per cache miss,
never per frame.

---

## Diagnostics

From [Diagnostics and source mapping](https://github.com/dvoyni/cog/issues/126).

### One error type

```go
// ErrShaderSource reports a shader the preprocessor refused, or one the backend
// refused after flattening.
type ErrShaderSource struct {
	Shader  string           // the label: "path [SUPPLY]"
	At      []ShaderLocation // where; may be empty
	Message string
	Err     error // the backend's error, when it was the backend that refused
}

type ShaderLocation struct {
	Source string // storage name of the .wgsl file
	Line   int    // 1-based
}
```

One type, not fifteen structs. gfx's convention is a struct per failure
(`ErrShaderNotFound`, `ErrDrawSamplesAttachment`, `ErrShaderExceedsWebLimits`),
and that convention earns its keep when a caller might branch on the failure.
**Nothing can recover from any preprocessor error**, so nothing ever will
branch, and fifteen exported types plus a `Kind` enum would be pure surface.

`At` carries however many locations the error has, which the language forces to
be zero, one, two, or a chain:

- **Two**, for every error that is a relationship between lines. The **first**
  entry is the offending line; the second is the line it conflicts with. An
  unclosed `#if` reported only at EOF is the least useful message a
  preprocessor can emit, so the opening line is never optional.
- **A chain**, for the two errors where the include chain *is* the message: a
  missing include and a cycle. The chain is on the stack while flattening, so
  emitting it costs nothing; the last entry is the failing `#include` line.
- **Zero**, for a malformed supply — the one class with no source location at
  all. An empty `At` *is* the encoding; no sentinel source name is invented,
  because `Shader` already carries the supply and the message reads correctly
  without one.

**The first error stops the flatten.** No recovery, no batch: every one of these
is fatal to the shader, only one error per frame reaches a human anyway (every
gfx path is `if *firstErr == nil`), and resynchronising a directive stream to
keep going is real work for a message nobody would read.

### The map is a segment table

```go
type ShaderSourceMap struct {
	Segments []ShaderSegment // in output order, contiguous, covering every line
}

type ShaderSegment struct {
	OutputStart  int            // 1-based line in the flattened text
	Length       int
	Source       string         // storage name
	SourceStart  int            // 1-based line in Source
	IncludedFrom ShaderLocation // the #include line that pulled it in; zero for the root
}
```

A per-output-line provenance array was proposed and is not needed. Blanking
preserves lines within a source and splicing only reorders whole runs, so the
output is a concatenation of contiguous segments with 1:1 line correspondence
inside each — measured, ten sources totalling 938 lines flattening to exactly
938. A table describes that as precisely as a per-line array, at roughly eleven
entries instead of 938. naga_oil needed per-line granularity because its
composition is not line-preserving; ours is, **by construction**, and that is
the property to state rather than the data structure.

**The hoisted §4 prologue is the one segment with no source correspondence.** It
gets a segment with an empty `Source`, and lines in it map to nothing. The
directives are hoisted verbatim, so a WGSL error in one names the text itself;
for `scene.wgsl` the prologue is **zero lines**.

### Nothing the backend said is rewritten

**No line number ever crosses the backend boundary as data.** `gogpu/naga`
returns `*wgsl/internal/parser.ParseError`, an **internal** type; the public
`wgsl.ParseError{Message, Line, Column}` is a re-declared struct nothing ever
returns. So even though `%w` chains cleanly all the way up, `errors.As` from gfx
can never recover a line — and gfx must not import a backend's parser in any
case.

gfx **appends the rendered segment table** and lets the reader subtract:

```
gfx: shader "scene/builtin/scene/scene.wgsl [SCENE_MORPH SCENE_SKIN]" failed to compile:
wgpu: shader reflection failed: parse error: line 340, column 12: expected ';'
  flattened: 1-40 scene.wgsl; 41-120 ./frame.wgsl (scene.wgsl:3); 121-380 ./pbr.wgsl (scene.wgsl:4); …
```

Rendered on **one indented line**, not one entry per line, because the default
error handler is `log.Printf("kernel: %v", err)` (`kernel/engine.go:343`) and
`Error()` must read as a single printed block.

Two alternatives were declined. **Scraping `line (\d+)` out of the message**
works against this backend today and silently misreads the next one —
wgpu-native and a browser each phrase it differently, and a wrong mapping is
worse than none. **Growing the `Backend` contract with a line-carrying error
interface** is the clean answer in a world where a backend can supply the data;
gogpu cannot.

**No `// ===== path.wgsl =====` banner lines are emitted.** They would add lines
and break the 1:1 correspondence the table rests on. The table in the message
replaces them, and unlike a banner it is visible without dumping the source.

### Fatal, cached, reported once

Every preprocessor error is fatal to its shader: `ensureShader` returns the
error, `translateDraw` drops the draw, `firstErr` carries it to
`kernel.ReportError`. `ErrShaderExceedsWebLimits` stays the only non-fatal
report in gfx.

**A failed shader is cached as failed**, keyed on the same `(root, supply)`,
with its module→sources set recorded even on failure so eviction clears it like
any other entry. Today a failure caches nothing, so the next frame re-reads ten
files, re-flattens, re-fails and re-reports — at the frame rate. The developer
loop is unchanged: fix the file, hot-reload evicts, the next frame retries and
reports afresh.

**The error is reported once, not once a frame**, following
`p.reportedMissingBackend` (`gfx/plugin.go:37`) — the existing precedent for a
condition that is true every frame and worth saying once.

### Retrieving the flattened source

```go
func FlattenShader(fs storage.FileSystem, descr ShaderDescr) (string, ShaderSourceMap, error)
```

An exported plain function, not a command. gfx **cannot** write the text
anywhere: the render handler holds `kernel.Read[storage.FileSystem]`, and
writing needs `kernel.Write`, which would serialise rendering against every
reader. A resource command would work but buys a queue round-trip for a
developer-loop call.

It costs nothing extra — the translator needs the function to exist regardless,
so exporting it is a signature decision, not work — and it pays for itself
twice: it is the **test surface** for the whole preprocessor, which otherwise
can only be exercised through a backend, and what a developer dumps is
byte-identical to what compiled, because it is the same call.

### The complete error set

| Error | `At` |
| --- | --- |
| Unknown directive keyword after `#` or `//#` at line start | 1 |
| `#endif` / `#elif` / `#else` with no open `#if` | 1 |
| `#elif` or `#else` after `#else` | 2 |
| Source ends with an `#if` still open | 2 |
| `#include`, `#define` or `#const` inside a conditional branch | 2 |
| `&` mixed with `\|` without parentheses | 1 |
| Empty condition | 1 |
| `#define` with trailing text (a define never carries a value) | 1 |
| Malformed `#const` — no `=`, or an empty name | 1 |
| `#const` name declared twice in the flattened tree | 2 |
| A name declared as both a define and a const | 2 |
| Two `diagnostic` directives naming one rule with different severities | 2 |
| `#include` with an empty path | 1 |
| A relative `#include` in a `ShaderWithText` shader | 1 |
| An `#include` path climbing above the storage root | 1 |
| Missing include | chain |
| Include cycle | chain |
| Empty name in the supply | 0 |
| Newline inside a supply const value | 0 |

A malformed supply surfaces at **flatten**, not at construction. A constructor
that panics turns a typo in a material declaration into a crash on a code path
that today cannot fail, and gfx's convention is that a bad descriptor surfaces
at translate time — `ErrShaderNotFound` is exactly this shape.

**The exception is duplicate names in the supply**, which must be resolved
*before* the key string exists: two option lists differing only in the losing
duplicate must not produce different keys for the same effective supply. The
constructor resolves duplicates **last-wins** while canonicalising, and reports
nothing.

### What is deliberately not an error

A standing principle spanning three tickets: **the preprocessor does not report
what it can only infer.** An author's mistake is the author's, and the WGSL
compiler is the backstop.

- An **undefined name in a condition** — it is false, with no typo detection.
- An **unused `#const`**. Injection leaves a dead `const`; naga accepts unused
  consts without complaint (measured). It is the normal state of an includee
  default this root does not use, so warning would fire on correct code.
- A **supply const key no source declares**.
- **Duplicate `#define`s.**
- A **`#const` in a switched-off source**, which is still declared and emitted.
- A **`#const` colliding with a hand-written `const`** of the same name. Left to
  WGSL — with a caveat recorded because the decision rests on the compiler
  failing and **this repo's compiler currently does not**: `const N = 4;`
  followed by `const N = 9;` lowers without error under gogpu/naga and the array
  sizes to 9, last declaration winning silently. WGSL makes a duplicate
  module-scope name an error, so a conformant toolchain and the browser build
  report it. This is an upstream conformance gap, recorded so nobody later reads
  the silence as intended behaviour.

### Named misspellings

A fixed six-entry table maps the C, GLSL and naga_oil habits onto a message:

| written | message |
| --- | --- |
| `#ifdef`, `#ifndef` | unknown directive; this language spells it `#if NAME` / `#if !NAME` |
| `#elseif`, `#end` | unknown directive; this language spells it `#elif` / `#endif` |
| `#undef` | unknown directive; this language has no equivalent |
| `#import` | unknown directive; this language spells it `#include` |

No fuzzy matching, no edit distance. It is the cheapest diagnostic on the list,
and the split confirmed `#ifdef` is what a C habit actually reaches for.

### Two facts worth carrying into implementation

- **The pure-Go backend swallows WGSL compile errors.**
  `hal/software.CreateShaderModule` returns `sm, nil` when `naga.Compile` fails
  — "compilation failure is non-fatal". What actually rejects bad WGSL on the
  backend developers work against is **cog's own** `reflectShaderLayout` call in
  `wgpu/gfxbackend.go`. So these diagnostics reach a developer only through the
  reflection path, and anything that path accepts compiles silently into a
  module that draws nothing.
- **A `requires` parse error is not softened.** Pre-empting the gogpu gap in the
  preprocessor would encode it here instead of in the backend. The segment table
  already makes the raw naga error locatable, which is the whole benefit a
  softened message would have bought.

---

## Worked migration: `scene.wgsl`

From [prototype: split scene.wgsl with the language](https://github.com/dvoyni/cog/issues/127).
This section is the evidence the language is sufficient for the one shader that
actually needs it. Every number below was produced by flattening with a
throwaway implementation of this language and lowering through `naga.Parse` →
`wgsl.Lower`, the same path `wgpu/gfxreflect.go` reflects with.

### Ten sources, two defines

`scene/builtin/scene/scene.wgsl` (862 lines: PBR, lighting, skinning and morph
behind one `vs_main` / `fs_main`) splits into:

`scene.wgsl` (entry points) → `instance.wgsl`, `deform.wgsl`, `material.wgsl`,
and under those `vertex.wgsl`, `skin.wgsl`, `morph.wgsl`, `anim.wgsl`,
`pbr.wgsl`, `frame.wgsl`.

Two defines drive it. Not three:

- **`SCENE_SKIN`** gates group 2 bindings 0 and 1 (`scenePoses`,
  `sceneSkinJoints`), the `@location(6)` / `@location(7)` joints and weights
  attributes inside `SceneVertexIn`, and the skinning half of
  `sceneDeformVertex`.
- **`SCENE_MORPH`** gates group 2 binding 2 (`sceneMorphDeltas`) and the morph
  call.
- Group 0 binding 2, `sceneAnim`, is guarded by **`#if SCENE_SKIN | SCENE_MORPH`**.
  There is no third define, because the condition can say it. This is the only
  place `|` earns its keep in the whole split, and it earns it by *removing* a
  define rather than by combining two.

### The payoff, measured

| variant | defines | bindings | storage buffers |
| --- | --- | --- | --- |
| `scene.wgsl` today | — | 17 | 7 |
| debug line / static prop | *(none)* | **13** | **3** |
| morph only (a face) | `SCENE_MORPH` | 15 | 5 |
| skinned, no morph | `SCENE_SKIN` | 16 | 6 |
| everything | `SCENE_SKIN` `SCENE_MORPH` | 17 | 7 |

The full define set reproduces the current module's reflected binding set
**exactly** — same groups, same bindings, same names, same struct spans — so the
split is behaviour-preserving where it should be.

A static draw drops from **7 storage buffers to 3**. That number is the whole
argument: `scene.wgsl:239` records that `sceneMorphDeltas` is "the seventh and
last one this module may ever declare — the eighth stays reserved", against the
browser core adapter's floor of eight per stage. The budget was one binding from
exhausted for *every* draw, including a debug line that reads none of them.
Conditional declaration does not just avoid the #48 bind-group failure — it
hands back the headroom.

### `#if` cuts in three shapes, all exercised

- **Around `@group` / `@binding` declarations** at module scope — the payoff.
- **Inside a struct declaration** — `SceneVertexIn`'s two skinning attributes,
  the case the grammar was justified by.
- **Inside a function body, with `#else`** — `sceneDeformVertex` picks between
  the morph call and a plain construction, and between the skinning loop and
  `return base;`. Not anticipated when "may cut anywhere" was settled, but it is
  where the split wanted the knife most.

### Exactly one `#const`

`SCENE_MAX_LIGHTS`, declared in `frame.wgsl` — the source that owns
`lights: array<SceneLight, SCENE_MAX_LIGHTS>` — as its own default, with Go free
to override it. Measured end to end: an override of `16` → `4` moved the
reflected `SceneFrame` span from **1056 bytes to 480**.

That is the includee-declares-a-default idiom working as described, and it is
what keeps scene's Go-side cap and the shader's array in step instead of
duplicated: `scene/light.go:65` declares `const maxLights = 16`, the shader
independently spells `array<SceneLight, 16>`, and `scene/light_test.go` pins the
two together by hand. One `#const` replaces all three.

One const in 862 lines is thin, but it is not ceremony: it is the value that had
a correctness reason to be shared.

### Nothing in gfx breaks

`prepareParameterPlan` (`gfx/parameterplan.go:72`) walks the **reflected layout**
and looks up a parameter by name for each resource, so a parameter that is
supplied but no longer declared is **never visited**. `scene/draw.go` can keep
supplying `scenePoses`, `sceneSkinJoints` and `sceneMorphDeltas`
unconditionally and they simply go unread on a static draw — no error, no change
needed. `buildShaderLayouts` builds two groups instead of three and the pipeline
layout follows.

One shape is new: the morph-only variant declares **group 2 binding 2 with no
bindings 0 or 1**. WebGPU permits non-contiguous binding numbers and gfx builds
its layouts from reflection rather than by counting, so this costs nothing — but
it is the first time a group's shape varies by variant, and anything that
assumes density would break on it.

### Consequences for scene, larger than "nothing breaks"

**Scene's null skin has nothing left to be for.** It exists only because every
draw had to bind group 2 (`scene/lookup.go`); under the split a draw with no
skin has no group 2 at all, and the shared identity pose and identity joint are
dead. Removing them is scene's work, not this spec's, but it is the reason the
migration is worth doing rather than merely safe.

### The vertex-attribute rule

Cutting `@location(6)` / `@location(7)` for an unskinned draw depends on which
way round the layout/shader matching rule is.

**The direction that fails validation is a shader input no attribute supplies.**
Extra attributes the layout supplies and the shader never declares are
permitted. That is the direction the split needs, so the cut is legal.

Scene's own contract already says so: `ErrMeshCustomLayoutNeedsMaterial`
(`scene/err.go`) guards only the other way round and states outright that "the
standard layout with a custom material - is fine". `scene.wgsl`'s comment at
line 261 claimed the opposite and has been corrected to match.

**Measured, and the rule holds.** Neither direction is checked anywhere in the
local stack — `wgpu/hal/software/draw.go` builds a `shaderLocation → attribute`
map from the layout and then iterates **the shader's** location inputs,
`continue`ing on a miss, so an attribute nothing consumes is never visited and a
shader input with no attribute is silently skipped. So it was settled against a
conformant implementation instead: Chrome's WebGPU on a D3D12 adapter, building
the two pipelines directly.

| pipeline | result |
| --- | --- |
| layout supplies 8 attributes, shader declares 6 | **validated** |
| layout supplies 8, shader declares 8 | validated |
| layout supplies 6, shader declares 8 | **rejected** — *"Vertex attribute slot 7 used in … is not present in the VertexState"* |

The direction that fails is a shader input no attribute supplies, exactly as
scene's own contract states; extra attributes the shader never declares are
permitted. So cutting `@location(6)` / `@location(7)` for an unskinned draw is
legal while the mesh keeps supplying all eight, and the fallback that was held in
reserve — declaring all eight unconditionally and guarding only the skinning
code — is not needed.

### The prototype code is gone

The throwaway implementation and the ten split sources were not captured to a
branch and no longer exist on disk. The measurements above survive in #127's
resolution comment; the split itself is re-done at implementation time, which
costs a day and is the only thing lost.

---

## Required gfx changes

A checklist for an implementation session, in dependency order.

**`gfx/contract.go`**

- `ShaderDescr` gains an unexported `supply string`.
- `ShaderWithText` and `ShaderWithResource` grow `...ShaderOption`.
- `ShaderOption`, `ShaderDefine(name)`, `ShaderConst(name, value)` are added;
  the constructor canonicalises (sort by name, `\n`-join, duplicates last-wins).
- `ErrShaderSource` and `ShaderLocation` are added.
- `ShaderSourceMap` and `ShaderSegment` are added.
- `FlattenShader(fs storage.FileSystem, descr ShaderDescr) (string, ShaderSourceMap, error)`
  is added.

**`gfx/material.go`**

- `MaterialDescr.Fingerprint` hashes `supply` alongside `source` and
  `textOrPath`. Non-optional; see above.

**`gfx/translate.go`**

- `ensureShader` calls the flattener on a cache miss and passes the flattened
  text to `Backend.NewShader`.
- `shaderLabel` appends ` [SUPPLY]` when the supply is non-empty.
- The translator records each module's resolved include set, on success and on
  failure.
- Failed shaders are cached as failed, and reported once.
- `releaseCachedResource` scans the forward module→sources map instead of
  probing `t.shaders[ShaderWithResource(path)]`.

**New: the preprocessor itself**

- Comment blanking, sigil recognition, prologue scanning.
- Phase 1: include resolution (relative/absolute, include-once, cycle
  detection), declaration collection, §4 hoisting.
- Phase 2: condition parsing and evaluation, branch blanking.
- `#const` injection at the declaration's line.
- The error set above, with the named-misspelling table.

**Tests** go through `FlattenShader`, which is why it is exported. The
implementation is falsifiable without a backend.

---

## Out of scope

Recorded so nobody reopens them believing they were overlooked.

- **Parsing or validating WGSL.** The preprocessor is a text-level tool: it
  resolves directives and hands the flattened result to the backend, which
  already reflects and validates it. **One bounded exception**: the three §4
  keywords are recognised at line start in a source's prologue so they can be
  hoisted, and a `diagnostic` line's rule name is read so a severity conflict
  can be reported. Nothing else in a WGSL line is ever interpreted.

- **Checking that bindings surviving a `#if` are actually supplied.** The
  preprocessor knows only which declarations survived; what is *supplied* is the
  parameter list `prepareParameterPlan` walks, which arrives per draw and can
  differ between two draws sharing one material. Both halves live in gfx, so the
  check is not preprocessor behaviour
  ([Binding coverage: should anything check what a #if left declared?](https://github.com/dvoyni/cog/issues/131)).

  **The non-guarantee is worth stating because the failure is silent, and
  narrower and worse than it looks.** An unsupplied **sampler** falls back to
  clamp/linear and an unsupplied **texture** to a white view, but an unsupplied
  **storage buffer** emits no bind-group entry at all, `CreateBindGroup` rejects
  the short list, `flushBinds` silently `continue`s, and the draw is encoded
  with no bind group — nothing reaching `firstErr` or `t.diagnostic`. That is a
  pre-existing gfx bug that conditional compilation merely aims a loaded gun at,
  filed as
  [gfx: an unsupplied storage buffer binding fails silently, while textures and samplers fall back](https://github.com/dvoyni/cog/issues/133).
  It does not gate this spec.

- **Entry-point selection and vertex variants** — the other half of
  [gfx: shader preprocessing and vertex variants](https://github.com/dvoyni/cog/issues/45).
  An entry-point field on `ShaderDescr` needs no preprocessing and lands
  independently.

- **Publishing scene's custom-shader prelude** — what WGSL text scene publishes,
  and how it splits, is a scene decision that *consumes* this language
  ([scene: custom shader contract and prelude](https://github.com/dvoyni/cog/issues/48)).

- **Migrating the four canvas shaders.** They were 58–121 lines and
  self-contained when this spec was written, and the mechanism was deliberately
  not designed around them. Canvas has since migrated on its own terms
  ([canvas: a shared include for the canvas shaders](https://github.com/dvoyni/cog/issues/147)):
  the three that draw artwork `#include ./keycolor.wgsl` for the key-colour ramp
  they used to carry three copies of. The `scene.wgsl` split did produce
  genuinely general sources — its `frame.wgsl` and `pbr.wgsl` name nothing
  scene-specific — so what canvas and scene might share is a real question, but
  it is a scene-and-canvas question about content, not a question about this
  language.

- **A phase-1 cache keyed on the root alone.** Available and permitted, never
  mandated; the observable rule is that identity is `(root, supply)` and
  flattened text is never observable.

- **Device-sourced const values.** See
  [Device facts are not an input](#device-facts-are-not-an-input).
