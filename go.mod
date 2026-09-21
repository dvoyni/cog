module github.com/dvoyni/cog

go 1.27

require (
	github.com/ebitengine/oto/v3 v3.5.0
	github.com/gogpu/gogpu v0.54.0
	github.com/gogpu/gpucontext v0.31.3
	github.com/gogpu/gputypes v0.8.0
	github.com/gogpu/naga v0.19.0
	github.com/gogpu/wgpu v0.34.5
	github.com/google/jsonschema-go v0.4.3
	github.com/jfreymuth/oggvorbis v1.0.5
	github.com/modelcontextprotocol/go-sdk v1.7.0
	github.com/qmuntal/gltf v0.29.0
	golang.org/x/image v0.44.0
	golang.org/x/sys v0.47.0
	golang.org/x/tools v0.47.0
)

require (
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	github.com/jfreymuth/pulse v0.1.3 // indirect
	github.com/jfreymuth/vorbis v1.0.2 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/mod v0.37.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/gogpu/naga => github.com/dvoyni/naga v0.19.1-0.20260910142728-7fd5ed312699

replace github.com/gogpu/gogpu => github.com/dvoyni/gogpu v0.54.1-0.20260910171621-041de1a5716f

replace github.com/jfreymuth/vorbis => github.com/dvoyni/vorbis v1.0.3-0.20260921113606-7c537d5a7801

replace github.com/jfreymuth/oggvorbis => github.com/dvoyni/oggvorbis v1.0.6-0.20260921113954-25bf5f5f79b5
