package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dfryer1193/mjolnir/utils/log"
)

// defaultLoader is the process-wide configuration singleton. Keeping the
// loaded state (and its logger) in a value lets tests build isolated loaders
// and run in parallel.
//
//nolint:gochecknoglobals // singleton pattern for loaded config
var defaultLoader = newLoader(log.GetLogger("config"))

// loader holds one loaded configuration. The package keeps a single default
// loader; tests construct their own.
type loader struct {
	mu       sync.Mutex
	instance Config
	loaded   bool
	logger   *slog.Logger
}

func newLoader(lg *slog.Logger) *loader {
	return &loader{logger: lg}
}

// fatal logs at Error and exits. The logging package is deliberately without
// a Fatal; a call site that must terminate logs and exits itself.
func (l *loader) fatal(msg string, err error) {
	if err != nil {
		l.logger.Error(msg, slog.Any("error", err))
	} else {
		l.logger.Error(msg)
	}
	os.Exit(1)
}

// Config is a map-based configuration structure.
// It uses map[string]any for maximum flexibility and JSON compatibility.
type Config map[string]any

const (
	defaultPort          = 8080
	defaultHost          = "localhost"
	defaultReadTimeout   = 30 * time.Second
	defaultWriteTimeout  = 30 * time.Second
	defaultLogLevel      = "info"
	defaultMinConns      = 5
	defaultMaxConns      = 25
	defaultMigrationsDir = "./migrations"
)

//nolint:gochecknoglobals // singleton pattern for defaults
var defaults = map[string]any{
	"server": map[string]any{
		"port":         defaultPort,
		"host":         defaultHost,
		"readTimeout":  defaultReadTimeout,
		"writeTimeout": defaultWriteTimeout,
	},
	"log": map[string]any{
		"level":       defaultLogLevel,
		"json_output": true,
	},
	"db": map[string]any{
		"pool": map[string]any{
			"min_conns": defaultMinConns,
			"max_conns": defaultMaxConns,
		},
	},
	"migrations": map[string]any{
		"directory": defaultMigrationsDir,
	},
}

// Load loads configuration from ./config.json, applies defaults,
// and returns the configuration singleton.
func Load() (Config, error) {
	return defaultLoader.load()
}

// load reads ./config.json into l exactly once, then returns the result.
func (l *loader) load() (Config, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.loaded {
		return l.instance, nil
	}

	l.instance = make(Config)
	l.loaded = true

	_, statErr := os.Stat("./config.json")
	switch {
	case statErr == nil:
		file, openErr := os.Open("./config.json")
		if openErr != nil {
			l.fatal("failed to open config.json", openErr)
		}
		defer file.Close()

		bytes, readErr := io.ReadAll(file)
		if readErr != nil {
			l.fatal("failed to read config.json", readErr)
		}

		if unmarshalErr := json.Unmarshal(bytes, &l.instance); unmarshalErr != nil {
			l.fatal("failed to parse config.json", unmarshalErr)
		}
	case errors.Is(statErr, os.ErrNotExist):
		l.logger.Warn("config.json not found, using defaults")
	default:
		l.fatal("failed to stat config.json", statErr)
	}

	applyDefaults(l.instance, defaults)

	if validateErr := validateRequired(l.instance); validateErr != nil {
		l.logger.Error("config validation failed", slog.Any("error", validateErr))
	}

	return l.instance, nil
}

// Get returns the singleton configuration instance.
func Get() Config {
	return defaultLoader.get()
}

func (l *loader) get() Config {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.loaded {
		l.fatal("config has not been loaded yet", nil)
	}
	return l.instance
}

// applyDefaults merges defaults into config recursively.
func applyDefaults(config, defaults Config) {
	for key, defaultValue := range defaults {
		if configValue, exists := config[key]; exists {
			if configMap, matched := configValue.(map[string]any); matched {
				if defaultMap, innerMatched := defaultValue.(map[string]any); innerMatched {
					applyDefaults(configMap, defaultMap)
					continue
				}
			}
		}
		config[key] = defaultValue
	}
}

// validateRequired checks for required fields that lack defaults.
func validateRequired(config Config) error {
	var errors []string

	for section := range defaults {
		if _, exists := config[section]; !exists {
			errors = append(errors, fmt.Sprintf("missing required section: %s", section))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("config validation errors: %s", strings.Join(errors, ", "))
	}
	return nil
}

// String returns a string representation of the configuration for debugging.
func (c Config) String() string {
	bytes, _ := json.MarshalIndent(c, "", "  ")
	return string(bytes)
}

// SetDirectory sets the config directory (for testing).
func SetDirectory(dir string) {
	if dir != "./config.json" {
		defaultLoader.logger.Warn("config directory override for testing", slog.String("directory", dir))
	}
}
