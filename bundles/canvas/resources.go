package canvas

// OpQueue is the frame-local writable resource used to record layered Canvas
// operations. The Canvas plugin consumes and resets it at the end of each tick.
type OpQueue = opQueue
