package pool

import (
	"context"
	"log"
	"math/rand/v2"
	"sync"
	"time"
)

// TaskResult wraps a single task's outcome: either a result or an error.
// Preserves the original task for retry/logging purposes.
type TaskResult[T any, R any] struct {
	Task   T     // original task input (preserved for retry/error reporting)
	Result R     // result on success
	Err    error // non-nil on failure
}

// Cooldown provides a global cooldown mechanism for all workers.
// When any worker triggers a cooldown (e.g. due to 412 risk control),
// all workers will wait until the cooldown period expires before proceeding.
type Cooldown struct {
	mu       sync.Mutex
	deadline time.Time
}

// Trigger sets a cooldown period. If a longer cooldown is already active,
// this call is a no-op.
func (c *Cooldown) Trigger(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	newDeadline := time.Now().Add(d)
	if newDeadline.After(c.deadline) {
		c.deadline = newDeadline
		log.Printf("WARN: [pool] Global cooldown triggered: all workers pausing for %v", d)
	}
}

// Wait blocks until the cooldown period expires or ctx is cancelled.
// Returns immediately if no cooldown is active.
func (c *Cooldown) Wait(ctx context.Context) {
	c.mu.Lock()
	remaining := time.Until(c.deadline)
	c.mu.Unlock()

	if remaining <= 0 {
		return
	}

	select {
	case <-ctx.Done():
	case <-time.After(remaining):
	}
}

// Run starts N workers, each consuming tasks from an internal channel.
// Returns after all tasks are processed or ctx is cancelled.
// Every task produces a TaskResult — the caller inspects Err to separate
// successes from failures. Failed tasks retain the original Task for retry.
//
// If maxConsecutiveFailures > 0, the pool will cancel ctx after that many
// consecutive failures (circuit breaker). A single success resets the counter.
//
// requestInterval adds a delay between consecutive requests within each worker
// to avoid triggering anti-crawl mechanisms.
//
// cooldown is an optional shared Cooldown that workers can check before each task.
// Pass nil to disable global cooldown.
func Run[T any, R any](
	ctx context.Context,
	concurrency int,
	tasks []T,
	worker func(ctx context.Context, task T) (R, error),
	maxConsecutiveFailures int,
	requestInterval time.Duration,
	cooldown ...*Cooldown,
) []TaskResult[T, R] {
	if len(tasks) == 0 {
		return nil
	}

	// Extract optional cooldown.
	var cd *Cooldown
	if len(cooldown) > 0 {
		cd = cooldown[0]
	}

	// Create a cancellable context for circuit breaker support.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	taskCh := make(chan T, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	resultCh := make(chan TaskResult[T, R], len(tasks))

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskCh {
				// Check if context is cancelled before processing.
				if ctx.Err() != nil {
					return
				}

				// Wait for global cooldown if active.
				if cd != nil {
					cd.Wait(ctx)
					if ctx.Err() != nil {
						return
					}
				}

				result, err := worker(ctx, task)
				resultCh <- TaskResult[T, R]{
					Task:   task,
					Result: result,
					Err:    err,
				}

				// Delay between requests with ±30% jitter to avoid fixed-rhythm detection.
				if requestInterval > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(JitteredDuration(requestInterval)):
					}
				}
			}
		}()
	}

	// Close resultCh after all workers finish.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Collect results and check circuit breaker.
	results := make([]TaskResult[T, R], 0, len(tasks))
	consecutiveFailures := 0

	for r := range resultCh {
		results = append(results, r)

		if maxConsecutiveFailures <= 0 {
			continue
		}

		if r.Err != nil {
			consecutiveFailures++
			if consecutiveFailures >= maxConsecutiveFailures {
				log.Printf("FATAL: %d consecutive failures, aborting pool", consecutiveFailures)
				cancel()
			}
		} else {
			consecutiveFailures = 0
		}
	}

	return results
}

// JitteredDuration returns a duration with ±30% random jitter applied.
// This helps avoid fixed-rhythm request patterns that anti-crawl systems detect.
func JitteredDuration(base time.Duration) time.Duration {
	// jitter range: [0.7 * base, 1.3 * base]
	jitterFactor := 0.7 + rand.Float64()*0.6
	return time.Duration(float64(base) * jitterFactor)
}
