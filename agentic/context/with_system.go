package context

import "context"

// CompactWithSystem compacts non-system messages while preserving the system prompt.
// The returned slice always begins with the system prompt when provided.
func CompactWithSystem(ctx context.Context, system Message, messages []Message, mgr Manager) ([]Message, string, error) {
	compacted, summary, err := mgr.CompactIfNeeded(ctx, messages)
	if err != nil {
		return nil, "", err
	}
	if system.Content == "" {
		return compacted, summary, nil
	}
	withSystem := make([]Message, 0, len(compacted)+1)
	withSystem = append(withSystem, system)
	withSystem = append(withSystem, compacted...)
	return withSystem, summary, nil
}
