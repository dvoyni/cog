// Package kernel is Cog's microkernel.
//
// A Supervisor runs one or more Kernels inside one application. Each Kernel
// owns its resources and its job graph; kernels share nothing but the
// Supervisor's worker pool and Logger, and talk to each other only by the
// Supervisor's Send. Kernels do not manage kernels: only the Supervisor spawns,
// destroys and quits, and a plugin that must is given it in its config.
//
// A Plugin registers against a Configurator, declaring resources, tasks and
// event handlers, and is then started and stopped with its kernel. What one
// plugin offers another is a resource: a struct, or an interface another
// plugin fills.
//
// Every piece of code that touches resources is a Task: a func whose resource
// struct declares its locks, registered once. Each scheduled run of a task is
// a Job, which runs asynchronously with those locks held, may depend on other
// jobs, and may take its arg from another job's result.
//
// Jobs are only scheduled during a publication. Publish runs each handler in
// turn on the calling goroutine, passing the event by value from one to the
// next together with an out dependency, and every handler schedules into one
// Executor whose batch is submitted when the last returns.
//
// Once spawned, a kernel allocates nothing.
//
// See docs/design.md.
package kernel
