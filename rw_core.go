package powerlock

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const unlimitedWaiters = -1

var nextLockAttemptID atomic.Uint64

type readLockable interface {
	RLock()
	RUnlock()
}

type rwReadLocker struct {
	lock readLockable
}

func (l rwReadLocker) Lock() {
	l.lock.RLock()
}

func (l rwReadLocker) Unlock() {
	l.lock.RUnlock()
}

type rwWaiter struct {
	id           LockAttemptID
	mode         LockMode
	ctx          context.Context
	ready        chan struct{}
	queuedAt     time.Time
	callers      []uintptr
	guarded      bool
	contended    bool
	finished     bool
	finishedAt   time.Time
	granted      bool
	err          error
	waitTimer    *time.Timer
	holdReport   *holdReport
	reportMu     sync.Mutex
	waitReported bool
	maxWaiting   int
}

type holdReport struct {
	mu                sync.Mutex
	acquiredDelivered chan struct{}
	reported          bool
}

type readerHold struct {
	acquiredAt time.Time
	callers    []uintptr
	timer      *time.Timer
	report     *holdReport
}

type lockNotification struct {
	observer LockObserver
	event    LockEvent
}

func (n lockNotification) send() {
	if n.observer != nil {
		n.observer.ObserveLock(n.event)
	}
}

func (n lockNotification) sendBeforeHoldReports(report *holdReport) {
	if report == nil {
		n.send()
		return
	}
	defer report.completeAcquisition()
	n.send()
}

func (r *holdReport) completeAcquisition() {
	close(r.acquiredDelivered)
}

type rwCore struct {
	mu             sync.Mutex
	name           string
	used           bool
	version        uint64
	readers        int
	writer         bool
	waiters        []*rwWaiter
	cancelled      bool
	observer       LockObserver
	waitThreshold  time.Duration
	holdThreshold  time.Duration
	captureCallers bool

	writerAttemptID  LockAttemptID
	writerAcquiredAt time.Time
	writerCallers    []uintptr
	writerHoldTimer  *time.Timer
	writerHoldReport *holdReport
	readerCohortAt   time.Time
	guardedReaders   map[LockAttemptID]readerHold
	pendingAcquired  int
	pendingEvents    []lockNotification
}

func (c *rwCore) configure(name string, observer LockObserver, waitThreshold time.Duration, holdThreshold time.Duration, captureCallers bool) {
	if waitThreshold < 0 {
		panic(invalidConfiguration(fmt.Sprintf("wait threshold must be zero or greater, received=%s", waitThreshold)))
	}
	if holdThreshold < 0 {
		panic(invalidConfiguration(fmt.Sprintf("hold threshold must be zero or greater, received=%s", holdThreshold)))
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		panic(invalidConfiguration(fmt.Sprintf("lock configuration cannot change after first use: current_name=%q requested_name=%q", c.name, name)))
	}
	c.name = name
	c.observer = observer
	c.waitThreshold = waitThreshold
	c.holdThreshold = holdThreshold
	c.captureCallers = captureCallers
}

func (c *rwCore) setName(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		panic(invalidConfiguration(fmt.Sprintf("lock name cannot change after first use: current=%q requested=%q", c.name, name)))
	}
	c.name = name
}

func (c *rwCore) nameValue() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.name
}

func (c *rwCore) acquire(ctx context.Context, mode LockMode, maxWaiting int, guarded bool) (LockAttemptID, error) {
	if ctx == nil {
		c.mu.Lock()
		name := c.name
		c.mu.Unlock()
		panic(invalidConfiguration(fmt.Sprintf("%s acquisition for lock %q requires a non-nil context, received nil", mode, name)))
	}

	c.mu.Lock()
	c.used = true
	observed := c.observer != nil
	now := time.Time{}
	attemptID := LockAttemptID(0)
	if observed || guarded {
		attemptID = LockAttemptID(nextLockAttemptID.Add(1))
	}
	callers := c.captureCallersLocked()
	if err := ctx.Err(); err != nil {
		now = time.Now()
		state := c.snapshotLocked(now)
		acquireErr := newAcquisitionError(c.name, mode, err, maxWaiting, state)
		notification := c.notificationLocked(LockEventRejected, mode, acquireErr.Result, attemptID, false, 0, 0, false, callers, state)
		c.mu.Unlock()
		notification.send()
		return 0, acquireErr
	}
	if c.cancelled {
		now = time.Now()
		state := c.snapshotLocked(now)
		acquireErr := newAcquisitionError(c.name, mode, ErrCancelled, maxWaiting, state)
		notification := c.notificationLocked(LockEventRejected, mode, acquireErr.Result, attemptID, false, 0, 0, false, callers, state)
		c.mu.Unlock()
		notification.send()
		return 0, acquireErr
	}
	if len(c.waiters) > 0 {
		now = time.Now()
		c.grantWaitersLocked(now)
	}
	if c.canAcquireImmediatelyLocked(mode) {
		if observed {
			now = time.Now()
		}
		holdReport := c.grantLocked(mode, attemptID, guarded, callers, now)
		if !observed {
			c.mu.Unlock()
			return attemptID, nil
		}
		state := c.snapshotLocked(now)
		notification := c.notificationLocked(LockEventAcquired, mode, LockResultAcquired, attemptID, false, 0, 0, false, callers, state)
		notification.event.ExactHold = mode == LockModeWrite || guarded
		c.mu.Unlock()
		c.sendAcquired(notification, holdReport)
		return attemptID, nil
	}
	if maxWaiting >= 0 && len(c.waiters) >= maxWaiting {
		now = time.Now()
		state := c.snapshotLocked(now)
		acquireErr := newAcquisitionError(c.name, mode, ErrMaxWaiting, maxWaiting, state)
		notification := c.notificationLocked(LockEventRejected, mode, acquireErr.Result, attemptID, true, 0, 0, false, callers, state)
		c.mu.Unlock()
		notification.send()
		return 0, acquireErr
	}

	now = time.Now()
	waiter := &rwWaiter{
		id:         attemptID,
		mode:       mode,
		ctx:        ctx,
		ready:      make(chan struct{}),
		queuedAt:   now,
		callers:    callers,
		guarded:    guarded,
		contended:  true,
		maxWaiting: maxWaiting,
	}
	c.waiters = append(c.waiters, waiter)
	c.version++
	if observed {
		waiter.reportMu.Lock()
	}
	if observed && c.waitThreshold > 0 {
		waiter.waitTimer = time.AfterFunc(c.waitThreshold, func() {
			c.reportWaitExceeded(waiter)
		})
	}
	notification := lockNotification{}
	if observed {
		state := c.snapshotLocked(now)
		notification = c.notificationLocked(LockEventWaitStarted, mode, LockResultBusy, attemptID, true, 0, 0, false, callers, state)
	}
	c.mu.Unlock()
	if observed {
		notification.send()
		waiter.reportMu.Unlock()
	}

	select {
	case <-waiter.ready:
		return c.finishWait(waiter)
	case <-ctx.Done():
		c.cancelWaiter(waiter, ctx.Err())
		return c.finishWait(waiter)
	}
}

func (c *rwCore) tryAcquire(mode LockMode, guarded bool) (LockAttemptID, bool) {
	c.mu.Lock()
	c.used = true
	observed := c.observer != nil
	now := time.Time{}
	attemptID := LockAttemptID(0)
	if observed || guarded {
		attemptID = LockAttemptID(nextLockAttemptID.Add(1))
	}
	callers := c.captureCallersLocked()
	if c.cancelled || !c.canAcquireImmediatelyLocked(mode) {
		result := LockResultBusy
		if c.cancelled {
			result = LockResultCancelled
		}
		notification := lockNotification{}
		if observed {
			now = time.Now()
			state := c.snapshotLocked(now)
			notification = c.notificationLocked(LockEventTryFailed, mode, result, attemptID, false, 0, 0, false, callers, state)
		}
		c.mu.Unlock()
		if observed {
			notification.send()
		}
		return 0, false
	}

	if observed {
		now = time.Now()
	}
	holdReport := c.grantLocked(mode, attemptID, guarded, callers, now)
	if !observed {
		c.mu.Unlock()
		return attemptID, true
	}
	state := c.snapshotLocked(now)
	notification := c.notificationLocked(LockEventAcquired, mode, LockResultAcquired, attemptID, false, 0, 0, false, callers, state)
	notification.event.ExactHold = mode == LockModeWrite || guarded
	c.mu.Unlock()
	c.sendAcquired(notification, holdReport)
	return attemptID, true
}

func (c *rwCore) release(mode LockMode, attemptID LockAttemptID) {
	report := (*holdReport)(nil)
	if c.observer != nil && c.holdThreshold > 0 {
		report = c.holdReportFor(mode, attemptID)
	}
	if report != nil {
		report.mu.Lock()
		defer report.mu.Unlock()
	}
	c.mu.Lock()
	pendingBeforeRelease := c.pendingAcquired
	c.used = true
	observed := c.observer != nil
	now := time.Time{}
	if observed || len(c.waiters) > 0 {
		now = time.Now()
	}
	holdDuration := time.Duration(0)
	holdDurationKnown := false
	releasedAttemptID := attemptID
	callers := []uintptr(nil)
	holdThresholdNotification := lockNotification{}

	switch mode {
	case LockModeWrite:
		if !c.writer {
			message := fmt.Sprintf("powerlock: write unlock for lock %q rejected: expected writer=true, observed writer=false readers=%d", c.name, c.readers)
			c.mu.Unlock()
			panic(message)
		}
		if attemptID != 0 && attemptID != c.writerAttemptID {
			message := fmt.Sprintf("powerlock: write guard release for lock %q rejected: expected attempt=%d, received attempt=%d", c.name, c.writerAttemptID, attemptID)
			c.mu.Unlock()
			panic(message)
		}
		releasedAttemptID = c.writerAttemptID
		if observed {
			holdDuration = now.Sub(c.writerAcquiredAt)
			holdDurationKnown = true
			callers = c.writerCallers
		}
		if report != nil && !report.reported && holdDuration >= c.holdThreshold {
			state := c.snapshotLocked(now)
			holdThresholdNotification = c.notificationLocked(LockEventHoldExceeded, mode, LockResultAcquired, releasedAttemptID, false, 0, holdDuration, true, callers, state)
			holdThresholdNotification.event.ExactHold = true
			report.reported = true
		}
		if c.writerHoldTimer != nil {
			c.writerHoldTimer.Stop()
		}
		c.writer = false
		c.writerAttemptID = 0
		c.writerAcquiredAt = time.Time{}
		c.writerCallers = nil
		c.writerHoldTimer = nil
		c.writerHoldReport = nil
	case LockModeRead:
		if c.readers == 0 {
			message := fmt.Sprintf("powerlock: read unlock for lock %q rejected: expected readers>0, observed readers=0 writer=%t", c.name, c.writer)
			c.mu.Unlock()
			panic(message)
		}
		if attemptID != 0 {
			hold, ok := c.guardedReaders[attemptID]
			if !ok {
				message := fmt.Sprintf("powerlock: read guard release for lock %q rejected: expected active attempt=%d, observed active guarded readers=%d", c.name, attemptID, len(c.guardedReaders))
				c.mu.Unlock()
				panic(message)
			}
			if observed {
				holdDuration = now.Sub(hold.acquiredAt)
				holdDurationKnown = true
				callers = hold.callers
			}
			if report != nil && !report.reported && holdDuration >= c.holdThreshold {
				state := c.snapshotLocked(now)
				holdThresholdNotification = c.notificationLocked(LockEventHoldExceeded, mode, LockResultAcquired, attemptID, false, 0, holdDuration, true, callers, state)
				holdThresholdNotification.event.ExactHold = true
				report.reported = true
			}
			if hold.timer != nil {
				hold.timer.Stop()
			}
			delete(c.guardedReaders, attemptID)
		}
		c.readers--
		if c.readers == 0 {
			c.readerCohortAt = time.Time{}
		}
	default:
		message := fmt.Sprintf("powerlock: unlock for lock %q rejected: expected mode=%d or mode=%d, received mode=%d", c.name, LockModeWrite, LockModeRead, mode)
		c.mu.Unlock()
		panic(message)
	}

	c.version++
	c.grantWaitersLocked(now)
	if !observed {
		c.mu.Unlock()
		return
	}
	state := c.snapshotLocked(now)
	notification := c.notificationLocked(LockEventReleased, mode, LockResultAcquired, releasedAttemptID, false, 0, holdDuration, holdDurationKnown, callers, state)
	notification.event.ExactHold = holdDurationKnown
	queueEvents := pendingBeforeRelease > 0
	if queueEvents {
		if holdThresholdNotification.observer != nil {
			c.pendingEvents = append(c.pendingEvents, holdThresholdNotification)
		}
		c.pendingEvents = append(c.pendingEvents, notification)
	}
	c.mu.Unlock()
	if queueEvents {
		return
	}
	holdThresholdNotification.send()
	notification.send()
}

func (c *rwCore) cancel() {
	now := time.Now()
	c.mu.Lock()
	c.used = true
	if c.cancelled {
		c.mu.Unlock()
		return
	}
	c.cancelled = true
	waiters := c.waiters
	c.waiters = nil
	c.version++
	state := c.snapshotLocked(now)
	for _, waiter := range waiters {
		waiter.finished = true
		waiter.finishedAt = now
		waiter.err = newAcquisitionError(c.name, waiter.mode, ErrCancelled, waiter.maxWaiting, state)
		close(waiter.ready)
	}
	c.mu.Unlock()
}

func (c *rwCore) isCancelled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cancelled
}

func (c *rwCore) snapshot() LockState {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked(now)
}

func (c *rwCore) canAcquireImmediatelyLocked(mode LockMode) bool {
	if len(c.waiters) > 0 || c.writer {
		return false
	}
	if mode == LockModeWrite {
		return c.readers == 0
	}
	return mode == LockModeRead
}

func (c *rwCore) grantLocked(mode LockMode, attemptID LockAttemptID, guarded bool, callers []uintptr, now time.Time) *holdReport {
	c.version++
	if c.observer != nil {
		c.pendingAcquired++
	}
	report := (*holdReport)(nil)
	switch mode {
	case LockModeWrite:
		c.writer = true
		c.writerAttemptID = attemptID
		c.writerAcquiredAt = now
		c.writerCallers = callers
		if c.observer != nil && c.holdThreshold > 0 {
			report = &holdReport{acquiredDelivered: make(chan struct{})}
			c.writerHoldReport = report
			c.writerHoldTimer = time.AfterFunc(c.holdThreshold, func() {
				c.reportHoldExceeded(LockModeWrite, attemptID)
			})
		}
	case LockModeRead:
		if c.readers == 0 {
			c.readerCohortAt = now
		}
		c.readers++
		if guarded {
			if c.guardedReaders == nil {
				c.guardedReaders = make(map[LockAttemptID]readerHold)
			}
			hold := readerHold{acquiredAt: now, callers: callers}
			if c.observer != nil && c.holdThreshold > 0 {
				report = &holdReport{acquiredDelivered: make(chan struct{})}
				hold.report = report
				hold.timer = time.AfterFunc(c.holdThreshold, func() {
					c.reportHoldExceeded(LockModeRead, attemptID)
				})
			}
			c.guardedReaders[attemptID] = hold
		}
	default:
		panic(fmt.Sprintf("powerlock: acquisition for lock %q rejected: expected mode=%d or mode=%d, received mode=%d", c.name, LockModeWrite, LockModeRead, mode))
	}
	return report
}

func (c *rwCore) grantWaitersLocked(now time.Time) {
	c.rejectCancelledWaitersLocked(now)
	if c.writer || len(c.waiters) == 0 {
		return
	}
	if c.readers > 0 && c.waiters[0].mode == LockModeWrite {
		return
	}
	if c.waiters[0].mode == LockModeWrite {
		waiter := c.waiters[0]
		c.waiters[0] = nil
		c.waiters = c.waiters[1:]
		c.grantWaiterLocked(waiter, now)
		return
	}

	for len(c.waiters) > 0 && c.waiters[0].mode == LockModeRead {
		waiter := c.waiters[0]
		c.waiters[0] = nil
		c.waiters = c.waiters[1:]
		c.grantWaiterLocked(waiter, now)
	}
}

func (c *rwCore) rejectCancelledWaitersLocked(now time.Time) {
	cancelled := make([]*rwWaiter, 0)
	remaining := c.waiters[:0]
	for _, waiter := range c.waiters {
		if waiter.ctx.Err() != nil {
			cancelled = append(cancelled, waiter)
		} else {
			remaining = append(remaining, waiter)
		}
	}
	for index := len(remaining); index < len(c.waiters); index++ {
		c.waiters[index] = nil
	}
	c.waiters = remaining
	if len(cancelled) > 0 {
		c.version++
	}
	state := c.snapshotLocked(now)
	for _, waiter := range cancelled {
		waiter.finished = true
		waiter.finishedAt = now
		waiter.err = newAcquisitionError(c.name, waiter.mode, waiter.ctx.Err(), waiter.maxWaiting, state)
		close(waiter.ready)
	}
}

func (c *rwCore) grantWaiterLocked(waiter *rwWaiter, now time.Time) {
	waiter.finished = true
	waiter.finishedAt = now
	waiter.granted = true
	waiter.holdReport = c.grantLocked(waiter.mode, waiter.id, waiter.guarded, waiter.callers, now)
	close(waiter.ready)
}

func (c *rwCore) cancelWaiter(waiter *rwWaiter, cause error) {
	now := time.Now()
	c.mu.Lock()
	if waiter.finished {
		c.mu.Unlock()
		return
	}
	for i, queued := range c.waiters {
		if queued == waiter {
			copy(c.waiters[i:], c.waiters[i+1:])
			last := len(c.waiters) - 1
			c.waiters[last] = nil
			c.waiters = c.waiters[:last]
			c.version++
			break
		}
	}
	waiter.finished = true
	waiter.finishedAt = now
	state := c.snapshotLocked(now)
	waiter.err = newAcquisitionError(c.name, waiter.mode, cause, waiter.maxWaiting, state)
	c.grantWaitersLocked(now)
	close(waiter.ready)
	c.mu.Unlock()
}

func (c *rwCore) finishWait(waiter *rwWaiter) (LockAttemptID, error) {
	observed := c.observerValue() != nil
	if observed {
		waiter.reportMu.Lock()
		defer waiter.reportMu.Unlock()
	}
	if waiter.waitTimer != nil {
		waiter.waitTimer.Stop()
	}
	if !observed {
		if waiter.err != nil {
			return 0, waiter.err
		}
		return waiter.id, nil
	}
	state := c.snapshot()
	waitDuration := waiter.finishedAt.Sub(waiter.queuedAt)
	if waiter.waitTimer != nil && !waiter.waitReported && waitDuration >= c.waitThreshold {
		waiter.waitReported = true
		lockNotification{
			observer: c.observerValue(),
			event: LockEvent{
				Name:         state.Name,
				Mode:         waiter.mode,
				Kind:         LockEventWaitExceeded,
				Result:       LockResultBusy,
				AttemptID:    waiter.id,
				Contended:    true,
				WaitDuration: waitDuration,
				Callers:      waiter.callers,
				State:        state,
			},
		}.send()
	}
	if waiter.err != nil {
		acquireErr, ok := waiter.err.(*AcquisitionError)
		result := acquisitionResult(waiter.err)
		if ok {
			result = acquireErr.Result
		}
		notification := lockNotification{
			observer: c.observerValue(),
			event: LockEvent{
				Name:         state.Name,
				Mode:         waiter.mode,
				Kind:         LockEventRejected,
				Result:       result,
				AttemptID:    waiter.id,
				Contended:    true,
				WaitDuration: waitDuration,
				Callers:      waiter.callers,
				State:        state,
			},
		}
		notification.send()
		return 0, waiter.err
	}

	notification := lockNotification{
		observer: c.observerValue(),
		event: LockEvent{
			Name:         state.Name,
			Mode:         waiter.mode,
			Kind:         LockEventAcquired,
			Result:       LockResultAcquired,
			AttemptID:    waiter.id,
			Contended:    true,
			WaitDuration: waitDuration,
			Callers:      waiter.callers,
			State:        state,
			ExactHold:    waiter.mode == LockModeWrite || waiter.guarded,
		},
	}
	c.sendAcquired(notification, waiter.holdReport)
	return waiter.id, nil
}

func (c *rwCore) sendAcquired(notification lockNotification, report *holdReport) {
	defer c.completeAcquiredDelivery()
	notification.sendBeforeHoldReports(report)
}

func (c *rwCore) completeAcquiredDelivery() {
	c.mu.Lock()
	if c.pendingAcquired == 0 {
		c.mu.Unlock()
		panic("powerlock: acquired event delivery completed without a pending acquisition")
	}
	c.pendingAcquired--
	if c.pendingAcquired > 0 {
		c.mu.Unlock()
		return
	}
	pending := c.pendingEvents
	c.pendingEvents = nil
	c.mu.Unlock()
	for _, notification := range pending {
		notification.send()
	}
}

func (c *rwCore) reportWaitExceeded(waiter *rwWaiter) {
	waiter.reportMu.Lock()
	defer waiter.reportMu.Unlock()
	now := time.Now()
	c.mu.Lock()
	if waiter.finished || waiter.waitReported {
		c.mu.Unlock()
		return
	}
	waiter.waitReported = true
	state := c.snapshotLocked(now)
	notification := c.notificationLocked(LockEventWaitExceeded, waiter.mode, LockResultBusy, waiter.id, true, now.Sub(waiter.queuedAt), 0, false, waiter.callers, state)
	c.mu.Unlock()
	notification.send()
}

func (c *rwCore) reportHoldExceeded(mode LockMode, attemptID LockAttemptID) {
	report := c.holdReportFor(mode, attemptID)
	if report == nil {
		return
	}
	<-report.acquiredDelivered
	report.mu.Lock()
	defer report.mu.Unlock()
	if report.reported {
		return
	}
	now := time.Now()
	c.mu.Lock()
	acquiredAt := time.Time{}
	callers := []uintptr(nil)
	switch mode {
	case LockModeWrite:
		if !c.writer || c.writerAttemptID != attemptID {
			c.mu.Unlock()
			return
		}
		acquiredAt = c.writerAcquiredAt
		callers = c.writerCallers
	case LockModeRead:
		hold, ok := c.guardedReaders[attemptID]
		if !ok {
			c.mu.Unlock()
			return
		}
		acquiredAt = hold.acquiredAt
		callers = hold.callers
	default:
		c.mu.Unlock()
		return
	}
	state := c.snapshotLocked(now)
	notification := c.notificationLocked(LockEventHoldExceeded, mode, LockResultAcquired, attemptID, false, 0, now.Sub(acquiredAt), true, callers, state)
	notification.event.ExactHold = true
	report.reported = true
	c.mu.Unlock()
	notification.send()
}

func (c *rwCore) holdReportFor(mode LockMode, attemptID LockAttemptID) *holdReport {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch mode {
	case LockModeWrite:
		if c.writer && (attemptID == 0 || c.writerAttemptID == attemptID) {
			return c.writerHoldReport
		}
	case LockModeRead:
		if hold, ok := c.guardedReaders[attemptID]; ok {
			return hold.report
		}
	}
	return nil
}

func (c *rwCore) notificationLocked(kind LockEventKind, mode LockMode, result LockResult, attemptID LockAttemptID, contended bool, waitDuration time.Duration, holdDuration time.Duration, holdDurationKnown bool, callers []uintptr, state LockState) lockNotification {
	return lockNotification{
		observer: c.observer,
		event: LockEvent{
			Name:              c.name,
			Mode:              mode,
			Kind:              kind,
			Result:            result,
			AttemptID:         attemptID,
			Contended:         contended,
			WaitDuration:      waitDuration,
			HoldDuration:      holdDuration,
			HoldDurationKnown: holdDurationKnown,
			Callers:           callers,
			State:             state,
		},
	}
}

func (c *rwCore) observerValue() LockObserver {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.observer
}

func (c *rwCore) captureCallersLocked() []uintptr {
	if !c.captureCallers || c.observer == nil {
		return nil
	}
	callers := make([]uintptr, 32)
	count := runtime.Callers(3, callers)
	return callers[:count]
}

func (c *rwCore) snapshotLocked(now time.Time) LockState {
	state := LockState{
		Name:      c.name,
		Version:   c.version,
		Readers:   c.readers,
		Writer:    c.writer,
		Cancelled: c.cancelled,
	}
	for _, waiter := range c.waiters {
		if waiter.mode == LockModeWrite {
			state.WaitingWriters++
		} else {
			state.WaitingReaders++
		}
		waitDuration := now.Sub(waiter.queuedAt)
		if waitDuration > state.OldestWaitDuration {
			state.OldestWaitDuration = waitDuration
		}
	}
	if c.observer != nil && c.writer && !c.writerAcquiredAt.IsZero() {
		state.WriterHoldDuration = now.Sub(c.writerAcquiredAt)
	}
	if c.observer != nil && c.readers > 0 && !c.readerCohortAt.IsZero() {
		state.ReaderCohortHoldDuration = now.Sub(c.readerCohortAt)
	}
	return state
}
