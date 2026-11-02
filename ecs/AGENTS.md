# ECS rules

## Code

- A composition error (an invalid component, too many component types for the world ID, a system parameter that cannot be declared) panics at registration. A runtime failure goes to `LogErr`.
- Components are pointer-free, checked with `container.IsPointerFree` at registration. Variable-size data goes in a ref.
- Nothing on the hot path allocates once the containers have grown: not a query, a commit, an observation reset or a system call.
- `query_gen.go` is written by `internal/generate`: change the generator and run `go generate`, never the output.

## Potential rules
