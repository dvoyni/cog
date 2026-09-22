// Package internal is the ecs plugin: New, the resolution of ecs.Config, and a
// registration that publishes the id authority, *ecs.Entities, as the resource
// every System holds for read and every structural change holds for write;
// registers ShrinkCmd and the three unexported read Commands that read the
// world by Component name (read.go); and registers the one Component the ECS
// owns, m.Transform. Every other Component is registered by the plugin that
// defines its Go type, and Systems are ordinary subscriptions, both through
// ecs's root. Composition roots and tests reach New through ecsplugin;
// everything else reaches ecs through its root.
//
// The plugin requires no Adapter and contributes none.
package internal
