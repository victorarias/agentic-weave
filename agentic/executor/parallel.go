package executor

import (
	"context"
	"fmt"
	"sync"

	"github.com/victorarias/agentic-weave/agentic"
)

// BatchExecutor can execute tool calls in a batch.
type BatchExecutor interface {
	ExecuteBatch(ctx context.Context, calls []agentic.ToolCall) ([]agentic.ToolResult, error)
}

// ParallelExecutor runs independent tool calls concurrently.
type ParallelExecutor struct {
	inner     agentic.ToolExecutor
	allowlist map[string]struct{}
}

// BatchError reports per-call failures from ExecuteBatch.
type BatchError struct {
	Errors []error
}

func (b BatchError) Error() string {
	count := 0
	for _, err := range b.Errors {
		if err != nil {
			count++
		}
	}
	return fmt.Sprintf("batch execution failed for %d call(s)", count)
}

func NewParallel(inner agentic.ToolExecutor, allowlist []string) *ParallelExecutor {
	allowed := make(map[string]struct{}, len(allowlist))
	for _, name := range allowlist {
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	return &ParallelExecutor{inner: inner, allowlist: allowed}
}

func (p *ParallelExecutor) ExecuteBatch(ctx context.Context, calls []agentic.ToolCall) ([]agentic.ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := make([]agentic.ToolResult, len(calls))
	errors := make([]error, len(calls))

	if len(p.allowlist) > 0 {
		for _, call := range calls {
			if _, ok := p.allowlist[call.Name]; !ok {
				return nil, agentic.ErrToolNotFound
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(len(calls))
	for i := range calls {
		idx := i
		call := calls[i]
		go func() {
			defer wg.Done()
			if err := ctx.Err(); err != nil {
				errors[idx] = err
				return
			}
			result, err := p.inner.Execute(ctx, call)
			results[idx] = result
			errors[idx] = err
		}()
	}
	wg.Wait()

	for _, err := range errors {
		if err != nil {
			return results, BatchError{Errors: errors}
		}
	}
	if err := ctx.Err(); err != nil {
		return results, err
	}
	return results, nil
}
