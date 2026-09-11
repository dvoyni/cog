package scene

// The skin's eight bytes: four of joint indices and four of weights, and the
// cap on a skin that one byte per joint index implies.
//
// This is the largest single narrowing on the vertex - sixteen bytes off a
// skinned one - and it is the only one that makes the picture more correct
// rather than merely smaller. An eight-bit weight cannot hold a set that sums
// to exactly one: naive rounding puts 11.7% of Fox's vertices off by exactly
// one code, and Fox's file weights are normalised to within a float32 ULP, so
// every bit of that drift would be scene's own. The answer is the divide the
// skin path was already one register away from making - it accumulated the
// total for the zero-influence check and then trusted the raw weights - and
// that divide also fixes a malformed file, which glTF permits because it only
// says a producer SHOULD normalise.
//
// So the contract is: the bake stores whatever codes it stores, and
// builtin/scene/deform.wgsl divides the deformed position by the total it
// accumulated. Neither half is allowed to assume the other made the sum one.
// scene/vertexskin_test.go holds the transcription that measures it.

const (
	// weightCodeMax is the largest code of one influence's 8-bit unorm, and
	// the divisor the fetch unit has already applied by the time the skin path
	// sees the weight. The encode scales by exactly it.
	weightCodeMax = 0xFF

	// sceneMaxSkinJoints is how many joints a model's skins may claim between
	// them, because the storage vertex names one in a single byte.
	//
	// It is a design cap rather than a measured one, and that is stated rather
	// than dressed up: Fox is the only mesh carrying JOINTS_0 in all fifteen
	// vendored assets, with a maximum index of 23 in a 24-joint skin, which is
	// no evidence either way. 256 sits comfortably above a detailed rigged
	// humanoid at 100-200 including a face rig, and the cap is per model
	// rather than per scene - a level full of characters does not share one
	// budget, because every model has its own joint numbering.
	//
	// Exceeding it fails the whole model at load, loudly, naming it. That is
	// the whole point of the cap: a joint index that did not fit would
	// truncate to a different bone, which is a prop attached to the wrong
	// thing with nothing reported anywhere.
	//
	// It binds the joints the *skins* claim, which are the only ones a vertex
	// ever names. A plain-bound node's joint rides the instance record in a
	// full u32 and is claimed after every skin, so however many of those a
	// file has, they cannot push a vertex's index over.
	sceneMaxSkinJoints = 256
)

// packJoint narrows one model joint index to the byte the storage vertex holds.
//
// It saturates rather than truncating, and neither is meant to happen: the load
// path rejects a model whose skins claim more than sceneMaxSkinJoints before a
// vertex is packed, and the authoring path's Joints are dead because a
// buffer-built mesh never skins. Saturating is what keeps an out-of-range index
// from quietly naming a plausible bone if either of those ever stops holding.
func packJoint(joint uint16) byte {
	if joint > weightCodeMax {
		return weightCodeMax
	}
	return byte(joint)
}

// packWeight quantises one influence's weight to its 8-bit unorm code.
//
// Nothing here tries to make the four codes of a vertex sum to 255. That was
// the alternative - largest-remainder rounding at bake - and it was declined
// for a reason beyond its cost: it could not cover a mesh authored through the
// public API, where scene does not do the rounding, and it would leave a
// malformed file skinned silently wrong. The shader's divide covers both.
//
// The clamps are the ends of the range only. The glTF path normalises a
// vertex's weights at load, so its values are in [0, 1] by construction; an
// authored one is whatever the app wrote, and a weight outside the range has no
// code to land on.
func packWeight(weight float32) byte {
	scaled := float64(weight) * weightCodeMax
	if !(scaled > 0) {
		return 0
	}
	if scaled >= weightCodeMax {
		return weightCodeMax
	}
	return byte(scaled + 0.5)
}
