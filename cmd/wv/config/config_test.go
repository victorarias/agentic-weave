package config

import "testing"

func TestLoadRejectsInvalidNumericEnv(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_MODEL", "m")
	t.Setenv("WV_MAX_TURNS", "abc")
	if _, err := Load(); err == nil {
		t.Fatal("expected parse error for WV_MAX_TURNS")
	}
}

func TestLoadRejectsOutOfRangeTemperature(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_MODEL", "m")
	t.Setenv("ANTHROPIC_TEMPERATURE", "2.5")
	if _, err := Load(); err == nil {
		t.Fatal("expected range error for ANTHROPIC_TEMPERATURE")
	}
}

func TestLoadParsesBooleanFeatureFlags(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_MODEL", "m")
	t.Setenv("WV_ENABLE_EXTENSIONS", "true")
	t.Setenv("WV_ENABLE_PROJECT_EXTENSIONS", "yes")
	t.Setenv("WV_ENABLE_BASH", "1")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.EnableExtensions || !cfg.EnableProjectExtensions || !cfg.EnableBash {
		t.Fatalf("expected feature flags to be enabled, got %#v", cfg)
	}
}

func TestLoadRejectsInvalidBooleanFeatureFlag(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("ANTHROPIC_MODEL", "m")
	t.Setenv("WV_ENABLE_BASH", "treu")
	if _, err := Load(); err == nil {
		t.Fatal("expected parse error for WV_ENABLE_BASH")
	}
}
