package powerlock

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

func BenchmarkSyncRWMutexWrite(b *testing.B) {
	var m sync.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkCancelRWMutexWrite(b *testing.B) {
	m := NewCancelRWMutex("benchmark")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkSyncRWMutexRead(b *testing.B) {
	var m sync.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.RLock()
		m.RUnlock()
	}
}

func BenchmarkCancelRWMutexRead(b *testing.B) {
	m := NewCancelRWMutex("benchmark")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.RLock()
		m.RUnlock()
	}
}

func BenchmarkContextRWMutexWrite(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkContextRWMutexRead(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.RLock()
		m.RUnlock()
	}
}

func BenchmarkMaxRWMutexWrite(b *testing.B) {
	m := NewMaxRWMutexWithLimit("benchmark", 1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkMaxRWMutexRead(b *testing.B) {
	m := NewMaxRWMutexWithLimit("benchmark", 1024)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.RLock()
		m.RUnlock()
	}
}

func BenchmarkObservedRWMutexWriteDisabled(b *testing.B) {
	m := NewObservedRWMutex("benchmark", nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkObservedRWMutexWrite(b *testing.B) {
	observer := LockObserverFunc(func(LockEvent) {})
	m := NewObservedRWMutex("benchmark", observer)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkWatchdogRWMutexWriteDisabled(b *testing.B) {
	m := NewWatchdogRWMutex("benchmark", nil)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkWatchdogRWMutexWrite(b *testing.B) {
	observer := LockObserverFunc(func(LockEvent) {})
	m := NewWatchdogRWMutex("benchmark", observer)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		m.Lock()
		m.Unlock()
	}
}

func BenchmarkContextRWMutexTryLockSuccess(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !m.TryLock() {
			b.Fatal("expected uncontended try acquisition")
		}
		m.Unlock()
	}
}

func BenchmarkContextRWMutexTryLockBusy(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	m.Lock()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if m.TryLock() {
			b.Fatal("unexpected busy try acquisition")
		}
	}
	b.StopTimer()
	m.Unlock()
}

func BenchmarkContextRWMutexPreCancelled(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.LockContext(ctx); err == nil {
			b.Fatal("expected cancelled acquisition")
		}
	}
}

func BenchmarkMaxRWMutexQueueSaturated(b *testing.B) {
	m := NewMaxRWMutexWithLimit("benchmark", 1)
	m.Lock()
	waiterDone := make(chan struct{})
	go func() {
		m.Lock()
		m.Unlock()
		close(waiterDone)
	}()
	waitForBenchmarkState(b, m.Snapshot, func(state LockState) bool { return state.WaitingWriters == 1 })
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := m.LockContext(context.Background()); err == nil {
			b.Fatal("expected saturated queue rejection")
		}
	}
	b.StopTimer()
	m.Unlock()
	<-waiterDone
}

func BenchmarkSyncRWMutexParallelWrite(b *testing.B) {
	var m sync.RWMutex
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.Lock()
			m.Unlock()
		}
	})
}

func BenchmarkContextRWMutexParallelWrite(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.Lock()
			m.Unlock()
		}
	})
}

func BenchmarkContextRWMutexParallelRead(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			m.RLock()
			m.RUnlock()
		}
	})
}

func BenchmarkContextRWMutexParallelMixed(b *testing.B) {
	m := NewContextRWMutex("benchmark")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		operation := 0
		for pb.Next() {
			operation++
			if operation%8 == 0 {
				m.Lock()
				m.Unlock()
			} else {
				m.RLock()
				m.RUnlock()
			}
		}
	})
}

func waitForBenchmarkState(b *testing.B, snapshot func() LockState, matches func(LockState) bool) {
	b.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		state := snapshot()
		if matches(state) {
			return
		}
		if time.Now().After(deadline) {
			b.Fatalf("lock state did not reach expected condition: %+v", state)
		}
		runtime.Gosched()
	}
}
