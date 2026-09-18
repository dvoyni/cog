package assets

// Descr is the whole of what names one asset: a storage path, or bytes the
// caller already holds, plus the typed bake parameters that make one source
// several assets. It is comparable, and it is the cache key.
//
// It is a key and never a handle. A descriptor is a request; it learns nothing
// from the load it names, and what the load produced is the T a Get returns.
//
// Name wins when it is set. The key is {Name, Params} when Name is not empty
// and {Blob, Params} otherwise, so a Blob supplied beside a Name is a payload
// rather than part of the identity: it is the bytes Load is handed in place of
// a read, and the path the Name spells is not opened. You named it, you own the
// name: two callers supplying different bytes under one Name get whichever
// arrived first, exactly as a path already behaves.
//
// That payload is what lets an asset inside a container be named at all. A
// GLB-embedded image has no path of its own and its bytes are already in hand
// inside the model's parse, so it is named by the model and carries those bytes
// - where a bare Name would make the Library read the whole container and the
// loader re-parse it to reach image N, and a bare Blob would move the key with
// every parse and strand the entry the last one made.
//
// For a blob-named asset the bytes are the identity, with everything Blob's own
// doc says about what that costs a call site that builds them fresh.
//
// The fields are exported, so a plugin may declare its own descriptor as this
// type - type ShaderDescr assets.Descr[ShaderDescrParams] - and anyone can then
// write one down. What keeps that safe is P: a P type's own fields stay
// unexported, so the only descriptor a caller can spell by hand carries a zero
// P, which is the no-options constructor.
type Descr[P comparable] struct {
	// Name is a storage path, and is empty when the asset is named by its Blob.
	Name string
	// Blob is the bytes, when the caller holds them. Beside a Name it is a
	// payload rather than part of the key, and it is still what gets loaded:
	// a descriptor carrying one is never read from storage.
	Blob Blob
	// Params are the bake parameters that make one source several assets: the
	// size a font is baked at, the colour space a texture is uploaded in, the
	// preprocessor supply a shader is flattened with.
	Params P
}

// key is the descriptor reduced to what identifies it. It is what the entry
// table and the report-once key are both spelled with, so a Free naming a path
// retires the entry a Get supplied a payload beside, and forgets the report
// that entry made.
func (d Descr[P]) key() Descr[P] {
	if d.Name != "" {
		d.Blob = Blob{}
	}
	return d
}
