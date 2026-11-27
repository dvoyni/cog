package sfx

// LoopRegion is the span a looping voice repeats between, in seconds. It is a
// fact about a clip, not a parameter of a voice: it rides in the Ogg file's
// Vorbis comment header, and the backend reads it in the same pass that reads
// the clip's duration, channels and rate.
type LoopRegion struct{ Start, End float32 }
