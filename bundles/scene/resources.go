package scene

// OpQueue is the frame-local writable resource gameplay records cameras, models
// and meshes into. The scene plugin consumes and republishes it at the end of
// each update tick.
type OpQueue = opQueue
