package notion

import (
	"context"
	"sync"
)

const recentConcurrency = 4

// recentParallel bounds in-flight work. Every API request still passes through
// the client's shared rate limiter; this overlaps response waiting only.
func recentParallel(ctx context.Context, count int, run func(context.Context, int) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int, count)
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for i := 0; i < min(count, recentConcurrency); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					return
				}
				if err := run(ctx, job); err != nil {
					once.Do(func() { firstErr = err; cancel() })
					return
				}
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
