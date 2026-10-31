// Package mcp declares the agent-facing Bundle: how a plugin offers typed
// capabilities to an agent, and nothing about how those capabilities reach one.
// It imports kernel and the standard library.
//
// mcp is a Bundle. Its plugin, the broker built by mcpplugin.New, collects every
// Provider through ProviderPort, contributes one McpProvider of its own, and
// serves the capabilities over the Model Context Protocol. The broker and its
// SDK and JSON-schema dependencies live in mcp's internal/, so a plugin that
// imports this root to offer a capability compiles in neither.
//
// A Provider is the Adapter a plugin contributes to offer Capabilities. A
// Capability is a named, described, typed dispatch, built with either Command
// or Func. This package must never learn protocol vocabulary, so a provider
// writes no schema, no tool name and no annotation.
//
// # The capability-body rule
//
// The capability body runs on the broker's goroutine and holds no locks. It may
// dispatch and it may wait. It may not touch provider state.
//
// This is the one rule a provider author must read before writing a Func. The
// broker calls a capability from an HTTP goroutine while the render thread is
// mid-frame, and a value read from a resource handle is valid only while the
// handler holds its lock. So the body must be a dispatch, never an access —
// which is exactly what the kernel.Executioner it receives permits, and nothing
// more. Capability built with Command enforces the rule by construction: there
// is nowhere to put code that would run on the broker's goroutine.
//
// One narrow exception, stated explicitly because an unstated exception is how
// a rule like this erodes:
//
// Engine-immutable data finalized before Run may be read directly from the
// Executioner. Describe is the only such source. Everything else goes through a
// dispatch.
//
// # Specification
//
// The full design, including the reasoning behind every rule here, is in
// bundles/mcp/docs/specs/mcp.md; the broker's half is in
// bundles/mcp/docs/specs/broker.md.
package mcp
