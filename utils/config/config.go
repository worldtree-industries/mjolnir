// Package config is a temporary configuration loader.
//
// The real configuration system has not been designed yet. This package exists
// only so callers can read a JSON file over a set of defaults without a
// process-wide singleton. It will be replaced; do not build new APIs on it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
)

// Config is a map-based configuration structure.
// It uses map[string]any for maximum flexibility and JSON compatibility.
type Config map[string]any

// Load reads the JSON file at path over defaults and returns the merged
// configuration. It never mutates defaults and every call returns an
// independent value.
//
// A missing file is not an error: Load returns a deep copy of defaults. A file
// that cannot be read or parsed is an error. Fields absent from the file are
// filled from defaults; explicit values in the file always win, including
// scalars, slices, arrays, null, and empty maps.
func Load(path string, defaults Config) (Config, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cloneMap(defaults), nil
		}
		return nil, fmt.Errorf("stat config file %q: %w", path, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	var user Config
	if unmarshalErr := json.Unmarshal(data, &user); unmarshalErr != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, unmarshalErr)
	}

	return merge(defaults, user), nil
}

// merge returns defaults with the user values applied. Keys missing from the
// user config are taken from defaults; non-empty maps are merged recursively;
// every other user value is kept exactly as written.
func merge(defaults, user Config) map[string]any {
	merged := cloneMap(defaults)

	for key, userValue := range user {
		defaultValue, hasDefault := merged[key]
		if !hasDefault {
			merged[key] = userValue
			continue
		}

		userMap, userIsMap := valueAsMap(userValue)
		defaultMap, defaultIsMap := valueAsMap(defaultValue)

		// An empty user map is a leaf: the user wrote it, so keep it.
		if userIsMap && defaultIsMap && len(userMap) > 0 {
			merged[key] = merge(defaultMap, userMap)
			continue
		}

		merged[key] = userValue
	}

	return merged
}

// valueAsMap returns value as a string-keyed map when it is one.
func valueAsMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case Config:
		return typed, true
	default:
		return nil, false
	}
}

// cloneMap deep-copies a configuration map so callers cannot share state.
func cloneMap(source map[string]any) map[string]any {
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = clone(value)
	}
	return cloned
}

// clone deep-copies the container types a configuration tree is built from.
func clone(value any) any {
	if value == nil {
		return nil
	}

	switch typed := value.(type) {
	case Config:
		return cloneMap(typed)
	case map[string]any:
		return cloneMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for i, item := range typed {
			cloned[i] = clone(item)
		}
		return cloned
	default:
		return cloneReflect(reflect.ValueOf(value)).Interface()
	}
}

// cloneReflect copies maps, slices, and arrays of any type. Other values are
// immutable for configuration purposes and are returned as-is.
func cloneReflect(value reflect.Value) reflect.Value {
	kind := value.Kind()

	if kind == reflect.Interface {
		if value.IsNil() {
			return value
		}
		cloned := cloneReflect(value.Elem())
		result := reflect.New(value.Type()).Elem()
		result.Set(cloned)
		return result
	}

	if kind == reflect.Map {
		if value.IsNil() {
			return value
		}
		cloned := reflect.MakeMapWithSize(value.Type(), value.Len())
		iter := value.MapRange()
		for iter.Next() {
			cloned.SetMapIndex(iter.Key(), cloneReflect(iter.Value()))
		}
		return cloned
	}

	if kind == reflect.Slice {
		if value.IsNil() {
			return value
		}
		cloned := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
		for i := range value.Len() {
			cloned.Index(i).Set(cloneReflect(value.Index(i)))
		}
		return cloned
	}

	if kind == reflect.Array {
		cloned := reflect.New(value.Type()).Elem()
		for i := range value.Len() {
			cloned.Index(i).Set(cloneReflect(value.Index(i)))
		}
		return cloned
	}

	return value
}

// String returns a string representation of the configuration for debugging.
func (c Config) String() string {
	bytes, _ := json.MarshalIndent(c, "", "  ")
	return string(bytes)
}
