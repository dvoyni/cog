package gfx

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
}

// plannedSampler is one reflected sampler binding and the parameter that fills
// it. A shader may declare several, each named separately.
type plannedSampler struct {
	group   int
	binding int
	param   parameterRef
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

func (t *translator) prepareParameterPlan(shader ShaderID, label string, layout ShaderLayout, material, draw []ParameterDescr) *parameterPlan {
	key := parameterPlanBucketKey{shader: shader, hash: parameterShapeHash(material, draw)}
	bucket := t.parameterPlans[key]
	for i := range bucket {
		if parameterShapeEqual(&bucket[i], material, draw) {
			return &bucket[i].plan
		}
	}

	entry := cachedParameterPlan{
		materialNames: parameterNames(material),
		drawNames:     parameterNames(draw),
		plan:          parameterPlan{uniformSize: layout.UniformSize},
	}
	entry.plan.uniforms = make([]plannedUniform, len(layout.Uniforms))
	for i := range layout.Uniforms {
		member := &layout.Uniforms[i]
		ref := parameterRefFor(member.Name, material, draw)
		entry.plan.checkKind(label, member.Name, ref, material, draw, declaredValue)
		entry.plan.uniforms[i] = plannedUniform{offset: member.Offset, param: ref}
	}
	entry.plan.resources = make([]plannedResource, 0, len(layout.Resources))
	for i := range layout.Resources {
		resource := &layout.Resources[i]
		ref := parameterRefFor(resource.Name, material, draw)
		if resource.Sampler {
			entry.plan.checkKind(label, resource.Name, ref, material, draw, declaredSampler)
			entry.plan.samplers = append(entry.plan.samplers, plannedSampler{
				group: resource.Group, binding: resource.Binding, param: ref,
			})
			continue
		}
		kind, declared := plannedTexture, declaredTexture
		if resource.StorageBuffer {
			kind, declared = plannedBuffer, declaredBuffer
		}
		entry.plan.checkKind(label, resource.Name, ref, material, draw, declared)
		entry.plan.resources = append(entry.plan.resources, plannedResource{
			kind: kind, group: resource.Group, binding: resource.Binding, param: ref,
		})
	}

	bucket = append(bucket, entry)
	t.parameterPlans[key] = bucket
	return &bucket[len(bucket)-1].plan
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

func (d declaredKind) accepts(kind paramKind) bool {
	switch d {
	case declaredTexture:
		return kind == paramTexture
	case declaredSampler:
		return kind == paramSampler
	case declaredBuffer:
		return kind == paramBuffer
	}
	return kind.valueKind()
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
	if param == nil || declared.accepts(param.kind) {
		return
	}
	plan.mismatch = ErrParameterKindMismatch{
		Shader: label, Parameter: name, Supplied: param.kind.String(), Declared: declared.String(),
	}
}

func parameterRefFor(name string, material, draw []ParameterDescr) parameterRef {
	for i := range draw {
		if draw[i].name == name {
			return parameterRef{source: parameterDraw, index: i}
		}
	}
	for i := range material {
		if material[i].name == name {
			return parameterRef{source: parameterMaterial, index: i}
		}
	}
	return parameterRef{}
}

func parameterNames(params []ParameterDescr) []string {
	names := make([]string, len(params))
	for i := range params {
		names[i] = params[i].name
	}
	return names
}

func parameterShapeEqual(cached *cachedParameterPlan, material, draw []ParameterDescr) bool {
	if len(cached.materialNames) != len(material) || len(cached.drawNames) != len(draw) {
		return false
	}
	for i := range material {
		if cached.materialNames[i] != material[i].name {
			return false
		}
	}
	for i := range draw {
		if cached.drawNames[i] != draw[i].name {
			return false
		}
	}
	return true
}

func parameterShapeHash(material, draw []ParameterDescr) uint64 {
	const prime uint64 = 1099511628211
	hash := uint64(1469598103934665603)
	mix := func(value byte) {
		hash ^= uint64(value)
		hash *= prime
	}
	mixName := func(name string) {
		for i := range name {
			mix(name[i])
		}
		mix(0)
	}
	for i := range material {
		mixName(material[i].name)
	}
	mix(0xff)
	for i := range draw {
		mixName(draw[i].name)
	}
	return hash
}
