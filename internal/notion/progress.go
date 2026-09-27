package notion

import (
	"context"
	"sync"
)

type QueryProgress struct {
	Stage string
	Done  int
	Total int // zero means pagination has not revealed a total yet
}

func progressTasks(ctx context.Context, count int, stage string, report func(QueryProgress), run func(context.Context, int) error) error {
	if count == 0 {
		return ctx.Err()
	}
	if report == nil {
		return recentParallel(ctx, count, run)
	}
	report(QueryProgress{Stage: stage, Total: count})
	var mu sync.Mutex
	done := 0
	return recentParallel(ctx, count, func(ctx context.Context, i int) error {
		if err := run(ctx, i); err != nil {
			return err
		}
		mu.Lock()
		done++
		report(QueryProgress{Stage: stage, Done: done, Total: count})
		mu.Unlock()
		return nil
	})
}
