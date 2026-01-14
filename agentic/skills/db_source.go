package skills

import "context"

// DBSource loads skills via an injected function or adapter.
type DBSource struct {
	ListFunc func(ctx context.Context) ([]Skill, error)
}

func (d DBSource) List(ctx context.Context) ([]Skill, error) {
	if d.ListFunc == nil {
		return nil, nil
	}
	return d.ListFunc(ctx)
}
