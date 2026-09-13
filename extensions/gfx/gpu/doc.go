// Package gpu is the contract a gfx Adapter implements: the Backend interface,
// the Queue gfx replays into it with its pass, bake and release sinks, the
// descriptors a backend creates GPU objects from, and every ID, format and enum
// that gfx's recording API and a backend both speak.
//
// It is gfx's vocabulary package. gfx records draws through its own root -
// OpQueue, ResourceQueue, the descriptors, PresentCmd - and translates them into
// a Queue; a backend such as wgpu reads nothing but this package to execute
// one. Recorders name the IDs, formats and states they pass to gfx from here
// too, so each type has exactly one name.
//
// It imports nothing in cog but libs/m, so a backend never builds the kernel or
// any recorder, and nothing in it can reach back into the recording half.
package gpu
