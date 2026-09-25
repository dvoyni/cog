// Package canvas declares the 2D drawing Bundle: layered sprites, text,
// primitives and custom triangles recorded into a frame-local queue, and sprites
// and text measured through a persistent lookup.
//
// canvas is a Bundle. Its plugin, built by canvasplugin.New, requires no Adapter
// and contributes one McpProvider. This package offers the OpQueue and Lookup
// resources, the recording vocabulary, the material sets and the halo, the
// draw-snapshot command and its views, and the ordering identity FlushOnUpdate,
// and declares none of them: each is an alias of what canvas's internal/
// declares, beside the flush that turns a recording into gfx draws, the
// batchers, the snapshot slot, the built-in shader mount and the mcp Provider.
//
// OpQueue and Lookup are concrete types, aliased from internal, so recording a
// sprite is a direct method call with nothing between the caller and the
// queue.
package canvas
