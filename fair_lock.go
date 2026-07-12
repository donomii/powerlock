package powerlock

// FairRWMutex is the explicitly named FIFO form of ContextRWMutex.
type FairRWMutex = ContextRWMutex

// NewFairRWMutex returns a FIFO context-aware read/write lock.
func NewFairRWMutex(name string) *FairRWMutex {
	return NewContextRWMutex(name)
}
