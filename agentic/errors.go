package agentic

import "errors"

var (
	ErrToolNotFound     = errors.New("tool not found")
	ErrSchemaMismatch   = errors.New("tool schema mismatch")
	ErrCallerNotAllowed = errors.New("tool caller not allowed")
)
