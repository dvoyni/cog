---
status: superseded in part by ADR-0007
---

# Plugin order is the dependency order

`Plugin` has no dependency list. The order plugins are passed to `Spawn` is the order they `Init` and `Start` in, and `Stop` and `Destroy` run it in reverse; a plugin that needs another's resource is listed after it. The caller composing a kernel already knows that order, and a declared graph would only restate it while adding a sort and its errors.
