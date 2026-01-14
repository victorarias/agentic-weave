package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// HashJSON returns a stable SHA-256 hash of a JSON payload.
// It normalizes the JSON to avoid key-order differences.
func HashJSON(payload json.RawMessage) (string, error) {
	if len(payload) == 0 {
		return "", nil
	}
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return "", err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(normalized)
	return hex.EncodeToString(sum[:]), nil
}
