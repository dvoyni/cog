package ui

import (
	"math"
	"slices"

	"github.com/dvoyni/cog/m"

	"github.com/dvoyni/cog/canvas"
)

type processor struct {
	interactions      []Interaction
	nodes             []layoutNode
	walk              []walkItem
	flow              []flowItem
	lines             []flowLine
	gridColumns       []float32
	gridRows          []float32
	gridColumnOffsets []float32
	gridRowOffsets    []float32
	ordered           []int
	captures          []capture
	hovered           interactionTarget
	capturedSet       map[ID]struct{}
}

func (context *processor) process(lookup canvas.LookupAccess, roots []Element, layers []canvas.Layer, state globalState, queue *canvas.OpQueue) {
	context.interactions = context.interactions[:0]
	context.flatten(roots, layers)
	context.disableGridOverflow()
	context.measure(lookup)
	context.arrange(state.Screen)
	context.resolveClips(state.Screen)
	context.orderNodes()
	context.processInteractions(state.Pointer)
	context.draw(lookup, queue, state.Screen)
}

type globalState struct {
	Screen  Rect
	Pointer pointerState
}

type pointerState struct {
	X, Y   float32
	Events []pointerEvent
}

type pointerEvent struct {
	X, Y   float32
	Button int
	Kind   pointerEventKind
}

type pointerEventKind uint8

const (
	pointerEventNone pointerEventKind = iota
	pointerEventDown
	pointerEventUp
)

type Interaction struct {
	ID       ID
	Kind     InteractionKind
	Button   int
	userData any
}

type InteractionKind uint8

const (
	InteractionNone InteractionKind = iota
	InteractionClick
	InteractionHover
	InteractionDown
	InteractionUp
	InteractionIn
	InteractionOut
)

type layoutNode struct {
	element                       *Element
	parent, firstChild, lastChild int
	nextSibling, subtreeEnd       int
	layer                         canvas.Layer
	order                         int
	rect, clip, childrenClip      Rect
	definiteWidth, definiteHeight bool
	active                        bool
}

type walkItem struct {
	element *Element
	parent  int
	base    canvas.Layer
}

type flowItem struct {
	node        int
	main, cross float32
}

type flowLine struct {
	start, end int
	cross      float32
}

type capture struct {
	interactionTarget
	button int
}

type interactionTarget struct {
	id       ID
	userData any
}

// Measure resolves element against the available area and returns its arranged
// size without drawing or processing interactions.
func Measure(element Element, available m.Vec2) m.Vec2 {
	context := processor{}
	roots := []Element{element}
	context.flatten(roots, nil)
	context.measure(canvas.LookupAccess{})
	context.arrange(Rect{Width: available.X, Height: available.Y})
	if len(context.nodes) == 0 {
		return m.Vec2{}
	}
	return m.Vec2{X: context.nodes[0].rect.Width, Y: context.nodes[0].rect.Height}
}

func (context *processor) flatten(roots []Element, layers []canvas.Layer) {
	context.nodes = context.nodes[:0]
	context.walk = context.walk[:0]
	for index := len(roots) - 1; index >= 0; index-- {
		layer := canvas.Layer(0)
		if index < len(layers) {
			layer = layers[index]
		}
		context.walk = append(context.walk, walkItem{element: &roots[index], parent: -1, base: layer})
	}

	for len(context.walk) > 0 {
		last := len(context.walk) - 1
		item := context.walk[last]
		context.walk = context.walk[:last]

		layer := item.base
		if item.parent >= 0 {
			layer = context.nodes[item.parent].layer
		}
		if item.element.layer.set {
			layer = item.base + canvas.Layer(item.element.layer.v)
		}

		nodeIndex := len(context.nodes)
		context.nodes = append(context.nodes, layoutNode{
			element:     item.element,
			parent:      item.parent,
			firstChild:  -1,
			lastChild:   -1,
			nextSibling: -1,
			layer:       layer,
			order:       nodeIndex,
			active:      true,
		})
		if item.parent >= 0 {
			parent := &context.nodes[item.parent]
			if parent.firstChild < 0 {
				parent.firstChild = nodeIndex
			} else {
				context.nodes[parent.lastChild].nextSibling = nodeIndex
			}
			parent.lastChild = nodeIndex
		}

		children := item.element.children
		for index := len(children) - 1; index >= 0; index-- {
			context.walk = append(context.walk, walkItem{element: &children[index], parent: nodeIndex, base: item.base})
		}
	}

	for index := len(context.nodes) - 1; index >= 0; index-- {
		node := &context.nodes[index]
		node.subtreeEnd = index + 1
		if node.lastChild >= 0 {
			node.subtreeEnd = context.nodes[node.lastChild].subtreeEnd
		}
	}
}

func (context *processor) disableGridOverflow() {
	for nodeIndex := range context.nodes {
		node := &context.nodes[nodeIndex]
		element := node.element
		if !node.active || element.layout != LayoutGrid || !element.columns.set || !element.rows.set {
			continue
		}

		columns := positiveCount(element.columns.v)
		rows := positiveCount(element.rows.v)
		capacity := int(^uint(0) >> 1)
		if rows <= capacity/columns {
			capacity = rows * columns
		}
		count := 0
		for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
			if context.nodes[child].element.ignoreLayout {
				continue
			}
			if count >= capacity {
				for inactive := child; inactive < context.nodes[child].subtreeEnd; inactive++ {
					context.nodes[inactive].active = false
				}
			}
			count++
		}
	}
}

func (context *processor) measure(lookup canvas.LookupAccess) {
	for nodeIndex := len(context.nodes) - 1; nodeIndex >= 0; nodeIndex-- {
		node := &context.nodes[nodeIndex]
		element := node.element
		if !node.active {
			element.intermediate = intermediate{}
			continue
		}

		defaultSize := m.Vec2{}
		if element.visual != nil && (!element.width.set || !element.height.set) {
			defaultSize = element.visual.DefaultSize(lookup)
		}
		contentSize := context.measureContent(nodeIndex)
		paddingWidth := intrinsicSize(element.paddingLeft) + intrinsicSize(element.paddingRight)
		paddingHeight := intrinsicSize(element.paddingTop) + intrinsicSize(element.paddingBottom)

		width := max(defaultSize.X, contentSize.X) + paddingWidth
		if element.width.set {
			width = resolveSize(element.width, 0)
		}
		height := max(defaultSize.Y, contentSize.Y) + paddingHeight
		if element.height.set {
			height = resolveSize(element.height, 0)
		}
		width = constrain(width, element.minWidth, element.maxWidth, 0)
		height = constrain(height, element.minHeight, element.maxHeight, 0)

		element.intermediate = intermediate{
			measured:       m.Vec2{X: width, Y: height},
			contentMinimum: m.Vec2{X: contentSize.X + paddingWidth, Y: contentSize.Y + paddingHeight},
			layer:          node.layer,
			active:         true,
		}
		if element.preserveAspectRatio && defaultSize.X > 0 && defaultSize.Y > 0 {
			element.intermediate.aspectRatio = defaultSize.X / defaultSize.Y
		}
		element.intermediate.measured = arrangedSize(element, Rect{})
	}
}

func (context *processor) measureContent(nodeIndex int) m.Vec2 {
	node := &context.nodes[nodeIndex]
	element := node.element
	switch element.layout {
	case LayoutHorizontal, LayoutVertical:
		horizontal := element.layout == LayoutHorizontal
		var main, cross float32
		count := 0
		for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
			childNode := &context.nodes[child]
			if !childNode.active || childNode.element.ignoreLayout {
				continue
			}
			size := childNode.element.intermediate.measured
			if horizontal {
				main += size.X
				cross = max(cross, size.Y)
			} else {
				main += size.Y
				cross = max(cross, size.X)
			}
			count++
		}
		if count > 1 {
			main += intrinsicSignedSize(element.gap) * float32(count-1)
		}
		main += context.mainFromCross(nodeIndex, crossBasis(element, cross, horizontal), horizontal)
		if horizontal {
			return m.Vec2{X: main, Y: cross}
		}
		return m.Vec2{X: cross, Y: main}

	case LayoutGrid:
		count := 0
		var maxWidth, maxHeight float32
		for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
			childNode := &context.nodes[child]
			if !childNode.active || childNode.element.ignoreLayout {
				continue
			}
			size := childNode.element.intermediate.measured
			maxWidth = max(maxWidth, size.X)
			maxHeight = max(maxHeight, size.Y)
			count++
		}
		width, widthDefinite := absoluteSize(element.width)
		height, heightDefinite := absoluteSize(element.height)
		columns, rows, columnMajor := gridShape(element, count, maxWidth, maxHeight, width, height, widthDefinite, heightDefinite)
		context.prepareGridTracks(columns, rows)
		itemIndex := 0
		for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
			childNode := &context.nodes[child]
			if !childNode.active || childNode.element.ignoreLayout {
				continue
			}
			column, row := gridCell(itemIndex, columns, rows, columnMajor)
			size := childNode.element.intermediate.measured
			context.gridColumns[column] = max(context.gridColumns[column], size.X)
			context.gridRows[row] = max(context.gridRows[row], size.Y)
			itemIndex++
		}
		gap := intrinsicSignedSize(element.gap)
		return m.Vec2{
			X: sumTracks(context.gridColumns) + gap*float32(max(columns-1, 0)),
			Y: sumTracks(context.gridRows) + gap*float32(max(rows-1, 0)),
		}

	case LayoutNone:
		var width, height float32
		for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
			childNode := &context.nodes[child]
			if !childNode.active || childNode.element.ignoreLayout {
				continue
			}
			childElement := childNode.element
			childWidth := childElement.intermediate.measured.X
			if explicit, ok := absoluteSize(childElement.width); ok {
				childWidth = explicit
			} else if !childElement.width.set && relativeEdges(childElement.left, childElement.right) {
				childWidth = constrain(0, childElement.minWidth, childElement.maxWidth, 0)
			}
			childHeight := childElement.intermediate.measured.Y
			if explicit, ok := absoluteSize(childElement.height); ok {
				childHeight = explicit
			} else if !childElement.height.set && relativeEdges(childElement.top, childElement.bottom) {
				childHeight = constrain(0, childElement.minHeight, childElement.maxHeight, 0)
			}
			width = max(width, intrinsicSize(childElement.left)+childWidth+intrinsicSize(childElement.right))
			height = max(height, intrinsicSize(childElement.top)+childHeight+intrinsicSize(childElement.bottom))
		}
		return m.Vec2{X: width, Y: height}
	}
	return m.Vec2{}
}

func relativeEdges(start, end opt[size]) bool {
	return start.set && start.v.relative && end.set && end.v.relative
}

// crossBasis is what a child's relative cross size measures against: the row's
// own cross where it has one, and otherwise the cross its other children have
// just settled, which is the cross the row will be arranged to.
func crossBasis(element *Element, contentCross float32, horizontal bool) float32 {
	dimension := element.width
	padding := intrinsicSize(element.paddingLeft) + intrinsicSize(element.paddingRight)
	if horizontal {
		dimension = element.height
		padding = intrinsicSize(element.paddingTop) + intrinsicSize(element.paddingBottom)
	}
	if explicit, ok := absoluteSize(dimension); ok {
		return max(explicit-padding, 0)
	}
	return contentCross
}

// mainFromCross reserves the main axis of children that take their cross from
// the row and their main from their ratio. Such a child measures as zero on
// both axes — a relative size has no basis until the row is arranged, and a
// ratio applied to zero is zero — so the row measures short of what it then
// arranges, by the whole main axis of every child sized that way. The row is
// then too narrow for what it lays out, and the tail of it is pushed outside
// the box and clipped. Resolving those children against the cross the rest of
// the row has settled costs one more pass over the children and reserves what
// each of them will take.
func (context *processor) mainFromCross(nodeIndex int, cross float32, horizontal bool) float32 {
	if cross <= 0 {
		return 0
	}
	node := &context.nodes[nodeIndex]
	var reserved float32
	for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
		childNode := &context.nodes[child]
		childElement := childNode.element
		if !childNode.active || childElement.ignoreLayout || childElement.intermediate.aspectRatio <= 0 {
			continue
		}
		if !isRelative(crossDimension(childElement, horizontal)) || mainSizeSet(childElement, horizontal) {
			continue
		}
		// The call arrange will make, against a containing rect known on the
		// cross axis alone: a relative main size still resolves against a zero
		// basis, as it does everywhere else in the measure pass.
		containing := Rect{Width: cross}
		if horizontal {
			containing = Rect{Height: cross}
		}
		size := arrangedSize(childElement, containing)
		main, measured := size.Y, childElement.intermediate.measured.Y
		if horizontal {
			main, measured = size.X, childElement.intermediate.measured.X
		}
		reserved += max(main-measured, 0)
	}
	return reserved
}

func (context *processor) arrange(screen Rect) {
	for nodeIndex := range context.nodes {
		node := &context.nodes[nodeIndex]
		if !node.active {
			continue
		}
		if node.parent < 0 {
			context.arrangeAbsolute(nodeIndex, screen)
		}
		if node.element.stayOnScreen {
			node.rect = clampToBounds(node.rect, screen)
		}
		context.arrangeChildren(nodeIndex)
	}
}

func (context *processor) arrangeChildren(nodeIndex int) {
	node := &context.nodes[nodeIndex]
	content := elementContentRect(node.element, node.rect)
	switch node.element.layout {
	case LayoutHorizontal:
		context.arrangeFlow(nodeIndex, true)
	case LayoutVertical:
		context.arrangeFlow(nodeIndex, false)
	case LayoutGrid:
		context.arrangeGrid(nodeIndex)
	default:
		for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
			childNode := &context.nodes[child]
			if !childNode.active {
				continue
			}
			parent := content
			if childNode.element.ignoreLayout {
				parent = node.rect
			}
			context.arrangeAbsolute(child, parent)
		}
	}
}

func (context *processor) arrangeAbsolute(nodeIndex int, parent Rect) {
	node := &context.nodes[nodeIndex]
	element := node.element
	natural := arrangedSize(element, parent)
	x, width, definiteWidth := resolveAxis(parent.X, parent.Width, natural.X,
		element.width, element.minWidth, element.maxWidth, element.left, element.right, element.pivotLeft, element.pivotRight)
	y, height, definiteHeight := resolveAxis(parent.Y, parent.Height, natural.Y,
		element.height, element.minHeight, element.maxHeight, element.top, element.bottom, element.pivotTop, element.pivotBottom)
	node.rect = Rect{X: x, Y: y, Width: width, Height: height}
	node.definiteWidth = definiteWidth
	node.definiteHeight = definiteHeight
}

func (context *processor) arrangeFlow(nodeIndex int, horizontal bool) {
	node := &context.nodes[nodeIndex]
	element := node.element
	content := elementContentRect(element, node.rect)
	availableMain := content.Height
	availableCross := content.Width
	canWrap := element.wrap && node.definiteHeight
	crossDefinite := node.definiteWidth
	if horizontal {
		availableMain, availableCross = content.Width, content.Height
		canWrap = element.wrap && node.definiteWidth
		crossDefinite = node.definiteHeight
	}
	context.flow = context.flow[:0]
	for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
		childNode := &context.nodes[child]
		if !childNode.active {
			continue
		}
		if childNode.element.ignoreLayout {
			// Against the parent's rect, not its content box: a child that ignores
			// layout ignores the parent's padding too, so the same declaration means
			// the same thing under a column as it does under an Overlay.
			context.arrangeAbsolute(child, node.rect)
			continue
		}
		size := arrangedSize(childNode.element, content)
		alignment := valueOr(element.childrenAlignment, AlignStart)
		if childNode.element.align.set {
			alignment = childNode.element.align.v
		}
		if crossDefinite && !canWrap && alignment == AlignStretch &&
			!crossSizeSet(childNode.element, horizontal) && !mainSizeSet(childNode.element, horizontal) {
			size = sizeFromStretchedCross(childNode.element, size, availableCross, horizontal)
		}
		item := flowItem{node: child, main: size.Y, cross: size.X}
		if horizontal {
			item.main, item.cross = size.X, size.Y
		}
		context.flow = append(context.flow, item)
	}
	if len(context.flow) == 0 {
		return
	}

	fixedGap := resolveSignedSize(element.gap, availableMain)
	context.lines = context.lines[:0]
	lineStart := 0
	var lineMain, lineCross float32
	for itemIndex := range context.flow {
		item := context.flow[itemIndex]
		itemGap := float32(0)
		if itemIndex > lineStart {
			itemGap = fixedGap
		}
		if canWrap && itemIndex > lineStart && lineMain+itemGap+item.main > availableMain {
			context.lines = append(context.lines, flowLine{start: lineStart, end: itemIndex, cross: lineCross})
			lineStart = itemIndex
			lineMain = 0
			lineCross = 0
			itemGap = 0
		}
		lineMain += itemGap + item.main
		lineCross = max(lineCross, item.cross)
	}
	context.lines = append(context.lines, flowLine{start: lineStart, end: len(context.flow), cross: lineCross})
	if !canWrap {
		context.lines[0].cross = availableCross
	}

	crossPosition := float32(0)
	for lineIndex := range context.lines {
		line := context.lines[lineIndex]
		items := context.flow[line.start:line.end]
		itemsAvailable := max(availableMain-fixedGap*float32(max(len(items)-1, 0)), 0)
		context.distribute(items, itemsAvailable, horizontal)

		var usedMain float32
		for itemIndex := range items {
			usedMain += items[itemIndex].main
		}
		freeMain := max(itemsAvailable-usedMain, 0)
		mainPosition, arrangedGap := arrangementSpacing(valueOr(element.childrenArrangement, ArrangeStart), freeMain, len(items))
		for itemIndex := range items {
			item := &items[itemIndex]
			child := &context.nodes[item.node]
			alignment := valueOr(element.childrenAlignment, AlignStart)
			if child.element.align.set {
				alignment = child.element.align.v
			}
			itemCross := item.cross
			if alignment == AlignStretch && !crossSizeSet(child.element, horizontal) {
				itemCross = line.cross
			}
			crossFree := max(line.cross-itemCross, 0)
			childCross := crossPosition + alignmentOffset(alignment, crossFree)
			if horizontal {
				child.rect = Rect{X: content.X + mainPosition, Y: content.Y + childCross, Width: item.main, Height: itemCross}
				child.definiteWidth = child.element.width.set
				child.definiteHeight = child.element.height.set ||
					(alignment == AlignStretch && node.definiteHeight && !canWrap)
			} else {
				child.rect = Rect{X: content.X + childCross, Y: content.Y + mainPosition, Width: itemCross, Height: item.main}
				child.definiteWidth = child.element.width.set ||
					(alignment == AlignStretch && node.definiteWidth && !canWrap)
				child.definiteHeight = child.element.height.set
			}
			mainPosition += item.main + fixedGap + arrangedGap
		}
		crossPosition += line.cross + fixedGap
	}
}

func sizeFromStretchedCross(element *Element, size m.Vec2, cross float32, horizontal bool) m.Vec2 {
	if element.intermediate.aspectRatio <= 0 {
		return size
	}
	if horizontal {
		size.X = max(cross*element.intermediate.aspectRatio, element.intermediate.contentMinimum.X)
		size.X = constrain(size.X, element.minWidth, element.maxWidth, 0)
		return size
	}
	size.Y = max(cross/element.intermediate.aspectRatio, element.intermediate.contentMinimum.Y)
	size.Y = constrain(size.Y, element.minHeight, element.maxHeight, 0)
	return size
}

func (context *processor) distribute(items []flowItem, available float32, horizontal bool) {
	var used float32
	for index := range items {
		used += items[index].main
	}
	delta := available - used
	if delta == 0 {
		return
	}
	growing := delta > 0
	remaining := abs(delta)
	for range len(items) {
		var totalWeight float32
		for index := range items {
			item := &items[index]
			element := context.nodes[item.node].element
			weight := weightFor(element, growing)
			minimum, maximum := mainLimits(element, horizontal, available)
			if weight > 0 && ((growing && item.main < maximum) || (!growing && item.main > minimum)) {
				totalWeight += weight
			}
		}
		if totalWeight == 0 || remaining <= 0.0001 {
			return
		}

		consumed := float32(0)
		for index := range items {
			item := &items[index]
			element := context.nodes[item.node].element
			weight := weightFor(element, growing)
			minimum, maximum := mainLimits(element, horizontal, available)
			if weight <= 0 || (growing && item.main >= maximum) || (!growing && item.main <= minimum) {
				continue
			}
			share := remaining * weight / totalWeight
			before := item.main
			if growing {
				item.main = min(item.main+share, maximum)
				consumed += item.main - before
			} else {
				item.main = max(item.main-share, minimum)
				consumed += before - item.main
			}
		}
		if consumed <= 0.0001 {
			return
		}
		remaining -= consumed
	}
}

func (context *processor) arrangeGrid(nodeIndex int) {
	node := &context.nodes[nodeIndex]
	element := node.element
	content := elementContentRect(element, node.rect)
	context.flow = context.flow[:0]
	var maxWidth, maxHeight float32
	for child := node.firstChild; child >= 0; child = context.nodes[child].nextSibling {
		childNode := &context.nodes[child]
		if !childNode.active {
			continue
		}
		if childNode.element.ignoreLayout {
			// Against the parent's rect, not its content box: a child that ignores
			// layout ignores the parent's padding too, so the same declaration means
			// the same thing under a column as it does under an Overlay.
			context.arrangeAbsolute(child, node.rect)
			continue
		}
		size := childNode.element.intermediate.measured
		maxWidth = max(maxWidth, size.X)
		maxHeight = max(maxHeight, size.Y)
		context.flow = append(context.flow, flowItem{node: child})
	}
	if len(context.flow) == 0 {
		return
	}

	columns, rows, columnMajor := gridShape(element, len(context.flow), maxWidth, maxHeight,
		content.Width, content.Height, node.definiteWidth, node.definiteHeight)
	context.prepareGridTracks(columns, rows)
	for itemIndex := range context.flow {
		column, row := gridCell(itemIndex, columns, rows, columnMajor)
		size := context.nodes[context.flow[itemIndex].node].element.intermediate.measured
		context.gridColumns[column] = max(context.gridColumns[column], size.X)
		context.gridRows[row] = max(context.gridRows[row], size.Y)
	}
	gapBasis := content.Width
	if columnMajor {
		gapBasis = content.Height
	}
	gap := resolveSignedSize(element.gap, gapBasis)
	if node.definiteWidth {
		expandTracks(context.gridColumns, content.Width-gap*float32(max(columns-1, 0)))
	}
	if node.definiteHeight {
		expandTracks(context.gridRows, content.Height-gap*float32(max(rows-1, 0)))
	}
	context.prepareGridOffsets(gap)
	for itemIndex := range context.flow {
		column, row := gridCell(itemIndex, columns, rows, columnMajor)
		cell := Rect{
			X:      content.X + context.gridColumnOffsets[column],
			Y:      content.Y + context.gridRowOffsets[row],
			Width:  context.gridColumns[column],
			Height: context.gridRows[row],
		}
		child := &context.nodes[context.flow[itemIndex].node]
		size := arrangedSize(child.element, cell)
		mainAlignment := arrangementAlignment(valueOr(element.childrenArrangement, ArrangeStart))
		crossAlignment := valueOr(element.childrenAlignment, AlignStart)
		if child.element.align.set {
			crossAlignment = child.element.align.v
		}
		if crossAlignment == AlignStretch {
			if !child.element.width.set {
				size.X = cell.Width
			}
			if !child.element.height.set {
				size.Y = cell.Height
			}
		}
		if columnMajor {
			child.rect = Rect{
				X:      cell.X + alignmentOffset(crossAlignment, max(cell.Width-size.X, 0)),
				Y:      cell.Y + alignmentOffset(mainAlignment, max(cell.Height-size.Y, 0)),
				Width:  size.X,
				Height: size.Y,
			}
		} else {
			child.rect = Rect{
				X:      cell.X + alignmentOffset(mainAlignment, max(cell.Width-size.X, 0)),
				Y:      cell.Y + alignmentOffset(crossAlignment, max(cell.Height-size.Y, 0)),
				Width:  size.X,
				Height: size.Y,
			}
		}
		child.definiteWidth = child.element.width.set
		child.definiteHeight = child.element.height.set
	}
}

func (context *processor) prepareGridTracks(columns, rows int) {
	context.gridColumns = resizeAndClear(context.gridColumns, columns)
	context.gridRows = resizeAndClear(context.gridRows, rows)
	context.gridColumnOffsets = resizeAndClear(context.gridColumnOffsets, columns)
	context.gridRowOffsets = resizeAndClear(context.gridRowOffsets, rows)
}

func (context *processor) prepareGridOffsets(gap float32) {
	for index := 1; index < len(context.gridColumns); index++ {
		context.gridColumnOffsets[index] = context.gridColumnOffsets[index-1] + context.gridColumns[index-1] + gap
	}
	for index := 1; index < len(context.gridRows); index++ {
		context.gridRowOffsets[index] = context.gridRowOffsets[index-1] + context.gridRows[index-1] + gap
	}
}

func expandTracks(tracks []float32, available float32) {
	extra := available - sumTracks(tracks)
	if extra <= 0 || len(tracks) == 0 {
		return
	}
	share := extra / float32(len(tracks))
	for index := range tracks {
		tracks[index] += share
	}
}

func resizeAndClear(values []float32, length int) []float32 {
	if cap(values) < length {
		return make([]float32, length)
	}
	values = values[:length]
	clear(values)
	return values
}

func gridCell(index, columns, rows int, columnMajor bool) (column, row int) {
	if columnMajor {
		return index / rows, index % rows
	}
	return index % columns, index / columns
}

func sumTracks(tracks []float32) float32 {
	var total float32
	for index := range tracks {
		total += tracks[index]
	}
	return total
}

func (context *processor) resolveClips(screen Rect) {
	for nodeIndex := range context.nodes {
		node := &context.nodes[nodeIndex]
		element := node.element
		if !node.active {
			element.intermediate = intermediate{}
			continue
		}
		clip := screen
		if node.parent >= 0 {
			clip = context.nodes[node.parent].childrenClip
		}
		if element.ignoreClip {
			clip = screen
		}
		node.clip = clip
		node.childrenClip = intersect(clip, node.rect)
		inheritedState := VisualState(0)
		if node.parent >= 0 {
			inheritedState = context.nodes[node.parent].element.intermediate.state.VisualState
		}
		element.intermediate.state = State{
			VisualState: transformVisualState(inheritedState, element),
			Rect:        node.rect, ContentRect: elementContentRect(element, node.rect),
			ClipRect: clip, Layer: node.layer,
		}
		element.intermediate.layer = node.layer
		element.intermediate.active = true
	}
}

func (context *processor) orderNodes() {
	context.ordered = context.ordered[:0]
	for nodeIndex := range context.nodes {
		if context.nodes[nodeIndex].active {
			context.ordered = append(context.ordered, nodeIndex)
		}
	}
	slices.SortFunc(context.ordered, func(left, right int) int {
		leftNode := &context.nodes[left]
		rightNode := &context.nodes[right]
		if leftNode.layer < rightNode.layer {
			return -1
		}
		if leftNode.layer > rightNode.layer {
			return 1
		}
		if leftNode.order < rightNode.order {
			return -1
		}
		if leftNode.order > rightNode.order {
			return 1
		}
		return 0
	})
}

func (context *processor) processInteractions(pointer pointerState) {
	context.ensureInteractionSets()
	for eventIndex := range pointer.Events {
		event := pointer.Events[eventIndex]
		switch event.Kind {
		case pointerEventDown:
			context.removeCaptures(event.Button)
			target := context.hitTarget(event.X, event.Y)
			if target.id == "" {
				continue
			}
			context.interactions = append(context.interactions, Interaction{ID: target.id, Kind: InteractionDown, Button: event.Button, userData: target.userData})
			context.captures = append(context.captures, capture{interactionTarget: target, button: event.Button})

		case pointerEventUp:
			target := context.hitTarget(event.X, event.Y)
			for captureIndex := range context.captures {
				captured := context.captures[captureIndex]
				if captured.button != event.Button {
					continue
				}
				context.interactions = append(context.interactions, Interaction{ID: captured.id, Kind: InteractionUp, Button: event.Button, userData: captured.userData})
				if target.id == captured.id {
					context.interactions = append(context.interactions, Interaction{ID: captured.id, Kind: InteractionClick, Button: event.Button, userData: captured.userData})
				}
			}
			context.removeCaptures(event.Button)
		}
	}

	clear(context.capturedSet)
	for captureIndex := range context.captures {
		context.capturedSet[context.captures[captureIndex].id] = struct{}{}
	}
	previous := context.hovered
	context.hovered = context.hitTarget(pointer.X, pointer.Y)

	if context.hovered.id != "" {
		if context.hovered.id != previous.id {
			context.interactions = append(context.interactions, Interaction{ID: context.hovered.id, Kind: InteractionIn, Button: -1, userData: context.hovered.userData})
		}
		context.interactions = append(context.interactions, Interaction{ID: context.hovered.id, Kind: InteractionHover, Button: -1, userData: context.hovered.userData})
	}
	if previous.id != "" && previous.id != context.hovered.id {
		context.interactions = append(context.interactions, Interaction{ID: previous.id, Kind: InteractionOut, Button: -1, userData: previous.userData})
	}
	for nodeIndex := range context.nodes {
		node := &context.nodes[nodeIndex]
		if !node.active {
			continue
		}
		inheritedState := VisualState(0)
		if node.parent >= 0 {
			inheritedState = context.nodes[node.parent].element.intermediate.state.VisualState
		}
		systemState := VisualState(0)
		if node.element.id != "" && !node.element.intermediate.state.Has(VisualDisabled) {
			if _, pressed := context.capturedSet[node.element.id]; pressed {
				systemState |= VisualPressed
			}
			if context.hovered.id == node.element.id {
				systemState |= VisualHovered
			}
		}
		node.element.intermediate.state.VisualState = transformVisualState(inheritedState|systemState, node.element)
	}
}

func (context *processor) ensureInteractionSets() {
	if context.capturedSet == nil {
		context.capturedSet = make(map[ID]struct{})
	}
}

func (context *processor) removeCaptures(button int) {
	kept := context.captures[:0]
	for captureIndex := range context.captures {
		if context.captures[captureIndex].button != button {
			kept = append(kept, context.captures[captureIndex])
		}
	}
	context.captures = kept
}

// hitTarget resolves who owns the pointer at a point. Every element absorbs the
// pointer over its visible rect, so only the topmost one is considered; when it
// carries no ID the input passes up to its nearest ancestor that does. The zero
// target means neither that element nor any ancestor is an enabled target, so
// the input is swallowed rather than falling through to whatever is drawn below.
// An element marked IgnoreHitTest is not itself a target and does not swallow
// the input on its ancestors' behalf either; reaching one while walking up
// without having found an ID lets the pointer fall through to whatever is
// drawn beneath, instead of stopping the search there.
func (context *processor) hitTarget(x, y float32) interactionTarget {
outer:
	for orderIndex := len(context.ordered) - 1; orderIndex >= 0; orderIndex-- {
		nodeIndex := context.ordered[orderIndex]
		if !pointInVisibleRect(&context.nodes[nodeIndex], x, y) {
			continue
		}
		for ; nodeIndex >= 0; nodeIndex = context.nodes[nodeIndex].parent {
			element := context.nodes[nodeIndex].element
			if element.id == "" {
				if element.ignoreHitTest {
					continue outer
				}
				continue
			}
			if element.intermediate.state.Has(VisualDisabled) {
				return interactionTarget{}
			}
			return interactionTarget{id: element.id, userData: element.userData}
		}
		return interactionTarget{}
	}
	return interactionTarget{}
}

func transformVisualState(state VisualState, element *Element) VisualState {
	return state&^element.removeState | element.addState
}

func (context *processor) draw(lookup canvas.LookupAccess, queue *canvas.OpQueue, screen Rect) {
	var previousLayer canvas.Layer
	hasLayer := false
	for orderIndex := range context.ordered {
		node := &context.nodes[context.ordered[orderIndex]]
		if node.element.visual == nil || !rectVisible(node.rect, node.clip) {
			continue
		}
		if queue != nil {
			if !hasLayer || node.layer != previousLayer {
				queue.SetLayerTransform(node.layer, m.Rect(screen), canvas.AspectStretch)
				previousLayer = node.layer
				hasLayer = true
			}
			queue.SetClip(node.clip)
		}
		node.element.visual.Draw(lookup, queue, node.element.intermediate.state)
		if queue != nil {
			queue.RemoveClip()
		}
	}
}

func pointInVisibleRect(node *layoutNode, x, y float32) bool {
	visible := intersect(node.rect, node.clip)
	return visible.Width > 0 && visible.Height > 0 &&
		x >= visible.X && x < visible.X+visible.Width &&
		y >= visible.Y && y < visible.Y+visible.Height
}

func rectVisible(rect, clip Rect) bool {
	visible := intersect(rect, clip)
	return visible.Width > 0 && visible.Height > 0
}

func arrangedSize(element *Element, containing Rect) m.Vec2 {
	width := element.intermediate.measured.X
	if element.width.set {
		width = resolveSize(element.width, containing.Width)
	}
	height := element.intermediate.measured.Y
	if element.height.set {
		height = resolveSize(element.height, containing.Height)
	}
	width = constrain(width, element.minWidth, element.maxWidth, containing.Width)
	height = constrain(height, element.minHeight, element.maxHeight, containing.Height)
	if element.width.set && !element.height.set && element.intermediate.aspectRatio > 0 {
		height = max(width/element.intermediate.aspectRatio, element.intermediate.contentMinimum.Y)
		height = constrain(height, element.minHeight, element.maxHeight, containing.Height)
	} else if element.height.set && !element.width.set && element.intermediate.aspectRatio > 0 {
		width = max(height*element.intermediate.aspectRatio, element.intermediate.contentMinimum.X)
		width = constrain(width, element.minWidth, element.maxWidth, containing.Width)
	}
	return m.Vec2{X: width, Y: height}
}

func resolveAxis(parentStart, parentLength, natural float32, dimension, minimum, maximum, start, end, pivotStart, pivotEnd opt[size]) (float32, float32, bool) {
	length := natural
	definite := dimension.set || (start.set && end.set)
	if dimension.set {
		length = resolveSize(dimension, parentLength)
	} else if start.set && end.set {
		denominator := 1 - relativeValue(pivotStart) - relativeValue(pivotEnd)
		if denominator > 0 {
			length = (parentLength - resolveSignedSize(start, parentLength) - resolveSignedSize(end, parentLength) +
				absoluteValue(pivotStart) + absoluteValue(pivotEnd)) / denominator
		} else {
			length = 0
		}
	}
	length = constrain(length, minimum, maximum, parentLength)

	position := parentStart - resolveSize(pivotStart, length)
	if start.set {
		position = parentStart + resolveSignedSize(start, parentLength) - resolveSize(pivotStart, length)
	} else if end.set {
		position = parentStart + parentLength - resolveSignedSize(end, parentLength) - length + resolveSize(pivotEnd, length)
	}
	return position, length, definite
}

func resolveSize(value opt[size], basis float32) float32 {
	if !value.set {
		return 0
	}
	resolved := value.v.value
	if value.v.relative {
		resolved *= basis
	}
	return max(resolved, 0)
}

func resolveSignedSize(value opt[size], basis float32) float32 {
	if !value.set {
		return 0
	}
	resolved := value.v.value
	if value.v.relative {
		resolved *= basis
	}
	return resolved
}

func constrain(value float32, minimum, maximum opt[size], basis float32) float32 {
	value = max(value, 0)
	minimumValue := float32(0)
	if minimum.set {
		minimumValue = resolveSize(minimum, basis)
	}
	maximumValue := float32(math.Inf(1))
	if maximum.set {
		maximumValue = resolveSize(maximum, basis)
	}
	if minimumValue > maximumValue {
		maximumValue = minimumValue
	}
	return min(max(value, minimumValue), maximumValue)
}

func relativeValue(value opt[size]) float32 {
	if value.set && value.v.relative {
		return value.v.value
	}
	return 0
}

func absoluteValue(value opt[size]) float32 {
	if value.set && !value.v.relative {
		return max(value.v.value, 0)
	}
	return 0
}

func absoluteSize(value opt[size]) (float32, bool) {
	if !value.set || value.v.relative {
		return 0, false
	}
	return max(value.v.value, 0), true
}

func gridShape(element *Element, count int, maxWidth, maxHeight, width, height float32, widthDefinite, heightDefinite bool) (int, int, bool) {
	if element.columns.set && element.rows.set {
		return positiveCount(element.columns.v), positiveCount(element.rows.v), false
	}
	if element.columns.set {
		columns := positiveCount(element.columns.v)
		return columns, max(ceilDiv(count, columns), 1), false
	}
	if element.rows.set {
		rows := positiveCount(element.rows.v)
		return max(ceilDiv(count, rows), 1), rows, true
	}
	if count <= 0 {
		return 1, 1, false
	}
	if widthDefinite && heightDefinite {
		columns := bestGridColumns(count, width, height, maxWidth, maxHeight)
		return columns, ceilDiv(count, columns), false
	}
	if widthDefinite && maxWidth > 0 {
		columns := max(int(width/maxWidth), 1)
		return columns, max(ceilDiv(count, columns), 1), false
	}
	if heightDefinite && maxHeight > 0 {
		rows := max(int(height/maxHeight), 1)
		return max(ceilDiv(count, rows), 1), rows, true
	}
	columns := max(int(math.Ceil(math.Sqrt(float64(count)))), 1)
	return columns, max(ceilDiv(count, columns), 1), false
}

func bestGridColumns(count int, width, height, childWidth, childHeight float32) int {
	if width <= 0 || height <= 0 || childWidth <= 0 || childHeight <= 0 {
		return max(int(math.Ceil(math.Sqrt(float64(count)))), 1)
	}
	ideal := math.Sqrt(float64(count) * float64(width) * float64(childHeight) / (float64(height) * float64(childWidth)))
	center := int(math.Round(ideal))
	bestColumns := max(min(center, count), 1)
	bestScore := math.Inf(1)
	for candidate := center - 2; candidate <= center+2; candidate++ {
		columns := max(min(candidate, count), 1)
		rows := ceilDiv(count, columns)
		cellRatio := float64(width/float32(columns)) / float64(height/float32(rows))
		targetRatio := float64(childWidth / childHeight)
		score := math.Abs(math.Log(cellRatio / targetRatio))
		if score < bestScore {
			bestScore = score
			bestColumns = columns
		}
	}
	return bestColumns
}

func arrangementSpacing(arrangement Arrangement, free float32, count int) (float32, float32) {
	switch arrangement {
	case ArrangeCenter:
		return free / 2, 0
	case ArrangeEnd:
		return free, 0
	case ArrangeSpaceBetween:
		if count > 1 {
			return 0, free / float32(count-1)
		}
	case ArrangeSpaceAround:
		if count > 0 {
			gap := free / float32(count)
			return gap / 2, gap
		}
	}
	return 0, 0
}

func arrangementAlignment(arrangement Arrangement) Alignment {
	switch arrangement {
	case ArrangeEnd:
		return AlignEnd
	case ArrangeCenter, ArrangeSpaceBetween, ArrangeSpaceAround:
		return AlignCenter
	default:
		return AlignStart
	}
}

func alignmentOffset(alignment Alignment, free float32) float32 {
	switch alignment {
	case AlignCenter:
		return free / 2
	case AlignEnd:
		return free
	default:
		return 0
	}
}

func intrinsicSize(value opt[size]) float32 {
	if !value.set || value.v.relative {
		return 0
	}
	return max(value.v.value, 0)
}

func intrinsicSignedSize(value opt[size]) float32 {
	if !value.set || value.v.relative {
		return 0
	}
	return value.v.value
}

func elementContentRect(element *Element, outer Rect) Rect {
	left := resolveSize(element.paddingLeft, outer.Width)
	right := resolveSize(element.paddingRight, outer.Width)
	top := resolveSize(element.paddingTop, outer.Height)
	bottom := resolveSize(element.paddingBottom, outer.Height)
	return Rect{
		X: outer.X + left, Y: outer.Y + top,
		Width: max(outer.Width-left-right, 0), Height: max(outer.Height-top-bottom, 0),
	}
}

func crossSizeSet(element *Element, horizontal bool) bool {
	return crossDimension(element, horizontal).set
}

func crossDimension(element *Element, horizontal bool) opt[size] {
	if horizontal {
		return element.height
	}
	return element.width
}

func isRelative(value opt[size]) bool {
	return value.set && value.v.relative
}

func mainSizeSet(element *Element, horizontal bool) bool {
	if horizontal {
		return element.width.set
	}
	return element.height.set
}

func weightFor(element *Element, growing bool) float32 {
	weight := element.shrink
	if growing {
		weight = element.stretch
	}
	if !weight.set {
		return 0
	}
	return max(weight.v, 0)
}

func mainLimits(element *Element, horizontal bool, basis float32) (float32, float32) {
	minimum, maximum := element.minHeight, element.maxHeight
	if horizontal {
		minimum, maximum = element.minWidth, element.maxWidth
	}
	minimumValue := float32(0)
	if minimum.set {
		minimumValue = resolveSize(minimum, basis)
	}
	maximumValue := float32(math.Inf(1))
	if maximum.set {
		maximumValue = resolveSize(maximum, basis)
	}
	if minimumValue > maximumValue {
		maximumValue = minimumValue
	}
	return minimumValue, maximumValue
}

// clampToBounds slides rect back inside bounds without resizing it. A rect
// larger than bounds on an axis keeps its start edge and overflows the far
// one, so an oversized element stays anchored instead of jittering between two
// impossible fits.
func clampToBounds(rect, bounds Rect) Rect {
	rect.X = clampAxis(rect.X, rect.Width, bounds.X, bounds.Width)
	rect.Y = clampAxis(rect.Y, rect.Height, bounds.Y, bounds.Height)
	return rect
}

func clampAxis(start, length, boundsStart, boundsLength float32) float32 {
	if length >= boundsLength {
		return boundsStart
	}
	return min(max(start, boundsStart), boundsStart+boundsLength-length)
}

func intersect(left, right Rect) Rect {
	x := max(left.X, right.X)
	y := max(left.Y, right.Y)
	rightEdge := min(left.X+left.Width, right.X+right.Width)
	bottomEdge := min(left.Y+left.Height, right.Y+right.Height)
	return Rect{X: x, Y: y, Width: max(rightEdge-x, 0), Height: max(bottomEdge-y, 0)}
}

func valueOr[T any](value opt[T], fallback T) T {
	if value.set {
		return value.v
	}
	return fallback
}

func positiveCount(value int) int {
	return max(value, 1)
}

func ceilDiv(value, divisor int) int {
	if value <= 0 {
		return 0
	}
	return 1 + (value-1)/divisor
}

func abs(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}
