# Compatibility policy

Powerlock follows semantic versioning after the first tagged release.

Before version 1.0, a minor release may change an API when the change corrects unsafe synchronization behavior or removes an API that has not shipped in a tagged release. Release notes must identify every such change and its replacement.

After version 1.0, existing exported names and documented behavior remain compatible within a major version. Correctness and security fixes may make previously accepted invalid input fail with a more specific error or panic. New lock types, methods, event fields, and metrics may be added in minor releases.

The behavior contract is `SPEC.md`. Changes to public behavior must update that file, the matching pseudocode, tests, and the changelog in the same change.
