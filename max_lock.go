//                           _       _
// __      _____  __ ___   ___  __ _| |_ ___
// \ \ /\ / / _ \/ _` \ \ / / |/ _` | __/ _ \
//  \ V  V /  __/ (_| |\ V /| | (_| | ||  __/
//   \_/\_/ \___|\__,_| \_/ |_|\__,_|\__\___|
//
//  Copyright © 2016 - 2024 Weaviate B.V. All rights reserved.
//
//  CONTACT: hello@weaviate.io
//

package powerlock

import (
	"errors"
	"sync"
)

var ErrMaxWaiting = errors.New("powerlock: too many waiting locks")

// MaxRWMutex is a read/write mutex with limited waiters
type MaxRWMutex struct {
	rwlock     sync.RWMutex
	location   string
	maxWaiting int
	waiting    chan struct{}
}

func NewMaxRWMutex(location string) *MaxRWMutex {
	const defaultMaxWaiting = 1
	return &MaxRWMutex{
		location:   location,
		maxWaiting: defaultMaxWaiting,
		waiting:    make(chan struct{}, defaultMaxWaiting),
	}
}

func (m *MaxRWMutex) SetLocation(location string) {
	m.location = location
}

func (m *MaxRWMutex) tryEnter() error {
	select {
	case m.waiting <- struct{}{}:
		return nil
	default:
		return ErrMaxWaiting
	}
}

func (m *MaxRWMutex) exit() {
	<-m.waiting
}

func (m *MaxRWMutex) Lock() {
	if err := m.tryEnter(); err != nil {
		panic(err)
	}
	m.rwlock.Lock()
}

func (m *MaxRWMutex) TryLock() bool {
	if err := m.tryEnter(); err != nil {
		return false
	}
	ok := m.rwlock.TryLock()
	if !ok {
		m.exit()
		return false
	}
	return true
}

func (m *MaxRWMutex) Unlock() {
	m.rwlock.Unlock()
	m.exit()
}

func (m *MaxRWMutex) RLock() {
	if err := m.tryEnter(); err != nil {
		panic(err)
	}
	m.rwlock.RLock()
}

func (m *MaxRWMutex) TryRLock() bool {
	if err := m.tryEnter(); err != nil {
		return false
	}
	ok := m.rwlock.TryRLock()
	if !ok {
		m.exit()
		return false
	}
	return true
}

func (m *MaxRWMutex) RTryLock() bool {
	return m.TryRLock()
}

func (m *MaxRWMutex) RUnlock() {
	m.rwlock.RUnlock()
	m.exit()
}
