package internal

type parameterSource uint8

const (
	parameterMissing parameterSource = iota
	parameterMaterial
	parameterDraw
)

type parameterRef struct {
	source parameterSource
	index  int
}

func (r parameterRef) value(material, draw []ParameterDescr) *ParameterDescr {
	switch r.source {
	case parameterMaterial:
		return &material[r.index]
	case parameterDraw:
		return &draw[r.index]
	default:
		return nil
	}
}

type plannedUniform struct {
	offset int
	param  parameterRef
}

type plannedResourceKind uint8

const (
	plannedTexture plannedResourceKind = iota
	plannedBuffer
)

type plannedResource struct {
	kind    plannedResourceKind
	group   int
	binding int
	param   parameterRef
	// name is the binding's declared name, kept because a binding no parameter
	// names has no parameter to read it off, and the report has to say which
	// one went unfilled.
	name string
	// view is the dimension a texture binding declares. It is the static half of
	// the dimension check and a pure function of the shader, so it is cached
	// here with the rest of the plan; the layer count it is compared against is
	// a per-draw value and cannot be. Meaningless for a buffer binding.
	view TextureViewDimension
}

// plannedSampler is one reflected sampler binding and the parameter that fills
// it. A shader may declare several, each named separately.
type plannedSampler struct {
	group   int
	binding int
	param   parameterRef
}

// declaredKind is what a reflected binding needs from the parameter that fills
// it. It is coarser than paramKind on purpose: the shader declares a slot, not a
// Go constructor, and several constructors legitimately fill one slot.
type declaredKind uint8

const (
	declaredValue declaredKind = iota
	declaredTexture
	declaredSampler
	declaredBuffer
)

func (d declaredKind) String() string {
	switch d {
	case declaredTexture:
		return "a texture"
	case declaredSampler:
		return "a sampler"
	case declaredBuffer:
		return "a storage buffer"
	}
	return "a uniform member"
}

func (d declaredKind) accepts(kind ParamKind) bool {
	switch d {
	case declaredTexture:
		return kind == ParamTexture
	case declaredSampler:
		return kind == ParamSampler
	case declaredBuffer:
		return kind == ParamBuffer
	}
	return kind.ValueKind()
}

type parameterPlan struct {
	uniformSize int
	uniforms    []plannedUniform
	samplers    []plannedSampler
	resources   []plannedResource
	// mismatch is the first parameter whose kind cannot fill the binding its
	// name matched. It is resolved during construction, which is cached per
	// (shader, parameter shape), so detecting it costs nothing per draw.
	mismatch error
}

type parameterPlanBucketKey struct {
	shader ShaderID
	hash   uint64
}

type cachedParameterPlan struct {
	materialNames []string
	drawNames     []string
	plan          parameterPlan
}

// checkKind records the first parameter whose kind cannot fill the binding its
// name matched. One name has one frequency: a value is a uniform member and a
// buffer is a storage binding, and supplying either where the other is declared
// binds the wrong descriptor into the slot and draws garbage with no diagnostic.
// Matching is by name alone, so nothing else catches it.
//
// A name that matched nothing is not a mismatch: leaving a shader value at its
// zero is ordinary, and gfx drops a parameter no shader declared.
func (plan *parameterPlan) checkKind(label, name string, ref parameterRef, material, draw []ParameterDescr, declared declaredKind) {
	if plan.mismatch != nil {
		return
	}
	param := ref.value(material, draw)
	if param == nil || declared.accepts(ParameterKind(param)) {
		return
	}
	plan.mismatch = ErrParameterKindMismatch{
		Shader: label, Parameter: name, Supplied: ParameterKind(param).String(), Declared: declared.String(),
	}
}

func parameterRefFor(name string, material, draw []ParameterDescr) parameterRef {
	for i := range draw {
		if draw[i].Name() == name {
			return parameterRef{source: parameterDraw, index: i}
		}
	}
	for i := range material {
		if material[i].Name() == name {
			return parameterRef{source: parameterMaterial, index: i}
		}
	}
	return parameterRef{}
}

func parameterNames(params []ParameterDescr) []string {
	names := make([]string, len(params))
	for i := range params {
		names[i] = params[i].Name()
	}
	return names
}

func parameterShapeEqual(cached *cachedParameterPlan, material, draw []ParameterDescr) bool {
	if len(cached.materialNames) != len(material) || len(cached.drawNames) != len(draw) {
		return false
	}
	for i := range material {
		if cached.materialNames[i] != material[i].Name() {
			return false
		}
	}
	for i := range draw {
		if cached.drawNames[i] != draw[i].Name() {
			return false
		}
	}
	return true
}

// parameterShapeHash hashes a draw's parameter shape: its material's names,
// then its own. The material half is split out so a material OpQueue recorded
// for the frame brings it already taken; see ParameterShapeState.
func parameterShapeHash(material, draw []ParameterDescr) uint64 {
	return ContinueParameterShape(ParameterShapeState(material), draw)
}
