package tracker

import (
	"time"

	. "github.com/onsi/ginkgo/v2" //nolint:revive
	. "github.com/onsi/gomega"    //nolint:revive

	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("DebouncedQueue", func() {
	var (
		debouncedQueue DebouncedQueue
		testKey1       types.NamespacedName
		testKey2       types.NamespacedName
		duration       time.Duration
	)

	BeforeEach(func() {
		testKey1 = types.NamespacedName{Name: "test1", Namespace: "default"}
		testKey2 = types.NamespacedName{Name: "test2", Namespace: "default"}
		duration = 50 * time.Millisecond
		debouncedQueue = NewDebouncedQueue(DebouncedQueueConfig{
			DebounceDuration: duration,
			RetryBaseDelay:   100 * time.Millisecond,
			RetryMaxDelay:    1000 * time.Millisecond,
			RateLimitQPS:     10,
			RateLimitBurst:   100,
		})
	})

	AfterEach(func() {
		debouncedQueue.ShutDown()
	})

	Context("when debouncing single updates", func() {
		It("should add update to queue after debounce duration", func() {
			// Initially queue should be empty
			Expect(debouncedQueue.Len()).To(Equal(0))

			// Trigger debounce
			debouncedQueue.Add(testKey1)

			// Should still be empty immediately
			Expect(debouncedQueue.Len()).To(Equal(0))

			// Wait for debounce duration plus buffer
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*3, 10*time.Millisecond).Should(Equal(1))

			// Verify the correct key was added
			item, shutdown := debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			Expect(item).To(Equal(testKey1))
			debouncedQueue.Done(item)
		})

		It("should reset timer on subsequent updates", func() {
			// First update
			debouncedQueue.Add(testKey1)

			// Wait half the debounce duration
			time.Sleep(duration / 2)

			// Queue should still be empty
			Expect(debouncedQueue.Len()).To(Equal(0))

			// Second update should reset the timer
			debouncedQueue.Add(testKey1)

			// Wait another half duration (original timer would have fired by now)
			time.Sleep(duration / 2)

			// Queue should still be empty because timer was reset
			Expect(debouncedQueue.Len()).To(Equal(0))

			// Wait for full duration from second update
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*2, 10*time.Millisecond).Should(Equal(1))
		})
	})

	Context("when debouncing multiple keys", func() {
		It("should handle multiple keys independently", func() {
			// Trigger debounce for both keys
			debouncedQueue.Add(testKey1)
			debouncedQueue.Add(testKey2)

			// Both should be processed independently
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*3, 10*time.Millisecond).Should(Equal(2))

			// Verify both keys are in queue
			receivedKeys := make(map[types.NamespacedName]bool)

			item1, shutdown := debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			receivedKeys[item1] = true
			debouncedQueue.Done(item1)

			item2, shutdown := debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			receivedKeys[item2] = true
			debouncedQueue.Done(item2)

			Expect(receivedKeys).To(HaveKey(testKey1))
			Expect(receivedKeys).To(HaveKey(testKey2))
		})

		It("should reset timers independently for different keys", func() {
			// Start timer for key1
			debouncedQueue.Add(testKey1)

			// Wait and start timer for key2
			time.Sleep(duration / 2)
			debouncedQueue.Add(testKey2)

			// Key1 should fire first (started earlier)
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*2, 10*time.Millisecond).Should(BeNumerically(">=", 1))

			// Get first item (should be key1)
			item, shutdown := debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			Expect(item).To(Equal(testKey1))
			debouncedQueue.Done(item)

			// Key2 should fire shortly after
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration, 10*time.Millisecond).Should(Equal(1))

			item, shutdown = debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			Expect(item).To(Equal(testKey2))
			debouncedQueue.Done(item)
		})
	})

	Context("when waiting for updates", func() {
		It("should wait for all pending updates to complete", func() {
			// Trigger multiple updates
			debouncedQueue.Add(testKey1)
			debouncedQueue.Add(testKey2)

			// WaitForUpdates should block until all timers complete
			done := make(chan bool)
			go func() {
				debouncedQueue.WaitForUpdates()
				done <- true
			}()

			// Should not complete immediately
			Consistently(done, duration/2).ShouldNot(Receive())

			// Should complete after debounce duration
			Eventually(done, duration*3).Should(Receive())

			// Queue should have both items
			Expect(debouncedQueue.Len()).To(Equal(2))
		})

		It("should handle WaitForUpdates with no pending updates", func() {
			// WaitForUpdates should return immediately when no updates pending
			done := make(chan bool)
			go func() {
				debouncedQueue.WaitForUpdates()
				done <- true
			}()

			// Should complete immediately
			Eventually(done, 100*time.Millisecond).Should(Receive())
		})

		It("should wait for queue to drain with WaitForCompletion", func() {
			// Trigger multiple updates
			debouncedQueue.Add(testKey1)
			debouncedQueue.Add(testKey2)

			// WaitForCompletion should block until all timers complete and queue is drained
			done := make(chan bool)
			go func() {
				debouncedQueue.WaitForCompletion()
				done <- true
			}()

			// Should not complete immediately
			Consistently(done, duration/2).ShouldNot(Receive())

			// Should still not complete after just debounce duration (queue not drained)
			time.Sleep(duration * 2)
			Consistently(done, 50*time.Millisecond).ShouldNot(Receive())

			// Drain the queue
			for debouncedQueue.Len() > 0 {
				item, shutdown := debouncedQueue.Get()
				if shutdown {
					break
				}
				debouncedQueue.Done(item)
			}

			// Now it should complete
			Eventually(done, 100*time.Millisecond).Should(Receive())
		})

		It("should stop waiting when queue is shut down", func() {
			// Trigger updates but don't process them
			debouncedQueue.Add(testKey1)
			debouncedQueue.Add(testKey2)

			// Wait for items to be in queue
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*3, 10*time.Millisecond).Should(Equal(2))

			// Start WaitForCompletion which should block
			done := make(chan bool)
			go func() {
				debouncedQueue.WaitForCompletion()
				done <- true
			}()

			// Should not complete immediately (queue not drained)
			Consistently(done, 50*time.Millisecond).ShouldNot(Receive())

			// Shut down the queue - this should cause WaitForCompletion to return
			debouncedQueue.ShutDown()

			// Should complete quickly now
			Eventually(done, 100*time.Millisecond).Should(Receive())
		})
	})

	Context("when shutting down", func() {
		It("should stop all pending timers", func() {
			// Trigger updates
			debouncedQueue.Add(testKey1)
			debouncedQueue.Add(testKey2)

			// Shut down before timers fire
			debouncedQueue.ShutDown()

			// Wait longer than debounce duration
			time.Sleep(duration * 2)

			// Queue should remain empty (timers were stopped)
			Expect(debouncedQueue.Len()).To(Equal(0))
		})
	})

	Context("when handling rapid updates", func() {
		It("should coalesce rapid updates into single queue item", func() {
			// Rapid fire updates for same key
			for i := 0; i < 10; i++ {
				debouncedQueue.Add(testKey1)
				time.Sleep(duration / 20) // Much shorter than debounce duration
			}

			// Should result in only one queued item
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*3, 10*time.Millisecond).Should(Equal(1))

			// Verify it's the correct key
			item, shutdown := debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			Expect(item).To(Equal(testKey1))
			debouncedQueue.Done(item)

			// No more items should be queued
			Consistently(func() int {
				return debouncedQueue.Len()
			}, duration).Should(Equal(0))
		})

		It("should handle interleaved updates for different keys", func() {
			// Interleave updates for different keys
			for i := 0; i < 5; i++ {
				debouncedQueue.Add(testKey1)
				debouncedQueue.Add(testKey2)
				time.Sleep(duration / 10)
			}

			// Should result in one item for each key
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*3, 10*time.Millisecond).Should(Equal(2))
		})
	})

	Context("when handling retries", func() {
		It("should support retry functionality", func() {
			// Add item normally first
			debouncedQueue.Add(testKey1)

			// Wait for debounced item to be available
			Eventually(func() int {
				return debouncedQueue.Len()
			}, duration*3, 10*time.Millisecond).Should(Equal(1))

			// Get and process item
			item, shutdown := debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			Expect(item).To(Equal(testKey1))

			// Check retry count (should be 0 initially)
			retries := debouncedQueue.GetRetryCount(testKey1)
			Expect(retries).To(Equal(0))

			// Add back with retry (simulating failure)
			debouncedQueue.AddWithRetry(testKey1)
			debouncedQueue.Done(item)

			// Get item again
			item, shutdown = debouncedQueue.Get()
			Expect(shutdown).To(BeFalse())
			Expect(item).To(Equal(testKey1))

			// Retry count should be incremented
			retries = debouncedQueue.GetRetryCount(testKey1)
			Expect(retries).To(BeNumerically(">", 0))

			// Forget retries
			debouncedQueue.ForgetRetries(testKey1)
			debouncedQueue.Done(item)
		})
	})
})
