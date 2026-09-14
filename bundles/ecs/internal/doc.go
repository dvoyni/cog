// Package internal is the ecs plugin: New, the resolution of ecs.Config, and the
// registration that publishes the id authority, *ecs.Entities, as the resource
// every System holds for read and every structural change holds for write. That
// publication is all it does. Components are registered by the plugins that
// define their Go types, and Systems are ordinary subscriptions, both through
// ecs's root. Composition roots and tests reach New through ecsplugin;
// everything else reaches ecs through its root.
//
// The plugin requires no Adapter and contributes none.
package internal
