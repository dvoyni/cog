package types

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"

	"github.com/dvoyni/cog/kernel"
)

// LayoutView is one tick's element tree with what layout resolved it to,
// rendered while the tree is still alive.
//
// It is not a copy. A ui tree is uncopyable in principle rather than
// awkwardly: Element.userData is an any, and an any cannot be cloned. It can
// be rendered, which is what happens here, inside the tick, over data the
// frame releases the moment processUpdate returns.
//
// It is flat, because the data already lives flat and a nested document would
// mean inventing a shape the engine does not have. Every index in it is a
// source index - a position in Processor.nodes, never a position in the
// emitted array - so with a filter on, parent links still point at the right
// elements and a drill-down still names the same element. The index is the
// address.
type LayoutView struct {
	// Elements are the tree's elements in depth-first pre-order, which is the
	// order flatten produced them in. A subtree is therefore a contiguous
	// range of indices.
	Elements []ElementView `json:"elements,omitempty"`
	// Count is how many elements the whole tick declared, whatever the filter
	// kept, so a filtered response still says how much of the tree it is
	// describing. Omitted is what the filter dropped: whatever a snapshot
	// omits it says it omitted, because a tool that silently truncates cannot
	// be told from a game that declared nothing.
	Count    int  `json:"count"`
	Omitted  int  `json:"omitted,omitempty"`
	Subtree  *int `json:"subtree,omitempty"`
	MaxDepth *int `json:"maxDepth,omitempty"`
}

// ElementView is one element: where layout put it, and - where it asked for
// something - what it asked for, beside what it got.
//
// That pairing is the point of the capability. The app's source shows what was
// written; only the two side by side show what survived the modifiers, which
// is what makes a stretch that did not apply visible at all.
type ElementView struct {
	// Index is the element's position in Processor.nodes: its address, stable
	// under filtering, and what a drill-down passes back in subtree. Parent is
	// the same kind of index, and is -1 for a root. They are the only two
	// structural fields: flatten order plus a parent index reconstructs the
	// tree, and more index fields would be more things to keep consistent
	// under filtering.
	//
	// A filtered slice's first element keeps its true parent, which may sit
	// outside the slice. Every other element's parent is inside it, because
	// both filters keep whole ancestries.
	Index  int `json:"index"`
	Parent int `json:"parent"`
	// ID is what the app called this element, and is absent from most of them.
	// It is a hint, not an address: ids are optional, they are hierarchical
	// strings, and Interactions.Has matches them by prefix, so two elements
	// can answer to one name. Index is the address.
	ID string `json:"id,omitempty"`
	// Active reports that layout kept this element. An inactive one was
	// dropped - grid overflow is the way that happens - and is reported with
	// zero geometry rather than left out, because "the element exists and laid
	// out nowhere" is a different answer from "the element is not there".
	Active bool `json:"active"`
	// Rect, ContentRect and ClipRect are what layout resolved, in viewport
	// units. ContentRect is Rect less this element's own padding, and is where
	// children are arranged; ClipRect is what the element is visible through,
	// and an element whose rect falls outside it draws nothing.
	Rect        RectView `json:"rect"`
	ContentRect RectView `json:"contentRect"`
	ClipRect    RectView `json:"clipRect"`
	// Layer is the canvas layer the element's visual records into, resolved
	// from the root's base layer and any Layer modifier on the way down.
	Layer int `json:"layer"`
	// DrawOrder is the element's place in the sequence ui draws in - layers
	// ascending, then declaration order - among the active elements. An
	// element with no visual holds its place and draws nothing; an inactive
	// one has no place at all and reports none.
	DrawOrder *int `json:"drawOrder,omitempty"`
	// State is the visual state the element resolved to, named rather than
	// numbered: the inherited states, the ones the pointer produced, and the
	// ones the app added or removed. States the app defined for itself above
	// VisualUserDefinedBase are named user(n) after their bit, because ui
	// knows they are the app's and does not know what the app calls them.
	State []string `json:"state,omitempty"`
	// Visual is the Go type of the visual the element draws with, which is
	// usually the whole answer to "which one is this". An element with no
	// visual has none and is pure layout.
	Visual string `json:"visual,omitempty"`
	// UserData is the element's opaque payload, marshalled if it marshals. If
	// it does not - it holds a channel, or its own MarshalJSON panicked - the
	// field becomes {"$type": ..., "$opaque": true} with the reason. The type
	// name always works and is usually the whole answer an agent wanted.
	UserData json.RawMessage `json:"userData,omitempty"`
	// Declared is what the element asked for, and is present only for an
	// element that asked for something. A plain element costs nothing extra.
	Declared *DeclaredView `json:"declared,omitempty"`
}

// DeclaredView is what an element declared, beside what the resolved fields
// say it got. Every field is absent unless it was set: Element's constraints
// are opt values that already carry set, so omitting an unset one is exact
// rather than a guess about zero meaning unset.
type DeclaredView struct {
	Width    *SizeView `json:"width,omitempty"`
	MinWidth *SizeView `json:"minWidth,omitempty"`
	MaxWidth *SizeView `json:"maxWidth,omitempty"`

	Height    *SizeView `json:"height,omitempty"`
	MinHeight *SizeView `json:"minHeight,omitempty"`
	MaxHeight *SizeView `json:"maxHeight,omitempty"`

	Left   *SizeView `json:"left,omitempty"`
	Right  *SizeView `json:"right,omitempty"`
	Top    *SizeView `json:"top,omitempty"`
	Bottom *SizeView `json:"bottom,omitempty"`

	PivotLeft   *SizeView `json:"pivotLeft,omitempty"`
	PivotRight  *SizeView `json:"pivotRight,omitempty"`
	PivotTop    *SizeView `json:"pivotTop,omitempty"`
	PivotBottom *SizeView `json:"pivotBottom,omitempty"`

	PaddingLeft   *SizeView `json:"paddingLeft,omitempty"`
	PaddingRight  *SizeView `json:"paddingRight,omitempty"`
	PaddingTop    *SizeView `json:"paddingTop,omitempty"`
	PaddingBottom *SizeView `json:"paddingBottom,omitempty"`

	// Stretch and Shrink are the flex weights, and they are the pair this
	// capability exists for: a stretch that did not apply is invisible in a
	// resolved rect and invisible in the source, and visible only here.
	Stretch *float32 `json:"stretch,omitempty"`
	Shrink  *float32 `json:"shrink,omitempty"`
	// Align is how the element asked to be placed across its parent's axis,
	// and Layer the offset it asked for from its root's base layer.
	Align string `json:"align,omitempty"`
	Layer *int   `json:"layer,omitempty"`
	// Layout, Gap, Wrap, Columns, Rows, ChildrenArrangement and
	// ChildrenAlignment are what the element declared about its children
	// rather than about itself - the other half of why a child ended up where
	// it did.
	Layout              string    `json:"layout,omitempty"`
	Gap                 *SizeView `json:"gap,omitempty"`
	Wrap                bool      `json:"wrap,omitempty"`
	Columns             *int      `json:"columns,omitempty"`
	Rows                *int      `json:"rows,omitempty"`
	ChildrenArrangement string    `json:"childrenArrangement,omitempty"`
	ChildrenAlignment   string    `json:"childrenAlignment,omitempty"`
	// The opt-outs. Each one is a silent way for an element to behave
	// differently from its neighbours, which is exactly what a snapshot is
	// read to find.
	IgnoreLayout        bool `json:"ignoreLayout,omitempty"`
	IgnoreClip          bool `json:"ignoreClip,omitempty"`
	IgnoreHitTest       bool `json:"ignoreHitTest,omitempty"`
	StayOnScreen        bool `json:"stayOnScreen,omitempty"`
	PreserveAspectRatio bool `json:"preserveAspectRatio,omitempty"`
	// AddState and RemoveState are the visual states the element forces on or
	// off, named as the resolved State is.
	AddState    []string `json:"addState,omitempty"`
	RemoveState []string `json:"removeState,omitempty"`
}

// SizeView is one declared length: the number the app wrote, and whether it
// wrote it as a fraction of the parent. Both halves travel, because "20" and
// "20% of a parent that came out 40 wide" resolve to very different rects and
// the resolved rect cannot tell you which was asked for.
type SizeView struct {
	Value    float32 `json:"value"`
	Relative bool    `json:"relative,omitempty"`
}

// RectView is a rectangle in viewport units, which is the space every ui rect
// is in. It is ui's own rather than canvas's: canvas's RectView is documented
// as layer world space, and the two coincide only for a layer that set no
// window.
type RectView struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// LayoutViewOf renders one tick's resolved tree, bounded by the filter:
// subtree is the source index of the element to start at and maxDepth the
// deepest level kept below it, each unbounded when nil. The plugin calls it from the
// one subscriber ordered after ui's processing, while the tree is still alive.
//
// It reads Processor.nodes directly, which is why the rendering lives beside
// the layout engine: nodes is safe to read from inside exactly one window, and
// state whose contents are only valid then should not be exported for somebody
// else to read at the wrong moment - not even to the plugin, which gets a finished
// LayoutView and nothing it could hold on to.
//
// The filter bounds the per-element work, which is all of the expensive work:
// rendering one element allocates its declared block and marshals its
// userData. What it does not bound is the two int slices below, which are one
// pass over the tree each and no allocation per element.
func LayoutViewOf(context *Processor, subtree, maxDepth *int) (LayoutView, error) {
	nodes := context.nodes
	view := LayoutView{
		Count: len(nodes), Subtree: subtree, MaxDepth: maxDepth,
	}
	start, end := 0, len(nodes)
	if subtree != nil {
		root := *subtree
		if root < 0 || root >= len(nodes) {
			return LayoutView{}, ErrLayoutNoSuchElement{Index: root, Count: len(nodes)}
		}
		start, end = root, nodes[root].subtreeEnd
	}

	depths := depthsOf(nodes)
	base := 0
	if subtree != nil {
		base = depths[start]
	}
	order := drawOrdersOf(context)

	for index := start; index < end; index++ {
		if maxDepth != nil && depths[index]-base > *maxDepth {
			continue
		}
		view.Elements = append(view.Elements, elementViewOf(index, &nodes[index], order[index]))
	}
	view.Omitted = view.Count - len(view.Elements)
	return view, nil
}

// depthsOf is each node's depth below its own root. Pre-order guarantees a
// parent is written before its children, so one forward pass is the whole
// computation.
func depthsOf(nodes []layoutNode) []int {
	depths := make([]int, len(nodes))
	for index := range nodes {
		if parent := nodes[index].parent; parent >= 0 {
			depths[index] = depths[parent] + 1
		}
	}
	return depths
}

// drawOrdersOf inverts Processor.ordered: for each node, its place in the
// sequence ui draws in, or -1 for a node that has none because layout dropped
// it.
func drawOrdersOf(context *Processor) []int {
	orders := make([]int, len(context.nodes))
	for index := range orders {
		orders[index] = -1
	}
	for at, node := range context.ordered {
		if node >= 0 && node < len(orders) {
			orders[node] = at
		}
	}
	return orders
}

// elementViewOf renders one node: what layout resolved, and what the element
// declared where it declared anything.
func elementViewOf(index int, node *layoutNode, drawOrder int) ElementView {
	element := node.element
	view := ElementView{
		Index: index, Parent: node.parent, ID: string(element.id),
		Active:      node.active,
		Rect:        rectViewOf(node.rect),
		ContentRect: rectViewOf(elementContentRect(element, node.rect)),
		ClipRect:    rectViewOf(node.clip),
		Layer:       int(node.layer),
		State:       visualStateNames(element.intermediate.state.VisualState),
		UserData:    userDataView(element.userData),
	}
	if drawOrder >= 0 {
		view.DrawOrder = &drawOrder
	}
	if element.visual != nil {
		view.Visual = visualTypeName(element.visual)
	}
	if declared, any := declaredViewOf(element); any {
		view.Declared = &declared
	}
	return view
}

// declaredViewOf renders what the element asked for, and reports whether it
// asked for anything at all. A plain element gets no declared block rather
// than an empty one, which is what makes the declared side free.
//
// Every value is copied out rather than pointed at. The element lives in the
// app's borrowed child storage, released at the end of this tick, so a
// pointer into it is a field that reads correctly here and as nonsense in the
// document.
func declaredViewOf(element *Element) (DeclaredView, bool) {
	var view DeclaredView
	declared := false
	length := func(target **SizeView, value opt[size]) {
		if value.set {
			*target = &SizeView{Value: value.v.value, Relative: value.v.relative}
			declared = true
		}
	}
	weight := func(target **float32, value opt[float32]) {
		if value.set {
			copied := value.v
			*target, declared = &copied, true
		}
	}
	count := func(target **int, value opt[int]) {
		if value.set {
			copied := value.v
			*target, declared = &copied, true
		}
	}
	named := func(target *string, set bool, value string) {
		if set {
			*target, declared = value, true
		}
	}
	flag := func(target *bool, value bool) {
		*target = value
		declared = declared || value
	}
	states := func(target *[]string, value VisualState) {
		*target = visualStateNames(value)
		declared = declared || value != 0
	}

	length(&view.Width, element.width)
	length(&view.MinWidth, element.minWidth)
	length(&view.MaxWidth, element.maxWidth)
	length(&view.Height, element.height)
	length(&view.MinHeight, element.minHeight)
	length(&view.MaxHeight, element.maxHeight)
	length(&view.Left, element.left)
	length(&view.Right, element.right)
	length(&view.Top, element.top)
	length(&view.Bottom, element.bottom)
	length(&view.PivotLeft, element.pivotLeft)
	length(&view.PivotRight, element.pivotRight)
	length(&view.PivotTop, element.pivotTop)
	length(&view.PivotBottom, element.pivotBottom)
	length(&view.PaddingLeft, element.paddingLeft)
	length(&view.PaddingRight, element.paddingRight)
	length(&view.PaddingTop, element.paddingTop)
	length(&view.PaddingBottom, element.paddingBottom)
	length(&view.Gap, element.gap)

	weight(&view.Stretch, element.stretch)
	weight(&view.Shrink, element.shrink)
	count(&view.Layer, element.layer)
	count(&view.Columns, element.columns)
	count(&view.Rows, element.rows)

	named(&view.Align, element.align.set, alignmentName(element.align.v))
	named(&view.Layout, element.layout != LayoutNone, layoutName(element.layout))
	named(&view.ChildrenArrangement, element.childrenArrangement.set,
		arrangementName(element.childrenArrangement.v))
	named(&view.ChildrenAlignment, element.childrenAlignment.set,
		alignmentName(element.childrenAlignment.v))

	flag(&view.Wrap, element.wrap)
	flag(&view.IgnoreLayout, element.ignoreLayout)
	flag(&view.IgnoreClip, element.ignoreClip)
	flag(&view.IgnoreHitTest, element.ignoreHitTest)
	flag(&view.StayOnScreen, element.stayOnScreen)
	flag(&view.PreserveAspectRatio, element.preserveAspectRatio)

	states(&view.AddState, element.addState)
	states(&view.RemoveState, element.removeState)
	return view, declared
}

func rectViewOf(rect Rect) RectView {
	return RectView{X: rect.X, Y: rect.Y, Width: rect.Width, Height: rect.Height}
}

// userDataView marshals one element's opaque payload, here rather than on the
// caller's goroutine, because the value stops being readable the moment this
// tick's handler returns.
//
// Each element's marshal is guarded on its own. This runs in-tick, on the
// game's own goroutine, over data the app owns, and an app's own MarshalJSON
// can panic: a debug facility that can kill a frame is not one. Degrading per
// element rather than per tree is the other half of that - one bad element
// must not blank the snapshot around it.
func userDataView(data any) json.RawMessage {
	if data == nil {
		return nil
	}
	encoded, err := marshalGuarded(data)
	if err != nil {
		return opaqueView(data, err.Error())
	}
	return encoded
}

// marshalGuarded is json.Marshal with the app's own panics turned into
// ordinary failures. encoding/json re-panics anything a MarshalJSON raises,
// and the goroutine it would take down is the one running the frame.
func marshalGuarded(data any) (encoded []byte, err error) {
	defer func() {
		if reason := recover(); reason != nil {
			encoded, err = nil, fmt.Errorf("marshalling it panicked: %v", reason)
		}
	}()
	return json.Marshal(data)
}

// opaqueView is the dummy an unmarshallable payload degrades to. It names the
// Go type rather than becoming null, because reflect.TypeOf always works and
// knowing that the thing is a game.UnitRef is usually the entire answer an
// agent wanted - the contents rarely matter.
func opaqueView(data any, reason string) json.RawMessage {
	encoded, err := json.Marshal(struct {
		Type   string `json:"$type"`
		Opaque bool   `json:"$opaque"`
		Reason string `json:"$reason"`
	}{Type: goTypeName(data), Opaque: true, Reason: reason})
	if err != nil {
		// Three strings cannot fail to marshal; the constant is here so that a
		// future edit to the struct above cannot produce invalid JSON inside a
		// document the whole response depends on.
		return json.RawMessage(`{"$opaque":true}`)
	}
	return encoded
}

// visualNamer is how a snapshot names the visual an element draws with. Only
// boundVisual implements it, and only so that the name reported is the
// application's own ParamVisual rather than the generic wrapper ui puts around
// it - a type the application never wrote and would not recognise.
//
// Both halves live here rather than beside boundVisual in element.go because
// this is the only thing that ever asks: it is snapshot machinery, not part of
// what an Element is.
type visualNamer interface {
	visualTypeName() string
}

func (b boundVisual[T]) visualTypeName() string { return goTypeName(b.visual) }

// visualTypeName is the Go type of the visual an element draws with, through
// the seam above where there is one.
func visualTypeName(visual Visual) string {
	if named, ok := visual.(visualNamer); ok {
		return named.visualTypeName()
	}
	return goTypeName(visual)
}

// goTypeName is the Go type of value as an agent should read it, rendered by
// kernel.TypeName like every other type name cog prints. A type declared in
// this package - every built-in visual - is named after ui, the package an
// application imports: "types" names nothing an agent could look up.
func goTypeName(value any) string {
	typ := reflect.TypeOf(value)
	if typ == nil {
		return ""
	}
	return kernel.TypeName(typ)
}

// The name tables. They are unexported for the reason gfx's and canvas's are:
// naming an enum for a debug document is not the same promise as giving every
// ui enum a String. An unknown value names its ordinal rather than falling
// back to a legal-looking name, so a member added without touching this file
// is visible instead of mislabelled.

func layoutName(layout Layout) string {
	switch layout {
	case LayoutNone:
		return "none"
	case LayoutHorizontal:
		return "horizontal"
	case LayoutVertical:
		return "vertical"
	case LayoutGrid:
		return "grid"
	}
	return unknownName(int(layout))
}

// alignmentName names an Alignment. AlignTop, AlignLeft and the rest are
// aliases of the same four values, so the axis-neutral name is the only one
// that can be reported: which axis it means is the parent's Layout.
func alignmentName(alignment Alignment) string {
	switch alignment {
	case AlignStart:
		return "start"
	case AlignCenter:
		return "center"
	case AlignEnd:
		return "end"
	case AlignStretch:
		return "stretch"
	}
	return unknownName(int(alignment))
}

func arrangementName(arrangement Arrangement) string {
	switch arrangement {
	case ArrangeStart:
		return "start"
	case ArrangeCenter:
		return "center"
	case ArrangeEnd:
		return "end"
	case ArrangeSpaceBetween:
		return "spaceBetween"
	case ArrangeSpaceAround:
		return "spaceAround"
	}
	return unknownName(int(arrangement))
}

// visualStateNames spells a VisualState as the flags it is made of. Every bit
// is accounted for: the four below VisualUserDefinedBase are ui's own, and
// everything at or above it belongs to the application, which names its own
// states and has not told ui what it calls them - so those are reported as
// user(n) after their bit value rather than as unknown, which would claim ui
// does not know what they are.
func visualStateNames(state VisualState) []string {
	if state == 0 {
		return nil
	}
	var names []string
	for bit := VisualState(1); bit != 0 && bit <= state; bit <<= 1 {
		if state&bit == 0 {
			continue
		}
		switch bit {
		case VisualDisabled:
			names = append(names, "disabled")
		case VisualActive:
			names = append(names, "active")
		case VisualHovered:
			names = append(names, "hovered")
		case VisualPressed:
			names = append(names, "pressed")
		default:
			names = append(names, "user("+strconv.Itoa(int(bit))+")")
		}
	}
	return names
}

// unknownName spells a value no table names, so an enum that grew reads as
// unknown rather than as whichever name happened to be last.
func unknownName(value int) string {
	return "unknown(" + strconv.Itoa(value) + ")"
}
