# Components are pointer-free; variable-size data lives in refs

A component contains no pointers at any depth: no strings, slices, maps or interfaces. Rows are fixed-width, so a store is never scanned by the GC, and a row is copied, compared and serialized as bytes, which change detection and replication depend on. Text, lists and blobs live in the world's `Refs[W]` container, held in components as typed, pointer-free handles that carry a version. The earlier ECS allowed strings and lists in components; that made every copy, compare and encoder walk the type, and is not coming back.
