package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config controls the wv CLI runtime.
type Config struct {
	Model                   string
	APIKey                  string
	SystemPrompt            string
	MaxTurns                int
	MaxTokens               int
	Temperature             *float64
	EnableExtensions        bool
	EnableProjectExtensions bool
	EnableBash              bool
	RunTimeoutSeconds       int
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	maxTurns, err := intEnvStrict("WV_MAX_TURNS", 6)
	if err != nil {
		return Config{}, err
	}
	maxTokens, err := intEnvStrict("ANTHROPIC_MAX_TOKENS", 0)
	if err != nil {
		return Config{}, err
	}
	runTimeoutSeconds, err := intEnvStrict("WV_RUN_TIMEOUT_SECONDS", 180)
	if err != nil {
		return Config{}, err
	}
	enableExtensions, err := boolEnvStrict("WV_ENABLE_EXTENSIONS", false)
	if err != nil {
		return Config{}, err
	}
	enableProjectExtensions, err := boolEnvStrict("WV_ENABLE_PROJECT_EXTENSIONS", false)
	if err != nil {
		return Config{}, err
	}
	enableBash, err := boolEnvStrict("WV_ENABLE_BASH", false)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		APIKey:                  trimmedEnv("ANTHROPIC_API_KEY"),
		Model:                   trimmedEnv("ANTHROPIC_MODEL"),
		SystemPrompt:            trimmedEnv("WV_SYSTEM_PROMPT"),
		MaxTurns:                maxTurns,
		MaxTokens:               maxTokens,
		EnableExtensions:        enableExtensions,
		EnableProjectExtensions: enableProjectExtensions,
		EnableBash:              enableBash,
		RunTimeoutSeconds:       runTimeoutSeconds,
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are wv, a pragmatic coding assistant in a terminal. Be concise and accurate."
	}
	if cfg.APIKey == "" {
		return Config{}, errors.New("config: ANTHROPIC_API_KEY is required")
	}
	if cfg.Model == "" {
		return Config{}, errors.New("config: ANTHROPIC_MODEL is required")
	}
	if temp := trimmedEnv("ANTHROPIC_TEMPERATURE"); temp != "" {
		parsed, err := strconv.ParseFloat(temp, 64)
		if err != nil {
			return Config{}, fmt.Errorf("config: invalid ANTHROPIC_TEMPERATURE: %w", err)
		}
		if parsed < 0 || parsed > 1 {
			return Config{}, errors.New("config: ANTHROPIC_TEMPERATURE must be between 0 and 1")
		}
		cfg.Temperature = &parsed
	}
	if cfg.MaxTurns <= 0 {
		return Config{}, errors.New("config: WV_MAX_TURNS must be greater than 0")
	}
	if cfg.MaxTokens < 0 {
		return Config{}, errors.New("config: ANTHROPIC_MAX_TOKENS must be zero or greater")
	}
	if cfg.RunTimeoutSeconds <= 0 {
		return Config{}, errors.New("config: WV_RUN_TIMEOUT_SECONDS must be greater than 0")
	}
	return cfg, nil
}

func trimmedEnv(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func intEnvStrict(key string, fallback int) (int, error) {
	value := trimmedEnv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s: %w", key, err)
	}
	return parsed, nil
}

func boolEnvStrict(key string, fallback bool) (bool, error) {
	value := strings.ToLower(trimmedEnv(key))
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("config: invalid %s: expected true/false", key)
	}
}
