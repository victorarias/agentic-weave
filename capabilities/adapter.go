package capabilities

// Adapter describes an LLM provider adapter.
type Adapter interface {
	Capabilities() Capabilities
}
