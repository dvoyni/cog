package canvas_test

import (
	"github.com/dvoyni/cog/libs/m"
	"github.com/dvoyni/cog/plugins/canvas"
)

// gateRecording is what one recording task of the gate's frame draws in
// each frame, made before the kernel starts, but for its two materials, which it
// makes in its first frame, setting the second on the sprites of every
// other switch period.
type gateRecording struct {
	cameras      []canvas.Camera
	sprites      []canvas.Sprite
	switchPeriod int
	spritesOnly  bool
	sprite       canvas.Sprite
	meshes       []canvas.Mesh
	shapes       []canvas.Shape
	texts        []canvas.Text
	triangles    canvas.Triangles[canvas.Vertex]
	second       canvas.Material
	dissolve     canvas.Material
}

// makeGateRecordings answers scenario's recordings, one a task, the first
// setting its cameras.
func makeGateRecordings(scenario gateScenario) []*gateRecording {
	quadrants := canvas.ImageFromFile(quadrantsPath)
	solid := canvas.ImageFromFile(gateSolidPath)
	panel := canvas.ImageFromFile(panelPath, canvas.ImageOptions{Slice: panelInsets})
	tiled := canvas.ImageFromFile(gateSolidPath, canvas.ImageOptions{Wrap: canvas.WrapBoth})
	tree := canvas.MeshFileFromFile(gateTreePath)
	layerCount := max(scenario.layerCount, 1)
	clip := m.Rect{X: -100, Y: -100, Width: 200, Height: 150}
	triangles := makeSquareTriangles(12)
	var recordings []*gateRecording
	for task := range scenario.taskCount {
		recording := &gateRecording{switchPeriod: scenario.switchPeriod, spritesOnly: scenario.spritesOnly}
		if task == 0 {
			recording.cameras = makeGateCameras(scenario.cameraCount)
		}
		for index := range scenario.spriteCount / scenario.taskCount {
			image := quadrants
			if index%2 == 1 {
				image = solid
			}
			x := float32(index%100 - 50)
			if !scenario.spritesOnly && index%4 >= 2 {
				x += 10000
			}
			recording.sprites = append(recording.sprites, canvas.Sprite{
				Image:     image,
				Transform: m.Translation2x3(x, float32(index%60-30)),
				Size:      m.Vec2{X: 8, Y: 8},
				Layer:     int32(index % layerCount),
				Order:     canvas.OrderOf(float32(index % 60)),
			})
		}
		if scenario.spritesOnly {
			recordings = append(recordings, recording)
			continue
		}
		recording.sprites = append(recording.sprites, canvas.Sprite{Image: panel, DrawMode: canvas.Sliced, Size: m.Vec2{X: 40, Y: 30}, Transform: m.Translation2x3(-60, 40)})
		recording.sprite = canvas.Sprite{Image: tiled, DrawMode: canvas.Tiled, Size: m.Vec2{X: 40, Y: 30}, Transform: m.Translation2x3(60, 40), Crop: canvas.Crop{Right: 0.25}}
		for index := range 8 {
			recording.meshes = append(recording.meshes, canvas.Mesh{File: tree, Transform: m.Translation2x3(float32(-40+10*index), -50)})
		}
		recording.shapes = []canvas.Shape{
			{Kind: canvas.Fill, Rect: m.Rect{X: -80, Y: -70, Width: 20, Height: 10}, Tint: m.White},
			{Kind: canvas.Fill, Rect: m.Rect{X: -50, Y: -70, Width: 20, Height: 10}, Radius: 4, Tint: m.White, Clip: clip},
			{Kind: canvas.Stroke, Rect: m.Rect{X: -20, Y: -70, Width: 20, Height: 10}, Width: 2, Tint: m.White},
			{Kind: canvas.Stroke, Rect: m.Rect{X: 10, Y: -70, Width: 20, Height: 10}, Tint: m.White},
			{Kind: canvas.Line, From: m.Vec2{X: 40, Y: -70}, To: m.Vec2{X: 60, Y: -60}, Width: 1.5, Tint: m.White},
			{Kind: canvas.Line, From: m.Vec2{X: 70, Y: -70}, To: m.Vec2{X: 90, Y: -60}, Tint: m.White},
		}
		recording.texts = []canvas.Text{
			{String: "Score 1200", Transform: m.Translation2x3(-80, 70)},
			{String: `Press \c:#f80{fire}`, Markup: true, Size: 20, Transform: m.Translation2x3(20, 70)},
		}
		recording.triangles = triangles
		recordings = append(recordings, recording)
	}
	return recordings
}

// record draws the recording's frame into list: its cameras, then its
// sprites in bulk, and unless spritesOnly, a sprite and a fill of the
// dissolve material given its argument, a tiled sprite alone, its meshes in
// bulk and one alone, its shapes, its texts and its triangles. In its first
// frame it makes its materials, and sets the second on the sprites of
// every other switch period.
func (recording *gateRecording) record(list canvas.DrawList, assets canvas.Assets, _ int) {
	if recording.second == (canvas.Material{}) {
		recording.makeMaterials(assets)
	}
	for _, camera := range recording.cameras {
		list.SetCamera(camera)
	}
	list.DrawSprites(recording.sprites)
	if recording.spritesOnly {
		return
	}
	argument := dissolve{Amount: 0.25, Colour: [4]float32{1, 1, 1, 1}}
	list.DrawSpriteWith(canvas.Sprite{Image: recording.sprites[0].Image, Size: m.Vec2{X: 8, Y: 8}, Transform: m.Translation2x3(-30, -10), Material: recording.dissolve}, argument)
	list.DrawShapeWith(canvas.Shape{Rect: m.Rect{X: 30, Y: -10, Width: 8, Height: 8}, Tint: m.White, Material: recording.dissolve}, argument)
	list.DrawSprite(recording.sprite)
	list.DrawMeshes(recording.meshes[1:])
	list.DrawMesh(recording.meshes[0])
	for _, shape := range recording.shapes {
		list.DrawShape(shape)
	}
	for _, text := range recording.texts {
		list.DrawText(text)
	}
	list.DrawTriangles(canvas.DrawTrianglesOptions{Transform: m.Translation2x3(-70, -20)}, recording.triangles)
}

// makeMaterials makes the recording's second and dissolve materials, and
// sets the second on the sprites of every other switch period.
func (recording *gateRecording) makeMaterials(assets canvas.Assets) {
	recording.second = assets.MakeMaterial(canvas.MaterialDescriptor{Label: "second", Shader: gateSecondShaderPath})
	recording.dissolve = assets.MakeMaterial(canvas.MaterialDescriptor{Label: "dissolve", Shader: dissolveShaderPath, Argument: canvas.ArgumentOf[dissolve]()})
	if recording.switchPeriod == 0 {
		return
	}
	for index := range recording.sprites {
		if index/recording.switchPeriod%2 == 1 {
			recording.sprites[index].Material = recording.second
		}
	}
}

// makeGateCameras answers count cameras: one over the whole window, or the
// window split into count-1 strips of order 0, each clearing, under a UI
// camera of order 1 over the whole window, drawing layer 0 at twice the
// zoom.
func makeGateCameras(count int) []canvas.Camera {
	if count == 1 {
		return []canvas.Camera{{Clear: true, ClearColor: m.Color{A: 1}}}
	}
	var cameras []canvas.Camera
	stripCount := count - 1
	width := 1 / float32(stripCount)
	for index := range stripCount {
		cameras = append(cameras, canvas.Camera{Viewport: m.Rect{X: float32(index) * width, Width: width, Height: 1}, Clear: true, ClearColor: m.Color{A: 1}, Position: m.Vec2{X: float32(index) * 20}})
	}
	return append(cameras, canvas.Camera{Order: 1, Layers: canvas.MakeLayer(0), Zoom: 2})
}

// makeGateRecords answers each of scenario's recordings' record, for a
// concurrentUser to call in a recording task of its own.
func makeGateRecords(scenario gateScenario) []recordFunc {
	var records []recordFunc
	for _, recording := range makeGateRecordings(scenario) {
		records = append(records, recording.record)
	}
	return records
}
