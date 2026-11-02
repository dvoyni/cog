# Workers are not locked to OS threads

Only the main thread is tied to an OS thread, for pinned jobs. A kernel's workers are plain goroutines, sequential kernels (one worker) included. A worker locked to its own thread was measured: on mostly idle servers it cost twice the CPU and three to four times the tick latency, because waking it means waking that one OS thread, and under full load it gained nothing.
