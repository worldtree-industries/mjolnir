// Package environment defines the fixed set of Environments (deployment
// stages) a program can run in, and reads the current one from the process
// environment.
package environment

import (
	"errors"
	"fmt"
	"os"
	"slices"
)

// Environment is a deployment stage. Its string form is its identity.
type Environment string

// The Environments. To add one, add a constant here and list it in all.
const (
	Dev     Environment = "dev"
	Staging Environment = "staging"
	Prod    Environment = "prod"
)

// all is every Environment, in declaration order.
//
//nolint:gochecknoglobals // immutable set, copied out by List
var all = []Environment{Dev, Staging, Prod}

// ErrMissing reports that the environment variable is unset or empty.
var ErrMissing = errors.New("environment is missing")

// ErrUnknown reports a value that is not exactly one of the Environments.
var ErrUnknown = errors.New("environment is unknown")

// String returns the Environment's string form.
func (e Environment) String() string { return string(e) }

// List returns every Environment. The caller may modify the result.
func List() []Environment { return slices.Clone(all) }

// Parse returns the Environment whose string form is exactly s. It does no
// trimming, case-folding or defaulting; anything else is ErrUnknown.
func Parse(s string) (Environment, error) {
	e := Environment(s)
	if !slices.Contains(all, e) {
		return "", fmt.Errorf("%w: %q", ErrUnknown, s)
	}
	return e, nil
}

// FromEnv reads the Environment from the process environment variable named
// name. An unset or empty variable is ErrMissing; a value that is not exactly
// an Environment is ErrUnknown. Test with [errors.Is].
func FromEnv(name string) (Environment, error) {
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("%w: %s is not set", ErrMissing, name)
	}
	e, err := Parse(v)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return e, nil
}
