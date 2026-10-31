// Package internal is the ecs plugin: New, the resolution of ecs.Config, and a
// registration that publishes the id authority, *ecs.Entities, as the resource
// every System holds for read and every structural change holds for write;
// registers ShrinkCmd and the six unexported Commands that read and write the
// world by Component name (read.go); registers the one Component the ECS
// owns, m.Transform; and subscribes the one System it owns, the general drainer
// (drainsystem.go), as ecs.DrainOnUpdate. Every other Component is registered
// by the plugin that defines its Go type, and every other System is an ordinary
// subscription, both through ecs's root. Composition roots and tests reach New
// through ecsplugin; everything else reaches ecs through its root.
//
// The plugin requires no Adapter and contributes one mcp.Provider, as
// ecs.McpProvider (mcpprovider.go), offering those six Commands to an Agent as
// the tools ecs_world, ecs_entity, ecs_query, ecs_spawn, ecs_despawn and
// ecs_update.
package internal
