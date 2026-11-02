# Shared queues and pools use a mutex, not lock-free structures

`concurrent.Queue` and `concurrent.Pool` guard their state with a plain mutex. Lock-free versions were measured against them: the mutex ones lose little uncontended and avoid compare-and-swap storms under contention without a tuned backoff. Don't replace them with lock-free versions without new measurements that say otherwise.
