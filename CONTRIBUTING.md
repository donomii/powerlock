# Contributing

Powerlock changes must preserve the behavior in `SPEC.md`, the matching files under `pseudocode/`, and the guarantees in `COMPATIBILITY.md`.
New tradeoffs belong in `LIMITATIONS.md`.

Run `./test.sh` before submitting a change. Run `./demo.sh` when changing public behavior or examples. Performance changes should include before-and-after results from `go test -run '^$' -bench=. -benchmem ./...`, including the Go version and processor.

Bug reports must include the Powerlock version, Go version, operating system, expected result, observed result, complete error text, and a deterministic reproducer. Concurrency tests must coordinate through channels or observable lock state and must not depend on external services.

Changes should be focused, should not reformat unrelated code, and should document every new exported symbol and configuration default.
