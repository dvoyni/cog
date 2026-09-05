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

func (t *translator) prepareParameterPlan(shader ShaderID, layout ShaderLayout, material, draw []ParameterDescr) *parameterPlan {
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
		entry.plan.uniforms[i] = plannedUniform{
			offset: member.Offset,
			param:  parameterRefFor(member.Name, material, draw),
		}
	}
	entry.plan.resources = make([]plannedResource, 0, len(layout.Resources))
	for i := range layout.Resources {
		resource := &layout.Resources[i]
		if resource.Sampler {
			entry.plan.samplers = append(entry.plan.samplers, plannedSampler{
				group: resource.Group, binding: resource.Binding,
				param: parameterRefFor(resource.Name, material, draw),
			})
			continue
		}
		kind := plannedTexture
		if resource.StorageBuffer {
			kind = plannedBuffer
		}
		entry.plan.resources = append(entry.plan.resources, plannedResource{
			kind: kind, group: resource.Group, binding: resource.Binding,
			param: parameterRefFor(resource.Name, material, draw),
		})
	}

	bucket = append(bucket, entry)
	t.parameterPlans[key] = bucket
	return &bucket[len(bucket)-1].plan
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
