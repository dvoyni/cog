module github.com/dvoyni/cog

go 1.27

require (
	github.com/gogpu/gogpu v0.54.0
	github.com/gogpu/gpucontext v0.31.3
	github.com/gogpu/gputypes v0.8.0
	github.com/gogpu/naga v0.19.0
	github.com/gogpu/wgpu v0.34.5
	github.com/qmuntal/gltf v0.29.0
	golang.org/x/image v0.44.0
)

require (
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/gogpu/naga => github.com/dvoyni/naga v0.19.1-0.20260909205556-fee6c529ac74
