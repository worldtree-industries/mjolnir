package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeConfig writes contents to a config.json inside a fresh temporary
// directory and returns its path.
func writeConfig(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func assertConfig(t *testing.T, got, want Config) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	defaults := Config{
		"server":   map[string]any{"host": "localhost", "port": 8080},
		"features": []any{"a", "b"},
	}
	path := filepath.Join(t.TempDir(), "config.json")

	got, err := Load(path, defaults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertConfig(t, got, defaults)
}

func TestLoadPartialNestedOverride(t *testing.T) {
	t.Parallel()

	defaults := Config{
		"server": map[string]any{"host": "localhost", "port": 8080},
		"db": map[string]any{
			"pool": map[string]any{"min_conns": 5, "max_conns": 25},
		},
	}
	path := writeConfig(t, `{
		"server": {"port": 9090},
		"db": {"pool": {"min_conns": 10}}
	}`)

	got, err := Load(path, defaults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := Config{
		"server": map[string]any{"host": "localhost", "port": float64(9090)},
		"db": map[string]any{
			"pool": map[string]any{"min_conns": float64(10), "max_conns": 25},
		},
	}
	assertConfig(t, got, want)
}

func TestLoadScalarVersusMap(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		defaults Config
		contents string
		want     Config
	}{
		{
			name:     "user scalar replaces default map",
			defaults: Config{"server": map[string]any{"port": 8080}},
			contents: `{"server": "disabled"}`,
			want:     Config{"server": "disabled"},
		},
		{
			name:     "user map replaces default scalar",
			defaults: Config{"server": "auto"},
			contents: `{"server": {"port": 9090}}`,
			want:     Config{"server": map[string]any{"port": float64(9090)}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := Load(writeConfig(t, test.contents), test.defaults)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertConfig(t, got, test.want)
		})
	}
}

func TestLoadArrayIsKeptAsWritten(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		contents string
		want     []any
	}{
		{name: "empty array", contents: `{"features":[]}`, want: []any{}},
		{name: "non-empty array", contents: `{"features":[1,2]}`, want: []any{float64(1), float64(2)}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			defaults := Config{"features": []any{"a", "b"}}
			got, err := Load(writeConfig(t, test.contents), defaults)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertConfig(t, got, Config{"features": test.want})
		})
	}
}

func TestLoadNullIsKept(t *testing.T) {
	t.Parallel()

	defaults := Config{"log": map[string]any{"level": "info"}}
	got, err := Load(writeConfig(t, `{"log": null}`), defaults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertConfig(t, got, Config{"log": nil})
}

func TestLoadEmptyMapIsFilled(t *testing.T) {
	t.Parallel()

	defaults := Config{
		"db": map[string]any{"pool": map[string]any{"min_conns": 5}},
	}
	got, err := Load(writeConfig(t, `{"db": {}}`), defaults)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertConfig(t, got, Config{
		"db": map[string]any{"pool": map[string]any{"min_conns": 5}},
	})
}

func TestLoadInvalidJSON(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"truncated":     `{"server":`,
		"not an object": `[]`,
		"empty file":    ``,
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := Load(writeConfig(t, contents), Config{"server": "default"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if got != nil {
				t.Fatalf("expected nil config, got %#v", got)
			}
		})
	}
}

func TestLoadUnreadablePath(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("permission bits do not stop root from reading the file")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"server": "file"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatalf("chmod directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Errorf("restore directory permissions: %v", err)
		}
	})

	got, err := Load(path, Config{"server": "default"})
	if err == nil {
		t.Fatal("expected an error for an unreadable path")
	}
	if got != nil {
		t.Fatalf("expected nil config, got %#v", got)
	}
}

func TestLoadPathIsDirectory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}

	got, err := Load(path, Config{"server": "default"})
	if err == nil {
		t.Fatal("expected an error when the path is a directory")
	}
	if got != nil {
		t.Fatalf("expected nil config, got %#v", got)
	}
}

func TestLoadReturnsIndependentValues(t *testing.T) {
	t.Parallel()

	defaults := Config{
		"db": map[string]any{
			"pool": map[string]any{"min_conns": 5},
		},
		"features": []any{"a", "b"},
	}
	want := Config{
		"db": map[string]any{
			"pool": map[string]any{"min_conns": 5},
		},
		"features": []any{"a", "b"},
	}
	path := filepath.Join(t.TempDir(), "config.json")

	first, err := Load(path, defaults)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	second, err := Load(path, defaults)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	// Mutating one result must not affect another result or the defaults.
	first["db"].(map[string]any)["pool"].(map[string]any)["min_conns"] = 99
	first["features"].([]any)[0] = "changed"
	first["added"] = true

	assertConfig(t, second, want)
	assertConfig(t, defaults, want)
}

func TestString(t *testing.T) {
	t.Parallel()

	c := Config{
		"name":   "mjolnir",
		"server": map[string]any{"port": 8080},
	}
	want := "{\n  \"name\": \"mjolnir\",\n  \"server\": {\n    \"port\": 8080\n  }\n}"

	if got := c.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
