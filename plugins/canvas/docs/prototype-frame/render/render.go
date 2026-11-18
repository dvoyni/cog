// Package render stands in for the few render types canvas's API names.
package render

// Texture names one of render's textures: a pointer-free, comparable handle.
type Texture struct{ id uint32 }

// TextureInfo is what this stub knows of a texture.
type TextureInfo struct {
	Label         string
	Width, Height uint32
}

var textures []TextureInfo

// MakeRenderTexture stands for Inventory.MakeTexture with a render-attachment
// usage: a texture a camera can draw into.
func MakeRenderTexture(label string, width, height uint32) Texture {
	textures = append(textures, TextureInfo{label, width, height})
	return Texture{uint32(len(textures))}
}

// Info answers what the stub knows of texture. PROTOTYPE only; it is the
// call the camera ticket listed as a gap in render.
func Info(texture Texture) (TextureInfo, bool) {
	if texture.id == 0 || int(texture.id) > len(textures) {
		return TextureInfo{}, false
	}
	return textures[texture.id-1], true
}

// BlendFactor, BlendOperation, BlendComponent and BlendState are render's.
type (
	BlendFactor    uint32
	BlendOperation uint32
	BlendComponent struct {
		Operation            BlendOperation
		SrcFactor, DstFactor BlendFactor
	}
	BlendState struct{ Color, Alpha BlendComponent }
)

const (
	BlendFactorZero BlendFactor = iota
	BlendFactorOne
	BlendFactorSrc
	BlendFactorOneMinusSrc
	BlendFactorSrcAlpha
	BlendFactorOneMinusSrcAlpha
	BlendFactorDst
)

const BlendOperationAdd BlendOperation = 0
