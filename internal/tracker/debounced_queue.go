package tracker

import (
	"sync"
	"time"

	"github.com/sigstore/model-validation-operator/internal/metrics"
	"golang.org/x/time/rate"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
)

// DebouncedQueue provides a queue with built-in debouncing functionality
// It encapsulates both debouncing logic and workqueue implementation
type DebouncedQueue interface {
	// Add adds an item to the queue with debouncing
	// If the same item is already pending debounce, it resets the timer
	Add(item types.NamespacedName)

	// Get gets the next item for processing (blocks if queue is empty)
	Get() (types.NamespacedName, bool)

	// Done marks an item as done processing
	Done(item types.NamespacedName)

	// AddWithRetry adds an item to the queue with rate limiting for retries
	AddWithRetry(item types.NamespacedName)

	// ForgetRetries forgets retry tracking for an item
	ForgetRetries(item types.NamespacedName)

	// GetRetryCount returns the number of retries for an item
	GetRetryCount(item types.NamespacedName) int

	// Len returns the number of items currently in the queue (not pending debounce)
	Len() int

	// WaitForUpdates waits for all pending debounced updates to complete
	WaitForUpdates()

	// WaitForCompletion waits for all pending debounced updates to complete
	// and for the queue to be fully drained (all items processed)
	WaitForCompletion()

	// ShutDown shuts down the queue and stops all timers
	ShutDown()
}

// DebouncedQueueImpl implements DebouncedQueue using workqueue and timers
type DebouncedQueueImpl struct {
	duration       time.Duration
	queue          workqueue.TypedRateLimitingInterface[types.NamespacedName]
	debounceTimers map[types.NamespacedName]*time.Timer
	debounceWg     sync.WaitGroup
	mu             sync.Mutex
	stopCh         chan struct{}
	// Protects metric updates to ensure consistency with queue state
	metricMu sync.Mutex
}

// DebouncedQueueConfig holds configuration for the debounced queue
type DebouncedQueueConfig struct {
	DebounceDuration time.Duration
	RetryBaseDelay   time.Duration
	RetryMaxDelay    time.Duration
	RateLimitQPS     float64
	RateLimitBurst   int
}

// NewDebouncedQueue creates a new debounced queue with the specified configuration
func NewDebouncedQueue(config DebouncedQueueConfig) DebouncedQueue {
	// Create custom rate limiter with configurable retry parameters
	rateLimiter := workqueue.NewTypedMaxOfRateLimiter(
		workqueue.NewTypedItemExponentialFailureRateLimiter[types.NamespacedName](
			config.RetryBaseDelay, config.RetryMaxDelay),
		&workqueue.TypedBucketRateLimiter[types.NamespacedName]{
			Limiter: rate.NewLimiter(rate.Limit(config.RateLimitQPS), config.RateLimitBurst)},
	)

	workQueue := workqueue.NewTypedRateLimitingQueue(rateLimiter)

	return &DebouncedQueueImpl{
		duration:       config.DebounceDuration,
		queue:          workQueue,
		debounceTimers: make(map[types.NamespacedName]*time.Timer),
		stopCh:         make(chan struct{}),
	}
}

// updateQueueSizeMetric updates the queue size metric
// Uses a mutex to ensure metric consistency with queue state
func (dq *DebouncedQueueImpl) updateQueueSizeMetric() {
	dq.metricMu.Lock()
	defer dq.metricMu.Unlock()

	// Skip metric updates if shutdown has been initiated
	if dq.isShutDown() {
		return
	}

	metrics.SetQueueSize(float64(dq.queue.Len()))
}

// isShutDown checks if the queue has been shut down (non-blocking)
func (dq *DebouncedQueueImpl) isShutDown() bool {
	select {
	case <-dq.stopCh:
		return true
	default:
		return false
	}
}

// Add adds an item to the queue with debouncing
// If the queue has been shut down, this method will return without adding the item
func (dq *DebouncedQueueImpl) Add(item types.NamespacedName) {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	if dq.isShutDown() {
		return
	}
	if timer, ok := dq.debounceTimers[item]; ok {
		timer.Reset(dq.duration)
	} else {
		dq.debounceWg.Add(1)
		dq.debounceTimers[item] = time.AfterFunc(dq.duration, func() {
			defer dq.debounceWg.Done()
			if dq.isShutDown() {
				return
			}
			dq.queue.Add(item)
			dq.updateQueueSizeMetric()
			dq.mu.Lock()
			delete(dq.debounceTimers, item)
			dq.mu.Unlock()
		})
	}
}

// Get gets the next item for processing
func (dq *DebouncedQueueImpl) Get() (types.NamespacedName, bool) {
	return dq.queue.Get()
}

// Done marks an item as done processing
func (dq *DebouncedQueueImpl) Done(item types.NamespacedName) {
	dq.queue.Done(item)
	dq.updateQueueSizeMetric()
}

// AddWithRetry adds an item to the queue with rate limiting for retries
// If the queue has been shut down, this method will return without adding the item
func (dq *DebouncedQueueImpl) AddWithRetry(item types.NamespacedName) {
	if dq.isShutDown() {
		return
	}
	dq.queue.AddRateLimited(item)
	dq.updateQueueSizeMetric()
}

// ForgetRetries forgets retry tracking for an item
func (dq *DebouncedQueueImpl) ForgetRetries(item types.NamespacedName) {
	dq.queue.Forget(item)
}

// GetRetryCount returns the number of retries for an item
func (dq *DebouncedQueueImpl) GetRetryCount(item types.NamespacedName) int {
	return dq.queue.NumRequeues(item)
}

// Len returns the number of items currently in the queue
func (dq *DebouncedQueueImpl) Len() int {
	return dq.queue.Len()
}

// WaitForUpdates waits for all pending debounced updates to complete
func (dq *DebouncedQueueImpl) WaitForUpdates() {
	dq.debounceWg.Wait()
}

// WaitForCompletion waits for all pending debounced updates to complete
// and for the queue to be fully drained (all items processed)
func (dq *DebouncedQueueImpl) WaitForCompletion() {
	dq.WaitForUpdates()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if dq.queue.Len() == 0 {
				return
			}
		case <-dq.stopCh:
			return
		}
	}
}

// ShutDown shuts down the queue and stops all timers
func (dq *DebouncedQueueImpl) ShutDown() {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	for _, timer := range dq.debounceTimers {
		timer.Stop()
	}
	dq.debounceTimers = make(map[types.NamespacedName]*time.Timer)

	if !dq.isShutDown() {
		close(dq.stopCh)
	}

	dq.queue.ShutDown()

	// Update metrics to reflect shutdown
	metrics.SetQueueSize(0)
}
