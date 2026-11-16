package render

// ShaderDefine is one name a shader is acquired with, gfx's of the same name.
// With no Value it is a flag, read by the source's #if conditionals and never
// reaching WGSL; defining a name no source mentions is harmless. With a Value
// it overrides the #const of that name, and is ignored where no source
// declares one. The Value is WGSL text, never interpreted: "16" and "16u"
// differ. A name given twice keeps its last entry.
type ShaderDefine struct {
	Name, Value string
}
