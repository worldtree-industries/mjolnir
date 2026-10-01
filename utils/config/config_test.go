package config

import (
	"testing"
)

func TestLoadNoConfigFile(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestApplyDefaults(t *testing.T) {
	c := make(Config)
	applyDefaults(c, defaults)

	if c["server"] == nil {
		t.Error("expected server section")
	} else {
		server := c["server"].(map[string]any)
		if server["port"] != 8080 {
			t.Errorf("expected default port 8080, got %v", server["port"])
		}
	}
	if c["db"] != nil {
		db := c["db"].(map[string]any)
		pool := db["pool"].(map[string]any)
		if pool["min_conns"] != 5 {
			t.Errorf("expected default min_conns 5, got %v", pool["min_conns"])
		}
	}
}

func TestValidateRequired(t *testing.T) {
	c := make(Config)
	c["server"] = map[string]any{}
	c["log"] = map[string]any{}
	c["db"] = map[string]any{}
	c["migrations"] = map[string]any{}

	err := validateRequired(c)
	if err != nil {
		t.Errorf("unexpected validation error: %v", err)
	}

	c2 := make(Config)
	c2["log"] = map[string]any{}
	err = validateRequired(c2)
	if err == nil {
		t.Error("expected validation error for missing server")
	}
}

func TestString(t *testing.T) {
	c := Config{"test": "value"}
	s := c.String()
	if s == "" {
		t.Error("expected non-empty string")
	}
}

func TestGet(t *testing.T) {
	c := Get()
	if c == nil {
		t.Fatal("expected non-nil config from Get()")
	}
}
