# Changelog

## Unreleased

- Correct bounded waiting so current holders do not consume waiter capacity.
- Use one FIFO queue for context cancellation, permanent cancellation, bounded waiting, and fairness.
- Add structured observers, watchdog thresholds, exact acquisition guards, live pprof state, and expanded Prometheus metrics.
- Add keyed and exclusive lock forms.
- Add typed acquisition errors, state snapshots, deterministic concurrency tests, examples, benchmarks, launchers, and hosted verification.
- Add actionable single-line event formatting and exact-output package examples.
- Add lock-selection, compatibility, limitations, provenance, benchmark, and social-preview repository assets.
- Prevent stale metric gauge overwrites, cancelled-waiter capacity leaks, keyed pre-cancellation leaks, lost elapsed thresholds,
  out-of-order pprof holder entries, and hold reports preceding acquisition delivery.
- Remove the untagged nonstandard `RTryLock` aliases in favor of `TryRLock`.
