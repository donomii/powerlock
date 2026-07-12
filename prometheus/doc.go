// Package powerlockprometheus converts Powerlock's structured lock events into bounded Prometheus metrics.
//
// Create one Observer for a registry, then use NewObservedRWMutex for state and duration metrics or
// NewWatchdogRWMutex for the same metrics plus wait and hold threshold counters. Lock names become labels and
// must therefore come from a stable, bounded set. Give simultaneously observed locks unique names so delayed
// state events can be rejected by version without combining unrelated locks.
package powerlockprometheus
