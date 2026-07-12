package diagnostics_test

import (
	"sync"
	"testing"
	"time"

	"github.com/christophcemper/rwmutexplus"
	"github.com/donomii/powerlock"
	linkdeadlock "github.com/linkdata/deadlock"
	sashadeadlock "github.com/sasha-s/go-deadlock"
)

func BenchmarkSyncRWMutexWrite(b *testing.B) {
	var lock sync.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.Lock()
		lock.Unlock()
	}
}

func BenchmarkPowerlockWatchdogRWMutexWrite(b *testing.B) {
	lock := powerlock.NewWatchdogRWMutex("external-benchmark", powerlock.LockObserverFunc(func(powerlock.LockEvent) {}))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.Lock()
		lock.Unlock()
	}
}

func BenchmarkSashaDeadlockRWMutexWrite(b *testing.B) {
	requireSashaDiagnostics(b)
	var lock sashadeadlock.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.Lock()
		lock.Unlock()
	}
}

func BenchmarkLinkdataDeadlockRWMutexWrite(b *testing.B) {
	requireLinkdataDiagnostics(b)
	var lock linkdeadlock.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.Lock()
		lock.Unlock()
	}
}

func BenchmarkRWMutexPlusWrite(b *testing.B) {
	lock := rwmutexplus.NewInstrumentedRWMutex(time.Second)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.Lock()
		lock.Unlock()
	}
}

func BenchmarkSyncRWMutexRead(b *testing.B) {
	var lock sync.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.RLock()
		lock.RUnlock()
	}
}

func BenchmarkPowerlockWatchdogRWMutexRead(b *testing.B) {
	lock := powerlock.NewWatchdogRWMutex("external-benchmark", powerlock.LockObserverFunc(func(powerlock.LockEvent) {}))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.RLock()
		lock.RUnlock()
	}
}

func BenchmarkSashaDeadlockRWMutexRead(b *testing.B) {
	requireSashaDiagnostics(b)
	var lock sashadeadlock.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.RLock()
		lock.RUnlock()
	}
}

func BenchmarkLinkdataDeadlockRWMutexRead(b *testing.B) {
	requireLinkdataDiagnostics(b)
	var lock linkdeadlock.RWMutex
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.RLock()
		lock.RUnlock()
	}
}

func BenchmarkRWMutexPlusRead(b *testing.B) {
	lock := rwmutexplus.NewInstrumentedRWMutex(time.Second)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		lock.RLock()
		lock.RUnlock()
	}
}

func requireSashaDiagnostics(b *testing.B) {
	b.Helper()
	if sashadeadlock.Opts.Disable {
		b.Fatal("sasha-s/go-deadlock diagnostics are disabled; remove the deadlock_disable build tag")
	}
}

func requireLinkdataDiagnostics(b *testing.B) {
	b.Helper()
	if !linkdeadlock.Enabled {
		b.Fatal("linkdata/deadlock diagnostics are disabled; run benchmarks/diagnostics/run.sh")
	}
}
