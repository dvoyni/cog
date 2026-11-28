# How other layout engines size with min, preferred, max and aspect

Research for [cog#976](https://github.com/dvoyni/cog/issues/976), on the map
[cog#975](https://github.com/dvoyni/cog/issues/975). Gathered 2026-11-28. It
feeds [#980](https://github.com/dvoyni/cog/issues/980), the sizing properties,
and [#981](https://github.com/dvoyni/cog/issues/981), the passes.

The question: how do established layout systems size a tree whose elements have
a minimum, preferred and maximum size and an aspect, so that a child can both
widen its parent and be limited by it?

It records facts and decides nothing. The last section is my assessment and is
marked as such.

**Sources.**

| System | What was read |
|---|---|
| v1 | `_legacy/bundles/ui` in the working tree at `28a06b9` |
| Clay | `clay.h` and README at [`e6cc369`](https://github.com/nicbarker/clay/tree/e6cc36941ab2af5d81107617039d6f527a1c660b) (2026-05-20) |
| Unity uGUI | Unity's mirror of `com.unity.ugui` at [`dd45d2c`](https://github.com/Unity-Technologies/uGUI/tree/dd45d2cee4422de98a375b937cef0728a79f28e8) (version 7.0.0, "Unity 7000.0.0a23", an alpha), and the released [2.0 manual](https://docs.unity3d.com/Packages/com.unity.ugui@2.0/manual/UIAutoLayout.html) |
| CSS | W3C editor's drafts of Flexbox 1, Sizing 3 and 4 and Position 3 at [`7027ab6`](https://github.com/w3c/csswg-drafts/tree/7027ab6fdf3c98d9c44666820041cb075b0e28dd) (2026-11-27). No browser engine. |
| Yoga | `main` at [`3a201ff`](https://github.com/facebook/yoga/tree/3a201ff472877df6558e05a1719edd273c2373e7) (2026-11-26), not a release |
| Flutter | 3.50.1 at [`ae9b586`](https://github.com/flutter/flutter/tree/ae9b586f34a8443573f82ef57c3bf914a908cbf7), and [Understanding constraints](https://docs.flutter.dev/ui/layout/constraints) |
| SwiftUI | Closed source: Apple's documentation, and the transcripts of WWDC19 [237](https://developer.apple.com/videos/play/wwdc2019/237/) and WWDC22 [10056](https://developer.apple.com/videos/play/wwdc2022/10056/) |
| WPF | [dotnet/wpf](https://github.com/dotnet/wpf/tree/997187874fe5d89cc66f9fbba8ededf14c556a30) at `9971878`, and Microsoft's [Layout](https://learn.microsoft.com/en-us/dotnet/desktop/wpf/advanced/layout) page |

**How the verdicts were reached.** By reading, except v1: its cells marked
"(run)" come from a temporary test injected with `go test -overlay`, built with a
copy of `_legacy/go.mod` less its `gogpu` replace, which names a directory that
is not there; nothing was added to the repository. No other engine was run. `L` is a line in the file
named. The tables across systems summarise the sections below them, where the
citations are.

A **walk** below is one traversal of the tree, or of a list of its elements.

## Answer in short

- **An element widens its parent and is limited by it** where four steps happen:
  a content size goes up, the parent's cap clamps it, the final width goes down,
  and the height is asked for **at that width**. v1 does the first three and
  never the fourth: its visual cannot be asked for a height at a width.
- **Two families.** Clay and uGUI make fixed walks, one axis at a time: widths
  settled, then heights. CSS, Yoga, Flutter, SwiftUI and WPF ask: a parent asks
  a child for a size in a stated space, sometimes several times, and relies on
  per-element caches.
- **Case D splits in two.** A row with a set height gives an image its width,
  and makes room for it, in v1, CSS, Yoga, Flutter, SwiftUI and WPF. A row whose
  height comes from its siblings does so cleanly in none I could verify: Clay
  gives the width too late for the row to make room, v1 only estimates, and
  Flutter needs a further query.
- **A capped wrapped text reports one of two widths:** the cap (Clay, CSS,
  Flutter by default) or its longest line (WPF, SwiftUI, v1). Case B's "the list
  is 300" follows from the first and not from the second.
- **Max rarely comes from children.** Where it does (uGUI 7.0), one narrow
  child caps a whole column.
- **Min against max differs.** Min wins in CSS, uGUI, WPF and v1; max wins in
  Clay.
- **Of the source read, only Clay's allocates nothing in a frame, declaring
  included.**

| System | A: longest 200 | B: longest 500 | C: wrapped tooltip | D1: image, row height set | D2: image, row height from siblings | E: tooltip leaves anchor | F: relative cap |
|---|---|---|---|---|---|---|---|
| v1 | yes (run) | **no** | **no** | yes | part (run) | yes | **no** (run) |
| Clay | yes | yes | yes | part | part | yes | **no** |
| uGUI 2.x, released | yes | **no** | **no** | unverified | unverified | yes | **no** |
| uGUI 7.0, alpha | yes | yes | yes | unverified | unverified | yes | **no** |
| CSS flexbox | yes | yes | yes | yes | **no** | yes | yes |
| Yoga | yes | part | part | yes | part | yes | part |
| Flutter | yes | yes | yes | yes | part | yes | part |
| SwiftUI | part | part | unverified | yes | unverified | yes | part |
| WPF | yes | part | yes | yes | **no** | yes | **no** |

The cases:

- **A.** A vertical list with a maximum width of 300, its longest item 200: the
  list and every item are 200, each on one line.
- **B.** The longest item 500: the list and every item are 300, the long item
  takes two lines, and its row and the list grow taller.
- **C.** A wrapped tooltip sizes itself with no hand-set wrap width and minimum
  width. A maximum width is taken as allowed.
- **D1.** An image keeps its aspect in a row whose height is set on the row.
- **D2.** The same, the row's height coming from its siblings' content. The map
  says "an image keeps its aspect in a row" and does not say which; the systems
  part exactly here.
- **E.** Adding a tooltip never resizes its anchor.
- **F.** A relative cap, such as a maximum width of half the parent, works,
  inside a content-sized parent too.

A and B assume the list is sized by its content. A parent that stretches it
(the default in CSS, Yoga and WPF) makes it as wide as the parent and its cap
allow, so 300 in both, and A fails.

## Across the systems

### What flows, how many walks, which axis first

| System | Goes down | Goes up | Walks | Axes |
|---|---|---|---|---|
| v1 | final rect | one size per axis | measure up, arrange down; then snap, clips, order, input, draw | both at once |
| Clay | final size | preferred and minimum per axis | fold at declaration, widths down, heights up, heights down, positions and commands down | width, wrap, height |
| uGUI | allocated size | min, preferred, flexible; max in 7.0 | widths up, widths down, heights up, heights down | width then height, fixed |
| CSS | available space | min-content and max-content contributions, then sizes | not fixed | intrinsic sizes first; flex: main then cross |
| Yoga | available size and mode, owner size | measured size | recursive, a node visited several times, cached | main then cross, per container |
| Flutter | constraints | size; intrinsic size on request | one, plus each intrinsic or dry-layout query | both at once |
| SwiftUI | a size proposal | the chosen size | not fixed | both at once |
| WPF | available size, then a rect | desired size | measure and arrange; some panels measure again | both at once |

### What width a capped, wrapped text reports

| System | Width of the text's box | Height for that width |
|---|---|---|
| v1 | longest line at a hand-set `WrapWidth` | wrapped at `WrapWidth`, drawn at the box's width |
| Clay | the width it is given | lines broken after the width walk, from cached word widths |
| uGUI | what it is allocated | `preferredHeight` at the current rect width |
| CSS | the available width, if the unwrapped text is wider | layout at the used width |
| Yoga | the measure function's choice | the same call |
| Flutter | the parent's maximum for more than one line; `longestLine` opts into hugging | the same call |
| SwiftUI | the longest line | the same call |
| WPF | the longest line | the same call; drawing re-wraps with the measure's width kept |

### Who gives min, preferred, max and aspect

V is the visual or content, C the children, A the author's own properties on the
element or a wrapper, P the parent.

| System | Min | Preferred | Max | Aspect | How P changes the final size |
|---|---|---|---|---|---|
| v1 | A | larger of V and C; A replaces | A | V, opted in | stretch and shrink weights, cross stretch |
| Clay | V (longest word), C, A | V, C; fixed replaces | A only | A only | grow, compression, percent |
| uGUI | V, C; A overrides | V, C; A overrides | 7.0 only: C; A overrides | a separate component | flexible shares, force expand |
| CSS | A; V and C through the automatic minimum | V, C (max-content); A replaces | A, or carried over by aspect | V (natural) or A | stretch, grow and shrink, percent basis |
| Yoga | A; V and C only with automatic min turned on | V (measure function), C; A replaces | A | A | stretch, grow and shrink, percent basis |
| Flutter | P's constraints; A by wrappers | V, C (max intrinsic) | P's constraints; A by wrappers | wrapper widget; an image's own | the constraints |
| SwiftUI | V and C's answer to a zero proposal; A | answer to an unspecified one; A | answer to an infinite one; A | V; A by modifier | the proposal |
| WPF | A | V, C (desired size) | A | V (an image's `Stretch`) | available size, `Stretch` alignment |

Two facts the table hides:

- **A leaf has content and a container has children**, so V and C rarely meet.
  v1 takes the larger of the two and adds padding (`layout.go` L292-299).
- **An author's explicit size replaces** the content size in all of them. It is a
  fourth source beside visual, children and parent.

### How aspect crosses from one axis to the other

| System | Width to height | Height to width | Height from siblings |
|---|---|---|---|
| v1 | a set width | a set height | estimated while measuring, for a relative height only |
| Clay | after the width walk | from a height on the element, or a percent one, the latter too late to make room | with a percent height only, and no room made |
| uGUI | `AspectRatioFitter` rewrites its rect | the same, after the walks | unverified |
| CSS | yes | when the height is definite | stretched, ratio lost |
| Yoga | yes | when the height is exact | ratio kept, row height ignored |
| Flutter | yes | when width is unbounded | needs `IntrinsicHeight`; the image's own size takes part |
| SwiftUI | yes | yes | unverified |
| WPF | yes | yes | no |

### A relative size inside a content-sized parent

| System | What happens |
|---|---|
| v1 | resolves against 0 while measuring, so a relative maximum makes the element 0 |
| Clay | a percent adds nothing to the parent's size, then is taken of the parent's final size |
| uGUI | the layout properties have no relative form |
| CSS | ignored while the parent sizes itself from content, then resolved against the parent's result; the parent is not sized again |
| Yoga | resolved against the size passed down, which while measuring is the space offered to the parent |
| Flutter | a fractional box reports the child's size divided by the factor, so the parent sizes to fit |
| SwiftUI | relative sizing is to the nearest "container" such as a window, not the parent |
| WPF | no percentages; a grid's star becomes content-sized when space is infinite |

### Cost

| System | Repeated measuring | Allocation |
|---|---|---|
| v1 | one visit each way; a visual asked its size once | reused slices (the engine's comment claims none warmed); declaring allocates ([#978](https://github.com/dvoyni/cog/issues/978)) |
| Clay | words cached across frames; a space measured again for each text it wraps | one arena of fixed capacity, no `malloc` |
| uGUI | only dirty subtrees; properties queried again each time | pooled component lists |
| CSS | not read | not read |
| Yoga | one cached layout and eight cached measurements per node | a `std::vector` per flex line computed |
| Flutter | one visit without intrinsics; intrinsic widgets are O(N²) in depth at worst | cache maps per render object |
| SwiftUI | results reused by the framework | not public |
| WPF | an unchanged measure is skipped | grid and text paths allocate |

## v1, the baseline

- **Flow.** Flatten, one measure walk in reverse order (`layout.go` L275-315),
  arrange forwards (`layout.go` L545-559), then snapping, clips, ordering,
  input and drawing (`layout.go` L38-49).
- **Height for a width.** None. `Visual.DefaultSize` takes no width
  (`element.go` L86-89), and a label wraps while measuring only at its
  hand-set `WrapWidth` (`elements.go` L465-473).
- **Relative sizes** resolve against 0 while measuring
  (`layout.go` L293-301). A relative maximum therefore clamps to 0
  (`layout.go` L1232-1246). Run: an element with `MaxWidthRel(0.5)`, as a
  root or as a column's child, is not drawn at all.
- **Stretch ignores the child's cross maximum** (`layout.go` L715-719). Run:
  a child with `MaxWidth(200)` in a stretching column 400 wide is 400 wide.
  Under stretch, the `MaxWidthRel(0.5)` child is also 400.
- **Aspect** comes from the visual's default size when opted in
  (`layout.go` L309-311). It crosses where an axis is set
  (`layout.go` L1173-1179), where a definite cross is stretched
  (`layout.go` L650-653), and for a relative cross size, which is estimated
  against what the siblings measured (`layout.go` L504-543).
- **Cases.** A passes (run: list and both items 200). B fails: heights are not
  computed again. C fails: the guide asks for `WrapWidth` and an equal
  `MinWidth` (`README.md` L474-487). D2, run, in a row with a sibling 60
  tall and a 2:1 image: with `HeightRel(1)` the image is 120×60 and the row makes
  room; with `AlignStretch` it is 40×60, out of shape. E passes
  (`WithFloating`). F fails.

## Clay

- **Flow.** Sizes fold up while the tree is declared. As an element closes, it
  sums or takes the largest of its children, once for the preferred size and once
  for the minimum, and clamps both by its own min and max
  ([clay.h L1867-1974](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1867-L1974)). Ending the layout then runs: widths top-down, text
  wrapping, heights from aspect, heights folded up, heights top-down, widths from
  aspect, and one walk that positions and emits render commands
  ([clay.h L2573-2716](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2573-L2716)).
- **Sharing space.** Along a container's axis, overflow is taken from the largest
  children first, down to their minimum, and spare space goes to `GROW` children
  smallest first, up to their max ([clay.h L2402-2491](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2402-L2491)). Across it, a
  child is held between its minimum and the parent's inner size
  ([clay.h L2492-2509](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2492-L2509)).
- **Height for a width.** A text's preferred width is unwrapped and its minimum
  is its longest word ([clay.h L2105-2108](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2105-L2108)). Lines are broken after widths
  are final, from cached word widths ([clay.h L2583-2636](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2583-L2636)), and heights
  are folded up again ([clay.h L2645-2691](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2645-L2691)). That fold writes the
  preferred height only, so a minimum height still holds its one-line value.
- **Aspect** is one number on an element ([clay.h L416-418](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L416-L418)). If one side
  is zero as the element closes, it is derived from the other
  ([clay.h L1857-1865](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1857-L1865)). After the width walk, each such element gets
  height = width / ratio, which also becomes its maximum height
  ([clay.h L2638-2643](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2638-L2643)); after the height walk, width = height × ratio,
  with no further width walk ([clay.h L2696-2700](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2696-L2700)). A percent height
  escapes that maximum: it is resolved in the height walk and the width derived
  there ([clay.h L2387-2400](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2387-L2400)).
- **Properties.** Per axis one of `FIT(min, max)`, `GROW(min, max)`, `FIXED` or
  `PERCENT` ([clay.h L300-339](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L300-L339), [README.md L1385-1393](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/README.md?plain=1#L1385-L1393)). Percent
  shares storage with min and max, so it cannot have either. An image carries
  only a pointer, with no natural size ([clay.h L425-427](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L425-L427)). A container
  that clips takes no minimum from its children along that axis
  ([clay.h L1893-1899](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1893-L1899)).
- **Where it gives up.** A percent child adds nothing to its parent's size
  ([clay.h L1935-1954](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1935-L1954)), then takes its share of the parent's final inner
  size ([clay.h L2387-2400](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2387-L2400)). The max is applied after the min, so max
  wins ([clay.h L1939](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1939)).
- **Floating elements** are roots of their own, not counted among the parent's
  children ([clay.h L1958-1970](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1958-L1970)). Only `GROW` and `PERCENT` ones take the
  parent's size ([clay.h L2291-2320](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2291-L2320)); a `FIT` one is limited by its own
  max alone.
- **Cost.** "Static arena based memory use with no malloc / free"
  ([README.md L10](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/README.md?plain=1#L10)), with fixed capacities, by default 8192 elements and
  16384 measured words ([clay.h L1150-1151](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1150-L1151)). Text measurement is cached
  by text and font settings and dropped after more than two unused frames
  ([clay.h L1639-1690](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L1639-L1690)); wrapping measures a space each time
  ([clay.h L2599](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2599)). Hover is tested against the last frame's boxes
  ([README.md L373](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/README.md?plain=1#L373)).
- **Cases.** A and B: list `FIT(0, 300)`, items `GROW`. C: floating
  `FIT(0, max)`. D1 and D2 are part: an image with a percent height of 1 takes
  the row's height and the ratio gives its width
  ([clay.h L2393-2398](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2393-L2398), [clay.h L2696-2700](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2696-L2700)), but the row's width
  and its other children were settled without it. With a `GROW` or `FIT` height
  the image gets no size, the width walk having made its maximum height 0
  ([clay.h L2638-2643](https://github.com/nicbarker/clay/blob/e6cc36941ab2af5d81107617039d6f527a1c660b/clay.h#L2638-L2643)). E passes. F fails: min and max are plain numbers,
  and percent is a size, not a cap.

## Unity uGUI

- **Flow.** Four walks: horizontal inputs bottom-up, widths set top-down,
  vertical inputs bottom-up, heights set top-down
  ([LayoutRebuilder.cs L90-104](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/LayoutRebuilder.cs#L90-L104)). "Calculated
  heights may depend on widths, but calculated widths can never depend on
  heights" ([UIAutoLayout.md L110-117](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Documentation~/UIAutoLayout.md?plain=1#L110-L117)). It runs for
  subtrees marked dirty, before rendering
  ([UIAutoLayout.md L120-126](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Documentation~/UIAutoLayout.md?plain=1#L120-L126)).
- **Properties.** Per axis, minimum, preferred and flexible. The 7.0 source adds
  maximum ([UIAutoLayout.md L13-22](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Documentation~/UIAutoLayout.md?plain=1#L13-L22)); the released
  [2.0 manual](https://docs.unity3d.com/Packages/com.unity.ugui@2.0/manual/UIAutoLayout.html)
  lists none. Space goes to minimums, then preferred, then flexible weights
  ([UIAutoLayout.md L24-28](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Documentation~/UIAutoLayout.md?plain=1#L24-L28)).
- **Who gives what.** Content components report their own: a text's preferred
  width is unwrapped ([Text.cs L738-768](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Text.cs#L738-L768)), an image's
  is its sprite's ([Image.cs L1869-1900](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Image.cs#L1869-L1900)). A layout
  group sums its children along its axis and takes the largest across it
  ([HorizontalOrVerticalLayoutGroup.cs L95-153](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/HorizontalOrVerticalLayoutGroup.cs#L95-L153)).
  A `LayoutElement` overrides any of them by priority, 1 by default
  ([LayoutElement.cs L21](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/LayoutElement.cs#L21)); among equals the
  largest wins, or the smallest for max, and a negative value means unset
  ([LayoutUtility.cs L334-436](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/LayoutUtility.cs#L334-L436)). The parent
  group decides whether it controls or force-expands its children
  ([HorizontalOrVerticalLayoutGroup.cs L237-258](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/HorizontalOrVerticalLayoutGroup.cs#L237-L258)).
- **Max from children (7.0).** A group's max across its axis is the **smallest**
  child's max
  ([HorizontalOrVerticalLayoutGroup.cs L125-131](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/HorizontalOrVerticalLayoutGroup.cs#L125-L131)),
  and preferred is clamped by it, min taking precedence
  ([LayoutUtility.cs L423-433](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/LayoutUtility.cs#L423-L433)).
- **Max not applied to shares (7.0).** The group reads each child's max when
  giving out space but does not use it: along the axis a child gets between min
  and preferred plus its flexible share, and across it a flexible child gets the
  whole width
  ([HorizontalOrVerticalLayoutGroup.cs L176-233](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/HorizontalOrVerticalLayoutGroup.cs#L176-L233)).
- **Height for a width.** `preferredHeight` is computed at the rect's current
  width, which the second walk has set
  ([Text.cs L761-768](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Text.cs#L761-L768)).
- **Aspect** is a separate `AspectRatioFitter` that rewrites its own rect when
  dimensions change and does nothing in the layout walks
  ([AspectRatioFitter.cs L138-220](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/AspectRatioFitter.cs#L138-L220)). It "does
  not take layout information into account such as minimum size and preferred
  size" ([UIAutoLayout.md L64-70](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Documentation~/UIAutoLayout.md?plain=1#L64-L70)). An image's
  `preserveAspect` changes only the quad drawn
  ([Image.cs L833-880](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Image.cs#L833-L880)).
- **Cost.** Component lists come from a pool, and properties are asked for again
  at each use; the source notes the component queries are made twice
  ([LayoutRebuilder.cs L93-103](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/LayoutRebuilder.cs#L93-L103)).
- **Cases.** A passes. B and C in 2.x fail: nothing declares a maximum, and
  overriding the preferred width is a hand-set wrap width that also breaks A. In
  7.0 a `LayoutElement` max clamps the preferred width, a `ContentSizeFitter`
  applies it to the root
  ([ContentSizeFitter.cs L107-128](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/ContentSizeFitter.cs#L107-L128)), and the
  group expands items to that width. D1 and D2 are unverified: widths are
  settled before heights, so no row can make room for a width that follows from
  its height, and how a fitter's late rewrite holds inside a group I did not
  check. E passes: a child can be ignored by layout
  ([LayoutGroup.cs L69-90](https://github.com/Unity-Technologies/uGUI/blob/dd45d2cee4422de98a375b937cef0728a79f28e8/com.unity.ugui/Runtime/UGUI/UI/Core/Layout/LayoutGroup.cs#L69-L90)). F fails.

## CSS: flexbox and intrinsic sizing

- **Vocabulary.** A box has a **min-content** size (narrowest without avoidable
  overflow) and a **max-content** size (ideal, given infinite space);
  **fit-content** is `max(min-content, min(max-content, available))`
  ([sizing-3 L249-303](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-sizing-3/Overview.bs#L249-L303)). Max-content is a preferred size,
  not a cap.
- **Flow.** Children give contributions, from which a flex container's own
  intrinsic sizes are made: the sum along the main axis in the web-compatible
  algorithm ([flexbox-1 L4556-4569](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L4556-L4569)), the largest across it
  ([flexbox-1 L4578-4583](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L4578-L4583)). Then, with the available space
  known: each item's base size, clamped to give a hypothetical size
  ([flexbox-1 L3696-3803](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L3696-L3803)); flexible lengths resolved in a
  loop that freezes min and max violators
  ([flexbox-1 L4073-4201](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L4073-L4201)); each item laid out at its main
  size to find its cross size; then stretch, which lays contents out again
  ([flexbox-1 L3892-4008](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L3892-L4008)). The spec fixes results, not
  walks ([flexbox-1 L3591-3596](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L3591-L3596)).
- **Properties.** Preferred, minimum and maximum size, each a length, a
  percentage or a content keyword; "the minimum size constraint is always the
  strongest" ([sizing-3 L433-452](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-sizing-3/Overview.bs#L433-L452)). A flex item's default
  minimum is content-based: its min-content size, limited by a definite preferred
  size and by its max
  ([flexbox-1 L1297-1361](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L1297-L1361)). That is what stops a text item
  shrinking below its longest word.
- **Aspect.** It acts only on an automatic size, naming a ratio-determining and a
  ratio-dependent axis ([sizing-4 L678-709](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-sizing-4/Overview.bs#L678-L709)). Definite min
  and max carry over to the other axis
  ([sizing-4 L839-864](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-sizing-4/Overview.bs#L839-L864)), and the box keeps its min-content
  size as an automatic minimum ([sizing-4 L723-732](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-sizing-4/Overview.bs#L723-L732)). In
  flex layout a definite cross size gives the base main size
  ([flexbox-1 L3734-3743](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L3734-L3743)), and later stretching does not
  change the main size, "even if it has a preferred aspect ratio"
  ([flexbox-1 L4007-4008](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L4007-L4008)). A stretched cross size counts as
  definite up front only when the container's is
  ([flexbox-1 L4239-4244](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-flexbox-1/Overview.bs#L4239-L4244)).
- **Where it gives up.** A percentage that depends on a parent sized from its
  content is treated as the initial value (or zero, for min) while that content
  size is computed, then resolved against the result, and "the containing block's
  size is not re-resolved". In the block axis it may stay `auto`
  ([sizing-3 L1553-1699](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-sizing-3/Overview.bs#L1553-L1699)).
- **Out of flow.** An absolutely positioned box does not affect its containing
  block's size; unless stretched between two set insets, an automatic size is
  fit-content in the space that block leaves
  ([position-3 L877-914](https://github.com/w3c/csswg-drafts/blob/7027ab6fdf3c98d9c44666820041cb075b0e28dd/css-position-3/Overview.bs#L877-L914)).
- **Cases.** A and B: a column list with `max-width: 300px`, sized to content
  (`align-self: flex-start`), its items stretched: fit-content gives 200, or 500
  capped at 300. C needs `width: max-content` with a `max-width`; left automatic,
  a tooltip wraps inside its containing block, which may be the anchor. D1
  passes. D2 fails: the image is stretched to the row's height and keeps its
  width. E and F pass.

## Yoga

- **Flow.** One recursive function per container: each child's flex basis
  (measuring it), line breaking, flexible lengths, justification, cross
  alignment with a second layout of stretched children, then the container's own
  size ([CalculateLayout.cpp L2124-2491](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L2124-L2491)). A parent passes an
  available size with a mode, `Undefined`, `Exactly` or `AtMost`
  ([YGNode.h L192-209](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/YGNode.h#L192-L209)).
- **Height for a width.** A leaf's measure function gets both sizes and modes and
  answers both ([CalculateLayout.cpp L462-572](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L462-L572)). The width it
  answers for wrapped text is its own choice.
- **Properties.** Width, min and max in points or percent; flex basis, grow and
  shrink; alignment; aspect ratio. Defaults differ from CSS: column direction and
  stretch ([Style.h L931-937](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/style/Style.h#L931-L937)), shrink 0
  ([Style.h L47-49](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/style/Style.h#L47-L49)). The CSS automatic minimum is **off** by
  default ([Config.h L90-91](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/config/Config.h#L90-L91)); turned on, it probes each shrinking
  or growing child's min-content size
  ([CalculateLayout.cpp L1167-1249](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L1167-L1249)). It is on `main`; releases
  v3.2.1 and v3.2.2 do not have it.
- **Aspect** turns one exact side into the other while finding the basis
  ([CalculateLayout.cpp L371-419](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L371-L419)), again after flexing
  ([CalculateLayout.cpp L1402-1409](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L1402-L1409)), and on stretch an item with
  a ratio takes its cross size from its main size, not from the line
  ([CalculateLayout.cpp L2423-2434](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L2423-L2434)).
- **Where it gives up.** Percentages resolve against the owner size passed down,
  which while measuring a content-sized parent is the space offered to it
  ([CalculateLayout.cpp L2176-2193](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L2176-L2193)). Stretch keeps the main size
  found before ([CalculateLayout.cpp L2423-2426](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L2423-L2426)). A value above
  the max returns the max before min is looked at
  ([BoundAxis.h L52-60](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/BoundAxis.h#L52-L60)).
- **Absolute children** take no part in the flex basis
  ([CalculateLayout.cpp L830-832](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L830-L832)). With no width, one is
  measured at max-content under a row, and fit-content against the containing
  block's width under a column, "to wrap to the size of its owner"
  ([AbsoluteLayout.cpp L377-397](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/AbsoluteLayout.cpp#L377-L397)).
- **Cost.** Each node caches one layout and eight measurements
  ([LayoutResults.h L26](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/node/LayoutResults.h#L26), [CalculateLayout.cpp L2969-3113](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L2969-L3113)).
  Each flex line computed builds a vector reserved to the child count
  ([FlexLine.cpp L26-27](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/FlexLine.cpp#L26-L27)). Results are rounded to the pixel grid
  at the end ([CalculateLayout.cpp L3242-3244](https://github.com/facebook/yoga/blob/3a201ff472877df6558e05a1719edd273c2373e7/yoga/algorithm/CalculateLayout.cpp#L3242-L3244)).
- **Cases.** A passes. B is part: the list is as wide as its widest child
  measures, so 300 only if the measure function answers the width it wrapped at.
  C is part: a tooltip wraps at its anchor's width or not at all unless sized;
  what a lone max width does there I did not verify. D1 passes. D2 is part: the
  ratio holds, from the image's own width, and the row's height is ignored. E
  passes. F is part.

## Flutter

- **Flow.** "Constraints go down. Sizes go up. Parent sets position", in one pass
  ([constraints](https://docs.flutter.dev/ui/layout/constraints)). A clean child
  given the same constraints is not laid out again
  ([object.dart L2869-2870](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/object.dart#L2869-L2870)). A flex lays out its inflexible
  children first, unbounded along its axis, then shares what is left
  ([flex.dart L1197-1305](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/flex.dart#L1197-L1305)).
- **Content sizes** are asked for separately: the **min intrinsic width** is the
  narrowest without clipping, and the **max intrinsic width** the width beyond
  which more never lowers the height; each takes the other axis as an argument
  ([box.dart L1635-1647](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/box.dart#L1635-L1647),
  [box.dart L1776-1790](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/box.dart#L1776-L1790)). These are content sizes, not caps.
- **Height for a width** is a layout call, or `getMinIntrinsicHeight(width)`
  ([paragraph.dart L839-860](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/paragraph.dart#L839-L860)). By default a paragraph of
  several lines takes the full width its parent gives; `longestLine` hugs
  ([text_painter.dart L153-163](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/painting/text_painter.dart#L153-L163)).
- **Properties** are wrappers, not fields: `ConstrainedBox`, `AspectRatio`,
  `Flexible`, `FractionallySizedBox`, `IntrinsicWidth` and `IntrinsicHeight`.
- **Aspect.** `AspectRatio` takes the maximum width if finite, else derives from
  the height, then fits the constraints; tight constraints override the ratio
  ([proxy_box.dart L522-580](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/proxy_box.dart#L522-L580)).
- **Both widening and being limited** needs `IntrinsicWidth`: it asks the child's
  max intrinsic width, clamps it to the incoming constraints and makes it tight
  ([proxy_box.dart L726-735](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/proxy_box.dart#L726-L735)). The source calls it relatively
  expensive, a speculative pass, O(N²) in depth at worst
  ([proxy_box.dart L623-626](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/proxy_box.dart#L623-L626)).
- **Where it gives up.** A flexible child that must fill, in a flex given
  unbounded space, is an error ([flex.dart L1105-1170](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/flex.dart#L1105-L1170)). `FractionallySizedBox` makes a
  **size**, not a cap, of the parent's maximum
  ([shifted_box.dart L1180-1201](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/shifted_box.dart#L1180-L1201)), and reports as intrinsic
  size the child's divided by the factor
  ([shifted_box.dart L1203-1253](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/shifted_box.dart#L1203-L1253)).
- **Tooltips** are drawn by an overlay, not by the anchor
  ([overlay.dart L1811-1814](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/widgets/overlay.dart#L1811-L1814),
  [raw_tooltip.dart L965-970](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/widgets/raw_tooltip.dart#L965-L970)), with the overlay's constraints
  loosened ([raw_tooltip.dart L993](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/widgets/raw_tooltip.dart#L993)).
- **Cost.** Intrinsic and dry-layout answers are memoised in maps per render
  object ([box.dart L1139-1162](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/box.dart#L1139-L1162)); each intrinsic call is
  documented as able to cause O(N²) behaviour
  ([box.dart L1647](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/box.dart#L1647)).
- **Cases.** A and B pass with a max-width box around `IntrinsicWidth` around a
  stretching column; without `IntrinsicWidth` a stretching column is 300 in both
  ([flex.dart L873-891](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/flex.dart#L873-L891)). C passes. D1 passes. D2 is part:
  `IntrinsicHeight` around a stretching row does it, but the row's intrinsic
  height includes the image's own at its natural width
  ([flex.dart L736-751](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/flex.dart#L736-L751),
  [proxy_box.dart L498-520](https://github.com/flutter/flutter/blob/ae9b586f34a8443573f82ef57c3bf914a908cbf7/packages/flutter/lib/src/rendering/proxy_box.dart#L498-L520)), so a large image sets the row.
  E passes. F is part: no cap relative to a parent was found among what I read.

## SwiftUI

- **Flow.** A parent proposes a size and the view chooses its own; there is "no
  way to force a size on your child"
  ([WWDC19 237](https://developer.apple.com/videos/play/wwdc2019/237/), 2:47).
  A parent may ask several times: a zero proposal asks the minimum, an infinite
  one the maximum, an unspecified one the ideal
  ([sizeThatFits](https://developer.apple.com/documentation/swiftui/layout/sizethatfits(proposal:subviews:cache:))).
  A horizontal stack offers an equal share to its least flexible child, deducts
  what it takes, and repeats (same talk, 14:18 to 16:11).
- **Height for a width** is a proposal with a width. A text's bounds never stretch
  beyond its displayed lines (same talk, 4:11).
- **Properties.**
  [`frame(minWidth:idealWidth:maxWidth:…)`](https://developer.apple.com/documentation/swiftui/view/frame(minwidth:idealwidth:maxwidth:minheight:idealheight:maxheight:alignment:)):
  with only a maximum, a frame proposed more than its child takes the proposal
  clamped to the maximum. So `maxWidth` means "grow up to", not "hug, up to".
  [`fixedSize`](https://developer.apple.com/documentation/swiftui/view/fixedsize())
  takes the ideal size whatever is proposed.
  [`layoutPriority`](https://developer.apple.com/documentation/swiftui/view/layoutpriority(_:))
  orders who gets space.
- **Aspect** is the
  [`aspectRatio`](https://developer.apple.com/documentation/swiftui/view/aspectratio(_:contentmode:)-771ow)
  modifier, fitting or filling.
- **Relative sizes** use
  [`containerRelativeFrame`](https://developer.apple.com/documentation/swiftui/view/containerrelativeframe(_:alignment:)),
  against the nearest window, column or scroll view, or a custom `Layout`.
- **Cost.** The framework reuses the results of layout calls, and a custom layout
  may keep arrays in its cache "to minimize the number of allocation events"
  ([makeCache](https://developer.apple.com/documentation/swiftui/layout/makecache(subviews:))).
- **Cases.** A and B are part: `frame(maxWidth: 300)` is 300 in A when offered
  more, and `fixedSize` ignores the cap; a custom `Layout` that asks ideal
  widths, caps the widest and proposes it can do both. WWDC22 10056 (8:06 on)
  shows the equal-width half of that, not the cap. C is unverified: the talk says
  a background is offered its view's size (WWDC19, 8:11); if an overlay is too,
  a tooltip wraps at its anchor's width. D1 passes by the documentation. D2 is
  unverified. E passes: an
  [overlay](https://developer.apple.com/documentation/swiftui/view/overlay(alignment:content:))
  leaves the modified view dominating layout. F is part.

## WPF

- **Flow.** A measure pass, in which a parent passes an available size and reads
  the child's desired size, then an arrange pass that passes the final rect
  ([Layout](https://learn.microsoft.com/en-us/dotnet/desktop/wpf/advanced/layout)).
  A clean element measured again with the same size is skipped
  ([UIElement.cs L618-624](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationCore/System/Windows/UIElement.cs#L618-L624)).
- **Properties.** `Width`, `MinWidth`, `MaxWidth` and alignment. An explicit
  width folds into both min and max, min winning
  ([FrameworkElement.cs L4036-4063](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/FrameworkElement.cs#L4036-L4063)).
  The available size is clamped by them before the element measures its content,
  and the result is clamped by them and by what was available: "parent wins"
  ([FrameworkElement.cs L4375-4451](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/FrameworkElement.cs#L4375-L4451)).
  An element not aligned `Stretch` is arranged at its desired size
  ([FrameworkElement.cs L4609-4619](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/FrameworkElement.cs#L4609-L4619)).
- **Height for a width.** A text block breaks lines at the available width and
  reports the longest line
  ([TextBlock.cs L1295-1308](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/TextBlock.cs#L1295-L1308)).
  Wrapping again at that narrower width can break differently, so drawing keeps
  the width measure was given and adjusts
  ([TextBlock.cs L3155-3277](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/TextBlock.cs#L3155-L3277)).
- **A stack panel** measures children with infinite space along its axis
  ([Stack.cs L559-572](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Stack.cs#L559-L572)), and
  arranges each at the panel's cross size
  ([Stack.cs L752-758](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Stack.cs#L752-L758)).
- **Aspect** has no property. An image scales from the available size, an
  infinite axis taking the other's scale
  ([Image.cs L307-337](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Image.cs#L307-L337),
  [ViewBox.cs L365-386](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/ViewBox.cs#L365-L386)).
- **Where it gives up.** No percentages. A grid measured with infinite space
  treats star tracks as content-sized
  ([Grid.cs L1149-1154](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Grid.cs#L1149-L1154)).
- **Popups** are zero-sized in the tree
  ([Popup.cs L1043-1053](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Primitives/Popup.cs#L1043-L1053)).
  Their content is measured unbounded, then, if it exceeds the screen limits,
  measured again restricted so that it can wrap
  ([PopupRoot.cs L137-205](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Primitives/PopupRoot.cs#L137-L205)).
- **Cost.** A grid measures some cells twice and repeats a cyclic case up to five
  times
  ([Grid.cs L598-676](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Grid.cs#L598-L676),
  [Grid.cs L3333](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Grid.cs#L3333)), allocating
  in places
  ([Grid.cs L1171-1175](https://github.com/dotnet/wpf/blob/997187874fe5d89cc66f9fbba8ededf14c556a30/src/Microsoft.DotNet.Wpf/src/PresentationFramework/System/Windows/Controls/Grid.cs#L1171-L1175)).
- **Cases.** A: a stack panel with `MaxWidth` 300, aligned left. B is part: the
  list is as wide as the longest wrapped line, at most 300. C passes with a
  `MaxWidth`. D1 passes. D2 fails: a sibling's height never reaches the image's
  measure. E passes. F fails.

## Assessment

Mine, not a finding, against what is already decided: min, preferred, max and
aspect; 2 or 3 passes, the last merged with drawing if possible; declarative and
functional; no allocation in a steady frame.

- **Fixed walks fit these constraints better than queries.** Content size up,
  clamp, final width down, height for it, final heights down is enough for A, B,
  C and E over flat reused slices, which v1 already has. The query models buy
  their flexibility with repeated visits and per-element caches or maps.
- **Done axis by axis that is four traversals, not two or three.** Clay and uGUI
  both make four or more. Two ways to reach three: fold the first into building
  the node list (Clay folds as each element closes), or make "widths down" and
  "heights up" one recursive walk, setting children's widths on the way in and
  summing heights on the way out. Folding at declaration needs the visual's sizes
  there, which in v1 need a canvas lookup the declarer does not hold. I have
  built neither. #981 should also say what counts as a pass.
- **Two content numbers per axis earn their place:** preferred lets a child widen
  its parent, and a minimum (a text's longest word) lets it be limited without
  overflowing. Canvas would have to measure both, and a height for a width.
- **Max from the author, and from aspect, is the least surprising.** uGUI 7.0
  shows what lifting children's max does to a column.
- **Report capped wrapped text at the width given,** as Clay, CSS and Flutter do.
  It gives B's 300 and never wraps again at a different width, the trap WPF and
  v1 work around. The price is a tooltip wider than its lines; hugging could be
  asked for, as Flutter's `longestLine` is.
- **For relative caps, CSS's rule costs no extra walk:** ignore the cap while
  sizing from content, apply it against the parent's final size on the way down,
  do not size the parent again. The parent can then be wider than the capped child
  needs. Whatever the rule, it must never resolve a cap against 0.
- **D2 is the one real conflict with width-first walks.** The choices are to
  require the row to carry a height; to estimate from siblings' preferred heights
  as v1 does, which is right only while no sibling's height depends on its width;
  to give the width late and let the row overflow, as Clay does; or to spend
  another walk.
- **Merging the last walk with drawing** is Clay's way, and Clay then tests hover
  against the last frame's boxes. v1 hit-tests between arrange and draw. That is
  already #981's question.
- **For #980:** it needs a home for the author's explicit size, which is none of
  visual, children or parent; a rule for min against max; and whether stretch and
  grow respect a max (v1's stretch does not).

## Not verified

- No engine but v1 was run; every other verdict is read from source,
  specification or documentation, and the cases were not built in any of them.
- No browser engine was read, so what CSS layout costs is not covered.
- SwiftUI is closed: C and D2 are open, and what an overlay is proposed is
  inferred from what the talk says of a background.
- uGUI's maximum size is in an alpha. I did not find which release it ships in.
- Clay's minimum height not following wrapped lines was read, and its effect (a
  column short of height compressing an item below its text) was not run.
