package types

// TextureTransition orders one texture's writes against its reads, or the other
// way round. The backend derives none of these itself: it tracks resources for
// lifetime and submit-time validation only, so a texture written as an
// attachment and then sampled is not ordered against those writes - not within
// one command encoder, and not across a submit boundary either. On Vulkan the
// sample then reads the image mid-write, which shows as a flicker that looks
// random, is not a CPU/GPU race, and is invisible to a capture taken while the
// app is redrawing.
//
// gfx is the layer that can see the hazard, because by translation time the
// frame's passes are sorted and merged and the write-then-read pairs are
// computable. From is the usage the texture is actually in, not a guess: a
// layout transition that names the wrong old layout is undefined behaviour.
type TextureTransition struct {
	Texture  TextureID
	From, To TextureUsage
}
