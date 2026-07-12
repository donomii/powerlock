# Source provenance

Powerlock commit [`d9163aa6ee1369e7f4de76614f82c59dcda646e6`](https://github.com/donomii/powerlock/commit/d9163aa6ee1369e7f4de76614f82c59dcda646e6), titled `Import code`, imported the context, bounded-wait, and metered locks on 2025-06-19.

The source is the closed Weaviate [Context lock pull request](https://github.com/weaviate/weaviate/pull/7975). Its description identifies the context, maximum-waiter, and metered locks, and the author's [handoff comment](https://github.com/weaviate/weaviate/pull/7975#issuecomment-2988468686) states that the code moved to Powerlock. The final public `context-lock` branch revision is [`434b795d6a885e1a06c9e3c85baa9912cc21e43c`](https://github.com/weaviate/weaviate/commit/434b795d6a885e1a06c9e3c85baa9912cc21e43c). The max and metered locks first appear in upstream commit [`1f643b9cf00aaea2eb0400b08a1468492bbccca8`](https://github.com/weaviate/weaviate/commit/1f643b9cf00aaea2eb0400b08a1468492bbccca8).

## Imported files

| Powerlock import path | Final upstream path | Upstream blob | Import blob | Relationship |
|---|---|---|---|---|
| `ctx_lock.go` | `entities/ctxlock/ctx_lock.go` | `e66d00d9494154b912c5655fb9dfa5c65ba7bf94` | `953c83a445d5d4460d563f57dd80aae64a0126f7` | Monitoring integration removed during import |
| `ctx_lock_test.go` | `entities/ctxlock/ctx_lock_test.go` | `03b54456abf42bcbcf2d74046496cd3873ffa271` | `03b54456abf42bcbcf2d74046496cd3873ffa271` | Byte-for-byte identical |
| `max_lock.go` | `entities/ctxlock/max_lock.go` | `a1464b4269b3367c0c7b232d452d05dd87740f0d` | `6a041cc64bbf7bd56c32a7622a51d0097f20c095` | Monitoring integration removed during import |
| `max_lock_test.go` | `entities/ctxlock/max_lock_test.go` | `45da1fb5a101893565de2388e053acaadfbe48a1` | `45da1fb5a101893565de2388e053acaadfbe48a1` | Byte-for-byte identical |
| `metered_lock.go` | `entities/ctxlock/metered_lock.go` | `46d037498210317f518f5dfa931f14596788c11f` | `855da368c2370ee5fac4370b3e76c5ceaf538508` | Global metrics access replaced by injected Prometheus gauges and registration helpers |
| `metered_lock_test.go` | `entities/ctxlock/metered_lock_test.go` | `995811a915d0e360c803e73e697383f351488124` | `49141506b5b2d0972ef8e55e6cd36dd30c5a2768` | Adapted to the injected metrics API |

Upstream commit [`553902f862c2b8bb2505fff3dee1499c4d7b9eb2`](https://github.com/weaviate/weaviate/commit/553902f862c2b8bb2505fff3dee1499c4d7b9eb2) added the Weaviate source headers imported by Powerlock. The current `max_lock.go`, `max_lock_test.go`, `prometheus/legacy.go`, and `prometheus/legacy_test.go` descendants retain those headers. The original context implementation and tests were replaced when Powerlock moved to the shared FIFO engine.

The upstream revision is distributed under its [BSD-3-Clause license](https://github.com/weaviate/weaviate/blob/434b795d6a885e1a06c9e3c85baa9912cc21e43c/LICENSE). Powerlock preserves the complete upstream notice in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
