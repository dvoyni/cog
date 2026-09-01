package ui

import (
	"iter"
)

// NewElement returns an empty element declaration.
func NewElement() Element {
	return Element{}
}

func (element Element) ID(id ID) Element {
	element.id = id
	return element
}

func (element Element) Width(value float32) Element {
	element.width = pixelSize(value)
	return element
}

func (element Element) WidthRel(value float32) Element {
	element.width = relativeSize(value)
	return element
}

func (element Element) MinWidth(value float32) Element {
	element.minWidth = pixelSize(value)
	return element
}

func (element Element) MinWidthRel(value float32) Element {
	element.minWidth = relativeSize(value)
	return element
}

func (element Element) MaxWidth(value float32) Element {
	element.maxWidth = pixelSize(value)
	return element
}

func (element Element) MaxWidthRel(value float32) Element {
	element.maxWidth = relativeSize(value)
	return element
}

func (element Element) Height(value float32) Element {
	element.height = pixelSize(value)
	return element
}

func (element Element) HeightRel(value float32) Element {
	element.height = relativeSize(value)
	return element
}

func (element Element) MinHeight(value float32) Element {
	element.minHeight = pixelSize(value)
	return element
}

func (element Element) MinHeightRel(value float32) Element {
	element.minHeight = relativeSize(value)
	return element
}

func (element Element) MaxHeight(value float32) Element {
	element.maxHeight = pixelSize(value)
	return element
}

func (element Element) MaxHeightRel(value float32) Element {
	element.maxHeight = relativeSize(value)
	return element
}

func (element Element) Left(value float32) Element {
	element.left = pixelSize(value)
	return element
}

func (element Element) LeftRel(value float32) Element {
	element.left = relativeSize(value)
	return element
}

func (element Element) Right(value float32) Element {
	element.right = pixelSize(value)
	return element
}

func (element Element) RightRel(value float32) Element {
	element.right = relativeSize(value)
	return element
}

func (element Element) Top(value float32) Element {
	element.top = pixelSize(value)
	return element
}

func (element Element) TopRel(value float32) Element {
	element.top = relativeSize(value)
	return element
}

func (element Element) Bottom(value float32) Element {
	element.bottom = pixelSize(value)
	return element
}

func (element Element) BottomRel(value float32) Element {
	element.bottom = relativeSize(value)
	return element
}

func (element Element) PivotLeft(value float32) Element {
	element.pivotLeft = pixelSize(value)
	return element
}

func (element Element) PivotLeftRel(value float32) Element {
	element.pivotLeft = relativeSize(value)
	return element
}

func (element Element) PivotRight(value float32) Element {
	element.pivotRight = pixelSize(value)
	return element
}

func (element Element) PivotRightRel(value float32) Element {
	element.pivotRight = relativeSize(value)
	return element
}

func (element Element) PivotTop(value float32) Element {
	element.pivotTop = pixelSize(value)
	return element
}

func (element Element) PivotTopRel(value float32) Element {
	element.pivotTop = relativeSize(value)
	return element
}

func (element Element) PivotBottom(value float32) Element {
	element.pivotBottom = pixelSize(value)
	return element
}

func (element Element) PivotBottomRel(value float32) Element {
	element.pivotBottom = relativeSize(value)
	return element
}

func (element Element) Padding(values ...float32) Element {
	return element.padding(values, pixelSize)
}

func (element Element) PaddingRel(values ...float32) Element {
	return element.padding(values, relativeSize)
}

func (element Element) padding(values []float32, makeSize func(float32) opt[size]) Element {
	if len(values) == 0 {
		return element
	}
	top, right, bottom, left := values[0], values[0], values[0], values[0]
	if len(values) >= 2 {
		top, bottom = values[0], values[0]
		right, left = values[1], values[1]
	}
	if len(values) >= 3 {
		bottom = values[2]
	}
	if len(values) >= 4 {
		left = values[3]
	}
	element.paddingTop = makeSize(top)
	element.paddingRight = makeSize(right)
	element.paddingBottom = makeSize(bottom)
	element.paddingLeft = makeSize(left)
	return element
}

func (element Element) PaddingLeft(value float32) Element {
	element.paddingLeft = pixelSize(value)
	return element
}

func (element Element) PaddingLeftRel(value float32) Element {
	element.paddingLeft = relativeSize(value)
	return element
}

func (element Element) PaddingRight(value float32) Element {
	element.paddingRight = pixelSize(value)
	return element
}

func (element Element) PaddingRightRel(value float32) Element {
	element.paddingRight = relativeSize(value)
	return element
}

func (element Element) PaddingTop(value float32) Element {
	element.paddingTop = pixelSize(value)
	return element
}

func (element Element) PaddingTopRel(value float32) Element {
	element.paddingTop = relativeSize(value)
	return element
}

func (element Element) PaddingBottom(value float32) Element {
	element.paddingBottom = pixelSize(value)
	return element
}

func (element Element) PaddingBottomRel(value float32) Element {
	element.paddingBottom = relativeSize(value)
	return element
}

func (element Element) Stretch(weight float32) Element {
	element.stretch = someValue(weight)
	return element
}

func (element Element) Shrink(weight float32) Element {
	element.shrink = someValue(weight)
	return element
}

func (element Element) Align(alignment Alignment) Element {
	element.align = someValue(alignment)
	return element
}

func (element Element) Layer(layer int) Element {
	element.layer = someValue(layer)
	return element
}

func (element Element) IgnoreLayout() Element {
	element.ignoreLayout = true
	return element
}

func (element Element) IgnoreClip() Element {
	element.ignoreClip = true
	return element
}

// IgnoreHitTest lets the pointer pass through this element and its
// non-interactive descendants to whatever is drawn below. A descendant with
// its own ID is still an eligible target, since it is matched directly rather
// than found by walking up from an ignored ancestor.
func (element Element) IgnoreHitTest() Element {
	element.ignoreHitTest = true
	return element
}

// PreserveAspectRatio keeps a visual's intrinsic ratio when layout fixes only
// one axis. Image elements enable this automatically.
func (element Element) PreserveAspectRatio() Element {
	element.preserveAspectRatio = true
	return element
}

func (element Element) State(add, remove VisualState) Element {
	element.addState |= add
	element.removeState |= remove
	return element
}

// Children borrows children until Process returns.
func (element Element) Children(children ...Element) Element {
	element.children = append(element.children, children...)
	return element
}

func (element Element) ChildrenSeq(children iter.Seq[Element]) Element {
	for child := range children {
		element.children = append(element.children, child)
	}
	return element
}

func (element Element) Layout(layout Layout) Element {
	element.layout = layout
	return element
}

func (element Element) ChildrenArrangement(arrangement Arrangement) Element {
	element.childrenArrangement = someValue(arrangement)
	return element
}

func (element Element) ChildrenAlignment(alignment Alignment) Element {
	element.childrenAlignment = someValue(alignment)
	return element
}

func (element Element) Gap(value float32) Element {
	element.gap = pixelSize(value)
	return element
}

func (element Element) GapRel(value float32) Element {
	element.gap = relativeSize(value)
	return element
}

func (element Element) Wrap() Element {
	element.wrap = true
	return element
}

func (element Element) Columns(columns int) Element {
	element.columns = someValue(columns)
	return element
}

func (element Element) Rows(rows int) Element {
	element.rows = someValue(rows)
	return element
}

// Visual binds a stateless ParamVisual to this element's params. The params keep
// their type all the way to DefaultSize and Draw.
func (element Element) Visual[T any](visual ParamVisual[T], params T) Element {
	element.visual = boundVisual[T]{visual: visual, params: params}
	return element
}

func (element Element) Fill() Element {
	return element.LeftRel(0).RightRel(0).TopRel(0).BottomRel(0)
}

func (element Element) UserData[T any](userData T) Element {
	element.userData = userData
	return element
}

func pixelSize(value float32) opt[size] {
	return opt[size]{v: size{value: value}, set: true}
}

func relativeSize(value float32) opt[size] {
	return opt[size]{v: size{value: value, relative: true}, set: true}
}

func someValue[T any](value T) opt[T] {
	return opt[T]{v: value, set: true}
}
