package powerlock

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// DefaultMaxKeys is the maximum number of active keyed lock entries used by the zero value and NewKeyedMutex.
const DefaultMaxKeys = 1024

type keyedLockEntry struct {
	core       rwCore
	references int
}

// KeyedMutex serializes work by comparable key and removes entries after their last holder or waiter exits.
type KeyedMutex[K comparable] struct {
	mu      sync.Mutex
	name    string
	used    bool
	maxKeys int
	entries map[K]*keyedLockEntry
}

// KeyGuard owns one keyed acquisition and releases it once.
type KeyGuard[K comparable] struct {
	owner     *KeyedMutex[K]
	entry     *keyedLockEntry
	key       K
	attemptID LockAttemptID
	released  atomic.Bool
}

// NewKeyedMutex returns a keyed lock using DefaultMaxKeys.
func NewKeyedMutex[K comparable](name string) *KeyedMutex[K] {
	return NewKeyedMutexWithLimit[K](name, DefaultMaxKeys)
}

// NewKeyedMutexWithLimit returns a keyed lock with a maximum count of referenced keys.
func NewKeyedMutexWithLimit[K comparable](name string, maxKeys int) *KeyedMutex[K] {
	if maxKeys < 1 {
		panic(invalidConfiguration(fmt.Sprintf("maximum keyed locks must be at least 1, received=%d", maxKeys)))
	}
	return &KeyedMutex[K]{name: name, maxKeys: maxKeys}
}

// SetLocation changes the diagnostic name before the first operation and panics after first use.
func (m *KeyedMutex[K]) SetLocation(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.used {
		panic(invalidConfiguration(fmt.Sprintf("keyed lock name cannot change after first use: current=%q requested=%q", m.name, name)))
	}
	m.name = name
}

// Name returns the immutable diagnostic name.
func (m *KeyedMutex[K]) Name() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.name
}

// MaxKeys returns the configured active-key limit.
func (m *KeyedMutex[K]) MaxKeys() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.effectiveMaxKeysLocked()
}

// ActiveKeys returns the number of keys currently held or awaited.
func (m *KeyedMutex[K]) ActiveKeys() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// Lock acquires the lock for key and panics when the active-key limit has been reached.
func (m *KeyedMutex[K]) Lock(key K) {
	if err := m.LockContext(context.Background(), key); err != nil {
		panic(err)
	}
}

// LockContext acquires the lock for key or returns a context or active-key-capacity error.
func (m *KeyedMutex[K]) LockContext(ctx context.Context, key K) error {
	if err := m.contextError(ctx, key); err != nil {
		return err
	}
	entry, err := m.referenceEntry(key)
	if err != nil {
		return err
	}
	_, err = entry.core.acquire(ctx, LockModeWrite, unlimitedWaiters, false)
	if err != nil {
		m.releaseReference(key, entry)
		return m.wrapEntryError(key, err)
	}
	return nil
}

// TryLock attempts to acquire key without waiting or retaining a failed entry reference.
func (m *KeyedMutex[K]) TryLock(key K) bool {
	entry, err := m.referenceEntry(key)
	if err != nil {
		return false
	}
	_, acquired := entry.core.tryAcquire(LockModeWrite, false)
	if !acquired {
		m.releaseReference(key, entry)
	}
	return acquired
}

// Unlock releases the lock for key.
func (m *KeyedMutex[K]) Unlock(key K) {
	entry := m.findEntry(key)
	if entry == nil {
		panic(fmt.Sprintf("powerlock: unlock for keyed lock %q received unknown key %v", m.Name(), key))
	}
	entry.core.release(LockModeWrite, 0)
	m.releaseReference(key, entry)
}

// LockGuard acquires key and returns an exact release token.
func (m *KeyedMutex[K]) LockGuard(ctx context.Context, key K) (*KeyGuard[K], error) {
	if err := m.contextError(ctx, key); err != nil {
		return nil, err
	}
	entry, err := m.referenceEntry(key)
	if err != nil {
		return nil, err
	}
	attemptID, err := entry.core.acquire(ctx, LockModeWrite, unlimitedWaiters, true)
	if err != nil {
		m.releaseReference(key, entry)
		return nil, m.wrapEntryError(key, err)
	}
	return &KeyGuard[K]{owner: m, entry: entry, key: key, attemptID: attemptID}, nil
}

// Snapshot returns the state for key and reports whether the key is referenced.
func (m *KeyedMutex[K]) Snapshot(key K) (LockState, bool) {
	entry := m.findEntry(key)
	if entry == nil {
		return LockState{Name: m.Name()}, false
	}
	return entry.core.snapshot(), true
}

// Unlock releases the keyed acquisition and panics if it has already been released.
func (g *KeyGuard[K]) Unlock() {
	if g == nil || g.owner == nil || g.entry == nil {
		panic("powerlock: unlock called on an empty keyed lock guard")
	}
	if !g.released.CompareAndSwap(false, true) {
		panic("powerlock: keyed lock guard released more than once")
	}
	g.entry.core.release(LockModeWrite, g.attemptID)
	g.owner.releaseReference(g.key, g.entry)
}

// Released reports whether Unlock has already released the keyed acquisition.
func (g *KeyGuard[K]) Released() bool {
	return g == nil || g.released.Load()
}

// Key returns the key owned by the guard.
func (g *KeyGuard[K]) Key() K {
	if g == nil {
		var zero K
		return zero
	}
	return g.key
}

// AttemptID returns the diagnostic identifier for the keyed acquisition.
func (g *KeyGuard[K]) AttemptID() LockAttemptID {
	if g == nil {
		return 0
	}
	return g.attemptID
}

// Error returns the complete keyed acquisition failure.
func (e *KeyedAcquisitionError[K]) Error() string {
	return fmt.Sprintf("powerlock: acquisition for keyed lock %q key=%v failed: active_keys=%d maximum_keys=%d: %v", e.Name, e.Key, e.ActiveKeys, e.MaxKeys, e.Cause)
}

// Unwrap returns the underlying keyed acquisition failure.
func (e *KeyedAcquisitionError[K]) Unwrap() error {
	return e.Cause
}

func (m *KeyedMutex[K]) referenceEntry(key K) (*keyedLockEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used = true
	if m.entries == nil {
		m.entries = make(map[K]*keyedLockEntry)
	}
	if entry, ok := m.entries[key]; ok {
		entry.references++
		return entry, nil
	}
	maxKeys := m.effectiveMaxKeysLocked()
	if len(m.entries) >= maxKeys {
		return nil, &KeyedAcquisitionError[K]{Name: m.name, Key: key, MaxKeys: maxKeys, ActiveKeys: len(m.entries), Cause: ErrMaxKeys}
	}
	entry := &keyedLockEntry{references: 1}
	entry.core.configure(m.name, nil, 0, 0, false)
	m.entries[key] = entry
	return entry, nil
}

func (m *KeyedMutex[K]) contextError(ctx context.Context, key K) error {
	if ctx == nil {
		panic(invalidConfiguration(fmt.Sprintf("write acquisition for keyed lock %q key=%v requires a non-nil context, received nil", m.Name(), key)))
	}
	cause := ctx.Err()
	if cause == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.used = true
	acquisitionErr := newAcquisitionError(m.name, LockModeWrite, cause, unlimitedWaiters, LockState{Name: m.name})
	return &KeyedAcquisitionError[K]{
		Name:       m.name,
		Key:        key,
		MaxKeys:    m.effectiveMaxKeysLocked(),
		ActiveKeys: len(m.entries),
		Cause:      acquisitionErr,
	}
}

func (m *KeyedMutex[K]) findEntry(key K) *keyedLockEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.entries[key]
}

func (m *KeyedMutex[K]) releaseReference(key K, entry *keyedLockEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.entries[key]
	if !ok || current != entry {
		panic(fmt.Sprintf("powerlock: release for keyed lock %q key=%v could not find its referenced entry", m.name, key))
	}
	entry.references--
	if entry.references < 0 {
		panic(fmt.Sprintf("powerlock: release for keyed lock %q key=%v produced negative references=%d", m.name, key, entry.references))
	}
	if entry.references == 0 {
		delete(m.entries, key)
	}
}

func (m *KeyedMutex[K]) wrapEntryError(key K, cause error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &KeyedAcquisitionError[K]{
		Name:       m.name,
		Key:        key,
		MaxKeys:    m.effectiveMaxKeysLocked(),
		ActiveKeys: len(m.entries),
		Cause:      cause,
	}
}

func (m *KeyedMutex[K]) effectiveMaxKeysLocked() int {
	if m.maxKeys == 0 {
		return DefaultMaxKeys
	}
	return m.maxKeys
}
