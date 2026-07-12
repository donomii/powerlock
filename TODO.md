# Powerlock roadmap

Powerlock should become a Go synchronization toolkit that explains which named lock is waiting, which lock is held, where it was acquired, and how long each state lasts.

## Correctness and API

- [x] Consolidate cancellation, bounded waiting, and fairness on one queued lock engine so combined features use identical ownership and ordering rules.
- [x] Make `MaxRWMutex` limit blocked waiters rather than holders plus waiters. Immediate compatible acquisitions must not consume queue capacity, and a waiter reservation must be released as soon as acquisition succeeds.
- [x] Prevent new `CancelRWMutex` readers from bypassing a waiting writer.
- [x] Avoid cancellation helper goroutines for contexts whose `Done` channel is nil, and replace broadcast-heavy context waiting with a queued design if measurements justify it.
- [x] Make lock identity immutable so acquisition and release always use the same diagnostic and metric name.
- [x] Add error-returning blocking acquisition methods that distinguish cancellation, deadline expiry, and queue saturation. Try methods retain their standard boolean result, while observers report ordinary contention. Blocking `Lock` methods remain for `sync.Locker` compatibility.
- [x] Define consistent zero-value behavior and document which types require constructors.
- [x] Remove the nonstandard `RTryLock` aliases before the first release.
- [x] Add `RLocker` where standard `sync.RWMutex` method compatibility is claimed.
- [x] Make acquisition, keyed-acquisition, and invalid-release failures include the operation, stable name, cause, and available capacity or lock state; make configuration failures include expected and received values.

## Diagnostics and monitoring

- [x] Add an observer interface in the core package. An observer receives stable lock name, read/write mode, wait duration, hold duration when measurable, and acquisition outcome.
- [x] Remove the legacy root Prometheus API after README compatibility work so core-only users no longer compile the Prometheus client packages. The compatibility surface now lives in `powerlock/prometheus`.
- [x] Expand Prometheus reporting with bounded labels, read/write waiter and holder gauges, acquisition outcome counters, and wait-duration histograms.
- [x] Cache metric handles for each immutable lock name rather than resolving labels in every lock operation.
- [x] Add `WatchdogRWMutex`. Its configurable wait and hold thresholds report slow acquisitions and long-held locks through the observer; it never releases a lock automatically.
- [x] Add live pprof profiles for current waiters and holders where acquisition tokens permit exact pairing.
- [x] Add an optional trace flight-recorder callback for watchdog events.
- [x] Add a guard-returning API for exact per-reader hold-duration reporting.
- [x] Add a state snapshot containing current readers, writer state, waiter counts, oldest wait duration, and oldest measurable hold duration.
- [x] Add a single-line event formatter containing duration, ownership, queue state, and the first captured external caller.

## Additional lock types

- [x] Add `FairRWMutex` and `FairMutex` as explicit aliases for the FIFO context-aware implementations.
- [x] Add `KeyedMutex[K]` with context cancellation, reference-counted entry cleanup, and a configured maximum key count.
- [x] Add plain exclusive mutex forms for callers that do not need shared readers.
- [x] Investigate debug-only lock-order diagnostics. Automatic goroutine inference is unsafe; a future opt-in package would require explicit logical-operation scopes and report cycle-closing acquisition stacks.
- [x] Consider guarded read/write upgrade and downgrade. Defer both until after v0.1; any future API will be guard-only, with nonblocking sole-reader upgrade and atomic downgrade semantics.

Do not add automatic lock expiry, forced unlock, another plain try-lock implementation, spin locks, distributed locks, or a weighted semaphore. Those either violate protected-state safety or duplicate established Go facilities.

## Verification and performance

- [x] Replace sleep-based coordination with deterministic channels or `testing/synctest` where supported.
- [x] Add saturation, cancellation/unlock race, writer fairness, zero-value, metric-value, and diagnostic-content tests.
- [x] Add benchmarks for Max, Context, Watchdog, observer-disabled operation, try paths, cancellation, queue saturation, mixed readers/writers, and parallel contention.
- [x] Extend `BENCHMARKS.md` beyond the current reproducible `sync.RWMutex` comparison with relevant diagnostic libraries.
- [x] Keep `go test -race ./...`, `go vet ./...`, formatting, examples, and supported-version tests in the local and hosted verification gates.

## Package and repository

- [x] Add a package `doc.go` and documentation for every exported type, constructor, method, constant, and error.
- [x] Add executable package examples and exact-output examples, including a real contention incident and the emitted diagnostic data.
- [x] Add a lock-selection and failure-semantics matrix in `LOCKS.md`.
- [x] Link the selection matrix from the existing README when repository instructions permit editing it.
- [x] Correct public installation, import, and module-path instructions when repository instructions permit editing the existing README.
- [x] Add a limitations document explaining fairness, panic behavior, metric cardinality, copying restrictions, and when the standard library is preferable.
- [x] Add hosted verification for tests, race detection, vetting, formatting, examples, and supported Go versions.
- [x] Establish Go 1.23 as the oldest supported version from dependency requirements and a local Go 1.23.0 test run.
- [x] Add a changelog, compatibility policy, contribution guide, issue forms, and security reporting instructions.
- [x] Audit source provenance and record the missing upstream information in `PROVENANCE.md`.
- [x] Identify the exact imported upstream revisions and make source notices consistent before release.
- [x] Add a diagnostic demonstration.
- [x] Add editable and 1280×640 PNG social-preview assets under `assets/`.
- [ ] Add a dashboard image and upload `assets/social-preview.png` in the repository settings.
- [x] Change the repository description to “Named FIFO Go locks with context cancellation, bounded queues, watchdog diagnostics, pprof, and Prometheus metrics.”
- [ ] Set the topics `go`, `golang`, `mutex`, `concurrency`, `debugging`, `observability`, `prometheus`, and `pprof`, then set the homepage to the pkg.go.dev package after release.
- [ ] Tag a `v0.1.0` release after the correctness and documentation gates pass.
- [ ] Ensure the package is rendered and discoverable on pkg.go.dev.
- [ ] Submit the released project to Awesome Go and announce one polished release with real diagnostic output and measured overhead.

## Publication

- [x] Commit the completed local changes when explicitly authorized.
- [ ] Push the completed release candidate when explicitly authorized.
- [ ] Create the hosted release and update repository settings when explicitly authorized.
