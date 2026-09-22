package types

// LightSelection is one pass's chosen lights: the shader's fixed array of
// MaxLights and the score each entry got in. A renderer resets it per pass,
// offers it the lights its own layer and frustum tests passed, and hands it to
// PackFrameLighting.
//
// It is filled by insertion - past the cap, a new light replaces the weakest
// entry if it beats it - with no sort and no allocation. The array's order is
// therefore not a ranking, which the shader does not need: it sums. Past the
// cap, the excess is dropped silently: a seventeenth light is dynamic and
// camera-shaped - it appears when you turn around - so there is no natural
// "once" to report at, a per-frame report is pure noise, and the degradation
// is continuous by construction.
type LightSelection struct {
	lights [MaxLights]Light
	scores [MaxLights]float32
	count  int
}

// Reset empties the selection for a new pass.
func (s *LightSelection) Reset() { s.count = 0 }

// Count reports how many lights the selection holds, at most MaxLights.
func (s *LightSelection) Count() int { return s.count }

// Lights reads the kept lights back, in no particular order. The slice aliases
// the selection and is valid until the next Reset or Offer.
func (s *LightSelection) Lights() []Light { return s.lights[:s.count] }

// Offer inserts one light at the score ContributionAt gave it, replacing the
// weakest kept light once the array is full and only if this one beats it.
func (s *LightSelection) Offer(light *Light, score float32) {
	if s.count < MaxLights {
		s.lights[s.count], s.scores[s.count] = *light, score
		s.count++
		return
	}
	weakest := 0
	for i := 1; i < MaxLights; i++ {
		if s.scores[i] < s.scores[weakest] {
			weakest = i
		}
	}
	if score > s.scores[weakest] {
		s.lights[weakest], s.scores[weakest] = *light, score
	}
}
