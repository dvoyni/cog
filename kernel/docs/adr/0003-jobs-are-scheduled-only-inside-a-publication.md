# Jobs are scheduled only inside a publication

There is no `Kernel.Run` or `Kernel.Schedule`: every job is born in an `Executor`'s batch during a publication, and nothing waits inline. That is what makes `ScheduleAfter` safe: no job of the batch starts before the last handler returns, so a producer cannot finish before its consumer is attached. A task or a plugin's `Start` that wants work done publishes an event. v1's nested synchronous dispatch, and the lock-closure folding it needed, are gone on purpose.
