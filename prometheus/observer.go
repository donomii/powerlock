package powerlockprometheus

import (
	"fmt"
	"math"
	"sync"

	"github.com/donomii/powerlock"
	promclient "github.com/prometheus/client_golang/prometheus"
)

type metricKey struct {
	name string
	mode powerlock.LockMode
}

type boundMetrics struct {
	waiting             promclient.Gauge
	held                promclient.Gauge
	waitDuration        promclient.Observer
	holdDuration        promclient.Observer
	contentions         promclient.Counter
	waitExceeded        promclient.Counter
	holdExceeded        promclient.Counter
	acquisitionByResult map[powerlock.LockResult]promclient.Counter
}

// Observer converts structured lock events into bounded Prometheus metrics.
type Observer struct {
	mu sync.Mutex

	waiting      *promclient.GaugeVec
	held         *promclient.GaugeVec
	waitDuration *promclient.HistogramVec
	holdDuration *promclient.HistogramVec
	acquisitions *promclient.CounterVec
	contentions  *promclient.CounterVec
	waitExceeded *promclient.CounterVec
	holdExceeded *promclient.CounterVec
	bound        map[metricKey]boundMetrics
	versions     map[string]uint64
}

// New registers an observer using Prometheus default duration buckets.
func New(registerer promclient.Registerer) (*Observer, error) {
	return NewWithBuckets(registerer, promclient.DefBuckets, promclient.DefBuckets)
}

// NewWithBuckets registers an observer with explicit wait and hold duration buckets, expressed in seconds.
func NewWithBuckets(registerer promclient.Registerer, waitBuckets []float64, holdBuckets []float64) (*Observer, error) {
	if registerer == nil {
		return nil, fmt.Errorf("powerlock Prometheus observer: registerer must not be nil")
	}
	if err := validateBuckets("wait", waitBuckets); err != nil {
		return nil, err
	}
	if err := validateBuckets("hold", holdBuckets); err != nil {
		return nil, err
	}

	o := &Observer{
		waiting: promclient.NewGaugeVec(promclient.GaugeOpts{
			Namespace: "powerlock",
			Name:      "waiting",
			Help:      "Current number of lock acquisitions waiting by stable lock name and mode.",
		}, []string{"name", "mode"}),
		held: promclient.NewGaugeVec(promclient.GaugeOpts{
			Namespace: "powerlock",
			Name:      "held",
			Help:      "Current number of held lock acquisitions by stable lock name and mode.",
		}, []string{"name", "mode"}),
		waitDuration: promclient.NewHistogramVec(promclient.HistogramOpts{
			Namespace: "powerlock",
			Name:      "wait_duration_seconds",
			Help:      "Time spent waiting for completed or rejected lock acquisitions.",
			Buckets:   waitBuckets,
		}, []string{"name", "mode"}),
		holdDuration: promclient.NewHistogramVec(promclient.HistogramOpts{
			Namespace: "powerlock",
			Name:      "hold_duration_seconds",
			Help:      "Time spent holding lock acquisitions whose duration can be paired exactly.",
			Buckets:   holdBuckets,
		}, []string{"name", "mode"}),
		acquisitions: promclient.NewCounterVec(promclient.CounterOpts{
			Namespace: "powerlock",
			Name:      "acquisitions_total",
			Help:      "Lock acquisition attempts by stable result.",
		}, []string{"name", "mode", "result"}),
		contentions: promclient.NewCounterVec(promclient.CounterOpts{
			Namespace: "powerlock",
			Name:      "contentions_total",
			Help:      "Lock acquisitions that entered the waiter queue.",
		}, []string{"name", "mode"}),
		waitExceeded: promclient.NewCounterVec(promclient.CounterOpts{
			Namespace: "powerlock",
			Name:      "wait_threshold_exceeded_total",
			Help:      "Lock acquisitions that exceeded the configured wait threshold.",
		}, []string{"name", "mode"}),
		holdExceeded: promclient.NewCounterVec(promclient.CounterOpts{
			Namespace: "powerlock",
			Name:      "hold_threshold_exceeded_total",
			Help:      "Exact lock acquisitions that exceeded the configured hold threshold.",
		}, []string{"name", "mode"}),
		bound:    make(map[metricKey]boundMetrics),
		versions: make(map[string]uint64),
	}

	registrations := []metricRegistration{
		{name: "powerlock_waiting", collector: o.waiting},
		{name: "powerlock_held", collector: o.held},
		{name: "powerlock_wait_duration_seconds", collector: o.waitDuration},
		{name: "powerlock_hold_duration_seconds", collector: o.holdDuration},
		{name: "powerlock_acquisitions_total", collector: o.acquisitions},
		{name: "powerlock_contentions_total", collector: o.contentions},
		{name: "powerlock_wait_threshold_exceeded_total", collector: o.waitExceeded},
		{name: "powerlock_hold_threshold_exceeded_total", collector: o.holdExceeded},
	}
	registered := make([]promclient.Collector, 0, len(registrations))
	for _, registration := range registrations {
		if err := registerer.Register(registration.collector); err != nil {
			for _, collector := range registered {
				registerer.Unregister(collector)
			}
			return nil, fmt.Errorf("powerlock Prometheus observer: register %s: %w", registration.name, err)
		}
		registered = append(registered, registration.collector)
	}
	return o, nil
}

// MustNew registers an observer and panics when registration fails.
func MustNew(registerer promclient.Registerer) *Observer {
	observer, err := New(registerer)
	if err != nil {
		panic(err)
	}
	return observer
}

// ObserveLock updates metrics from one structured lock event.
func (o *Observer) ObserveLock(event powerlock.LockEvent) {
	validateEvent(event)
	readMetrics := o.metrics(event.Name, powerlock.LockModeRead)
	writeMetrics := o.metrics(event.Name, powerlock.LockModeWrite)
	o.updateState(event.Name, event.State, readMetrics, writeMetrics)

	metrics := readMetrics
	if event.Mode == powerlock.LockModeWrite {
		metrics = writeMetrics
	}
	switch event.Kind {
	case powerlock.LockEventWaitStarted:
		metrics.contentions.Inc()
	case powerlock.LockEventWaitExceeded:
		metrics.waitExceeded.Inc()
	case powerlock.LockEventAcquired:
		metrics.acquisitionByResult[powerlock.LockResultAcquired].Inc()
		metrics.waitDuration.Observe(event.WaitDuration.Seconds())
	case powerlock.LockEventReleased:
		if event.HoldDurationKnown {
			metrics.holdDuration.Observe(event.HoldDuration.Seconds())
		}
	case powerlock.LockEventTryFailed, powerlock.LockEventRejected:
		metrics.acquisitionByResult[event.Result].Inc()
		if event.Contended {
			metrics.waitDuration.Observe(event.WaitDuration.Seconds())
		}
	case powerlock.LockEventHoldExceeded:
		metrics.holdExceeded.Inc()
	}
}

func validateEvent(event powerlock.LockEvent) {
	if event.Mode != powerlock.LockModeRead && event.Mode != powerlock.LockModeWrite {
		panic(fmt.Sprintf("powerlock Prometheus observer: event kind=%d received invalid mode=%d", event.Kind, event.Mode))
	}
	switch event.Kind {
	case powerlock.LockEventWaitStarted, powerlock.LockEventWaitExceeded:
		if event.Result != powerlock.LockResultBusy {
			panic(fmt.Sprintf("powerlock Prometheus observer: event kind=%s expected result=busy, received result=%s", event.Kind, event.Result))
		}
	case powerlock.LockEventAcquired, powerlock.LockEventReleased, powerlock.LockEventHoldExceeded:
		if event.Result != powerlock.LockResultAcquired {
			panic(fmt.Sprintf("powerlock Prometheus observer: event kind=%s expected result=acquired, received result=%s", event.Kind, event.Result))
		}
	case powerlock.LockEventTryFailed, powerlock.LockEventRejected:
		if event.Result != powerlock.LockResultBusy && event.Result != powerlock.LockResultQueueFull && event.Result != powerlock.LockResultCancelled && event.Result != powerlock.LockResultDeadlineExceeded {
			panic(fmt.Sprintf("powerlock Prometheus observer: event kind=%s received invalid failure result=%d", event.Kind, event.Result))
		}
	default:
		panic(fmt.Sprintf("powerlock Prometheus observer: received invalid event kind=%d", event.Kind))
	}
}

func (o *Observer) updateState(name string, state powerlock.LockState, readMetrics boundMetrics, writeMetrics boundMetrics) {
	o.mu.Lock()
	defer o.mu.Unlock()
	current := o.versions[name]
	if state.Version < current {
		return
	}
	o.versions[name] = state.Version
	readMetrics.waiting.Set(float64(state.WaitingReaders))
	readMetrics.held.Set(float64(state.Readers))
	writeMetrics.waiting.Set(float64(state.WaitingWriters))
	if state.Writer {
		writeMetrics.held.Set(1)
	} else {
		writeMetrics.held.Set(0)
	}
}

// NewObservedRWMutex returns a lock that reports every state transition to this observer.
func (o *Observer) NewObservedRWMutex(name string) *powerlock.ObservedRWMutex {
	return powerlock.NewObservedRWMutex(name, o)
}

// NewWatchdogRWMutex returns a lock using Powerlock's default wait and hold thresholds.
func (o *Observer) NewWatchdogRWMutex(name string) *powerlock.WatchdogRWMutex {
	return powerlock.NewWatchdogRWMutex(name, o)
}

type metricRegistration struct {
	name      string
	collector promclient.Collector
}

func (o *Observer) metrics(name string, mode powerlock.LockMode) boundMetrics {
	key := metricKey{name: name, mode: mode}
	o.mu.Lock()
	defer o.mu.Unlock()
	metrics, ok := o.bound[key]
	if ok {
		return metrics
	}
	modeName := mode.String()
	metrics = boundMetrics{
		waiting:      o.waiting.WithLabelValues(name, modeName),
		held:         o.held.WithLabelValues(name, modeName),
		waitDuration: o.waitDuration.WithLabelValues(name, modeName),
		holdDuration: o.holdDuration.WithLabelValues(name, modeName),
		contentions:  o.contentions.WithLabelValues(name, modeName),
		waitExceeded: o.waitExceeded.WithLabelValues(name, modeName),
		holdExceeded: o.holdExceeded.WithLabelValues(name, modeName),
		acquisitionByResult: map[powerlock.LockResult]promclient.Counter{
			powerlock.LockResultAcquired:         o.acquisitions.WithLabelValues(name, modeName, powerlock.LockResultAcquired.String()),
			powerlock.LockResultBusy:             o.acquisitions.WithLabelValues(name, modeName, powerlock.LockResultBusy.String()),
			powerlock.LockResultQueueFull:        o.acquisitions.WithLabelValues(name, modeName, powerlock.LockResultQueueFull.String()),
			powerlock.LockResultCancelled:        o.acquisitions.WithLabelValues(name, modeName, powerlock.LockResultCancelled.String()),
			powerlock.LockResultDeadlineExceeded: o.acquisitions.WithLabelValues(name, modeName, powerlock.LockResultDeadlineExceeded.String()),
		},
	}
	o.bound[key] = metrics
	return metrics
}

func validateBuckets(name string, buckets []float64) error {
	if len(buckets) == 0 {
		return fmt.Errorf("powerlock Prometheus observer: %s duration buckets must not be empty", name)
	}
	previous := float64(0)
	for index, bucket := range buckets {
		if math.IsNaN(bucket) || math.IsInf(bucket, 0) || bucket <= 0 {
			return fmt.Errorf("powerlock Prometheus observer: %s duration bucket at index %d must be finite and greater than zero, received %g", name, index, bucket)
		}
		if index > 0 && bucket <= previous {
			return fmt.Errorf("powerlock Prometheus observer: %s duration buckets must increase strictly, index %d received %g after %g", name, index, bucket, previous)
		}
		previous = bucket
	}
	return nil
}
