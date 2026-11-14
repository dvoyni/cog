package render

// Inspection is one frame written down in render's terms: its command lists
// by order. render/rendertest's reader answers one for each frame gfx's
// recording adapter recorded.
type Inspection struct {
	CommandLists []InspectedCommandList
}

// InspectedCommandList is one command list of an inspected frame: its passes,
// in the order recorded.
type InspectedCommandList struct {
	Passes []InspectedPass
}

// InspectedPass is one pass of an inspected command list: its label, and its
// colour targets with their loads and stores.
type InspectedPass struct {
	Label        string
	ColorTargets []ColorTarget
}
