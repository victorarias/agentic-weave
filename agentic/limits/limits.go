package limits

// ModelLimits describes model context constraints.
type ModelLimits struct {
	ContextWindow int // total context tokens
	MaxOutput     int // max completion tokens
}

// Provider exposes model limits.
type Provider interface {
	Limits() ModelLimits
}
