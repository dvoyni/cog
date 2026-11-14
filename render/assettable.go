package render

// assetTable is the private resource of every asset render holds, behind the
// handles its users keep. Every job reads it; it holds nothing until render
// holds assets.
type assetTable struct{}
