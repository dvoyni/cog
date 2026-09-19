---
name: "Go File Layout"
description: "Use when creating a Go file, adding a type, or deciding which file a type or method belongs in, anywhere outside a plugin root. Covers one-type-per-file, where a type's methods live, splitting a large type, file naming, and declaration order within a file."
applyTo: "**/*.go"
---

# Go File Layout

Which file a declaration goes in, and where in that file it sits.

**This does not govern a plugin root.** A root's files are its public inventory —
`commands.go` is what the plugin offers — and they are placed by *what a
declaration is*, not by type. That rule is
[`kernel.instructions.md`](kernel.instructions.md) § Package File Layout, and it
wins inside a root. Everything else is this document: `kernel/`, `libs/`,
`internal/`, `internal/types/`, and every constructor package.

## These are house rules, and Go's own guidance is against some of them

Worth knowing before you argue with a reviewer, and before you propose relaxing
any of it.

The Go team decided file boundaries carry no meaning. Russ Cox, on the design:
*"in Go file boundaries are never relevant, nor is the order of top-level
declarations. This makes it nearly trivial to move code between files as makes
sense for organization."* The spec agrees — a top-level identifier's scope is
the package block, and the package clause exists only to say which files belong
together. Google's style guide rejects the convention by name: *"There is no
'one type, one file' convention as in some other languages"*, and warns against
*"having many tiny files."*

So nothing below is required by the language, and none of it buys the compiler
anything. What it buys is **findability**, which is the test Google does state:
*"files should be focused enough that a maintainer can tell which file contains
something, and the files should be small enough that it will be easy to find
once there."* A type-named file passes that test without the reader knowing
anything about the package.

The shape being copied is `bytes`, which Google cites as a good example:
`buffer.go` holds `Buffer` and its methods, `reader.go` holds `Reader` and its
methods, `bytes.go` holds the functions belonging to no type. Note what the
standard library does *not* do: `net/http` keeps twenty-odd unexported helper
types inside `transport.go`, and spreads `Transport`'s methods over six files.
We are stricter than that on purpose, and the reason is agents and grep, not
the compiler.

## One type, one file

**A class-like type gets its own file, named after it.** `scheduler` lives in
`scheduler.go`, `ResourceAccess` in `resourceaccess.go`.

**Class-like** means the type has at least one method that does something, as
opposed to rendering or exposing what it already holds. `Error`, `String`,
`MarshalJSON`, a plain getter and a plain setter do not count. So every type in
`err.go` is *not* class-like — an `Error()` method renders, it does not behave —
and they stay grouped.

**POD-like types group into one well-named file.** A struct plus its accessors,
a set of enums, a family of error types: put them together under a name that
says what the family is. `err.go` for the exported error types is the pattern.

**Tiny, closely related types may share a file even when class-like.** There is
deliberately no line count. A type is tiny when it is a constructor and a
one-line method, or two or three short methods — something a reader takes in
without scrolling. `Read[T]` and `Write[T]` are tiny and belong together with
the cell they read; `ResourceAccess` is not.

> **If you are unsure whether a type is tiny enough to share a file, ask.** The
> judgement is cheap for a person and expensive to get wrong in a review, and a
> wrong answer here is the thing that produces either a 900-line file or a
> directory of twelve-line ones.

## A type's methods live in its type's file

Every method of a type is declared in the file that declares the type. A method
does not move to the file of the feature it serves — that is how `Registrar`
ended up with four methods in `adapter.go`.

**The one exception is a type too large for a single file.** Split it, and then:

- the extra files hold **functions only** — methods on the type, and helpers
  they need. No new named types.
- each extra file's name **leads with the type's name**, so a directory listing
  sorts the pieces together.

### Never use an underscore in that name

Go reads a trailing `_<word>` as an **implicit build constraint** when the word
is a `GOOS`, a `GOARCH`, `unix`, or `test`. A file called `scheduler_js.go` or
`store_windows.go` is silently dropped from every build that does not match, with
no error and no warning.

Use one word, or a dash:

```
schedulergrant.go        ok
scheduler-grant.go       ok — go build and go list both accept it
scheduler_grant.go       avoid: reads like a build constraint
scheduler_js.go          WRONG: builds only on GOOS=js
```

**Underscores are reserved** for `_test.go` and for files genuinely selected by
platform — `_js`, `_windows`, `_desktop` — where the implicit constraint is the
point.

## Order within a file

```
1. file-level consts and vars
2. supporting types — typedefs, small PODs, the types the main type is built from
3. the main type's block:
       its consts
       the type
       its constructor(s)
       its exported methods
       its unexported methods
4. plain utility functions belonging to no type
```

**Supporting types come first** so that a reader reaches the main content with
the vocabulary already in hand. They are usually typedefs or small PODs, so this
costs a few lines before the subject rather than a page.

**Within the main type's block**, order methods in rough call order — a method
before the ones it calls — and keep the constructor immediately after the type.
This is [Uber's rule](https://github.com/uber-go/guide/blob/master/style.md#function-grouping-and-ordering):
*"Functions should be sorted in rough call order. Functions in a file should be
grouped by receiver... A `newXYZ()`/`NewXYZ()` may appear after the type is
defined, but before the rest of the methods on the receiver... plain utility
functions should appear towards the end of the file."*

Uber also says exported functions come first *"after `struct`, `const`, `var`
definitions"*. Read it as it is written: it orders **functions**, not types. It
does not promote an exported type's declaration above a supporting unexported
one.

## Shapes that were rejected

**Splitting by import set.** Dave Cheney's *Practical Go*: *"If you find your
files have similar `import` declarations, consider combining them."* It is the
one mechanical split rule anybody states, and it measures the wrong thing under
a type-centric layout — two type files in the same package *should* have
converging imports, and `bytes`' own `buffer.go` and `reader.go` do. Recorded
because it is the obvious counter-proposal.

**A numeric size threshold for "tiny".** Rejected in favour of a judgement and a
question. A line count invites a type to be padded or squeezed to land on the
right side of it.

**Enforcing this with a test.** `kernel/archtest` enforces the plugin tiers, and
the same could be done for the method-placement rule. Rejected: the parts worth
checking mechanically are the parts nobody gets wrong, and "tiny and closely
related" and "a well-named family" are judgements a test cannot make. If the
rule rots in practice, revisit this decision first.
