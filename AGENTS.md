# cog

Before any work, read and follow:

- submodules/rules/HOW-TO-USE-AND-EXTEND-RULES.md
- submodules/rules/GENERAL-RULES.md
- submodules/rules/GO-RULES.md

Each module has its own `AGENTS.md` for rules that hold only there.

## Plugins

- A plugin's constructor takes no parameters: `NewPlugin()`. What it can be configured with is a plain `Config` struct of values in its package, given as its part of the kernel's config; `Init` asserts a non-nil part to `Config`, and nil means the defaults. No functional options. (kernel/docs/design.md, *Plugins*)
- A plugin is never a resource: no one may reach its lifecycle methods. What it offers others is an interface it implements, installed as `configurator.InitResource[I](p)`; such small interfaces live in the plugin package's `resources.go`. (kernel/docs/design.md, *Plugins*)

## Upstream forks

- A change in a forked upstream repository (gogpu, naga, wgpu and the rest under `submodules/`) leaves its `CHANGELOG.md` alone: the maintainers write it when they merge the PR. What a changelog entry would say, such as a behaviour change, goes in the PR description.
