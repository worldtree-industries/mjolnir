package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

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

//nolint:gochecknoglobals // singleton pattern for loaded config
var (
	configInstance Config
	configLoadOnce sync.Once
	configLoaded   bool
)

// Load loads configuration from ./config.json, applies defaults,
// and returns the configuration singleton.
func Load() (Config, error) {
	configLoadOnce.Do(func() {
		configInstance = make(Config)
		configLoaded = true

		_, statErr := os.Stat("./config.json")
		switch {
		case statErr == nil:
			file, openErr := os.Open("./config.json")
			if openErr != nil {
				log.Fatal().Err(openErr).Msg("failed to open config.json")
			}
			defer file.Close()

			bytes, readErr := io.ReadAll(file)
			if readErr != nil {
				log.Fatal().Err(readErr).Msg("failed to read config.json")
			}

			if unmarshalErr := json.Unmarshal(bytes, &configInstance); unmarshalErr != nil {
				log.Fatal().Err(unmarshalErr).Msg("failed to parse config.json")
			}
		case errors.Is(statErr, os.ErrNotExist):
			log.Warn().Msg("config.json not found, using defaults")
		default:
			log.Fatal().Err(statErr).Msg("failed to stat config.json")
		}

		applyDefaults(configInstance, defaults)

		if validateErr := validateRequired(configInstance); validateErr != nil {
			log.Error().Err(validateErr).Msg("config validation failed")
		}
	})

	return configInstance, nil
}

// Get returns the singleton configuration instance.
func Get() Config {
	if !configLoaded {
		log.Fatal().Msg("config has not been loaded yet")
	}
	return configInstance
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
		log.Warn().Str("directory", dir).Msg("config directory override for testing")
	}
}
