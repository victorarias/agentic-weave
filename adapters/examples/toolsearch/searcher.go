package toolsearch

import (
	"context"
	"sort"
	"strings"

	"github.com/victorarias/agentic-weave/agentic"
)

// StaticSearcher is a simple keyword-based tool searcher.
type StaticSearcher struct {
	Tools []agentic.ToolDefinition
}

func (s StaticSearcher) SearchTools(ctx context.Context, query string) ([]agentic.ToolDefinition, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, nil
	}
	terms := strings.Fields(query)
	results := make([]scored, 0, len(s.Tools))
	for _, tool := range s.Tools {
		score := scoreTool(tool, terms)
		if score > 0 {
			results = append(results, scored{tool: tool, score: score})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	list := make([]agentic.ToolDefinition, 0, len(results))
	for _, item := range results {
		list = append(list, item.tool)
	}
	return list, nil
}

type scored struct {
	tool  agentic.ToolDefinition
	score int
}

func scoreTool(tool agentic.ToolDefinition, terms []string) int {
	name := strings.ToLower(tool.Name)
	desc := strings.ToLower(tool.Description)
	score := 0
	for _, term := range terms {
		if strings.Contains(name, term) {
			score += 2
		}
		if strings.Contains(desc, term) {
			score += 1
		}
	}
	return score
}
