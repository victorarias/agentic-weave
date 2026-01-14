package skills

import "context"

// Skill is a reusable prompt or behavior snippet.
type Skill struct {
	ID          string
	Name        string
	Description string
	Body        string
	Tags        []string
	Source      string
}

// Source loads skills from a system (filesystem, DB, API).
type Source interface {
	List(ctx context.Context) ([]Skill, error)
}
