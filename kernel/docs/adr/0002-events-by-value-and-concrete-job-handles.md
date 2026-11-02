# Events travel by value, and dependencies are a concrete JobHandle

A handler takes the event and returns a changed copy rather than taking `&event`, and a dependency is the concrete struct `JobHandle` from `Job[T].Handle()` rather than an interface `Job[T]` satisfies. Both are what keeps the hot path allocation-free: a pointer handed through a function value escapes (measured: 1 alloc per publish), and boxing a `Job[T]` in an interface allocates once it escapes (measured: 1 per conversion). The cost is a copy per handler and an explicit `.Handle()` call, so events stay small.
