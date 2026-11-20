package environment_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dfryer1193/mjolnir/utils/environment"
)

func TestParse_AcceptsEachEnvironmentExactly(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"dev", "staging", "prod"} {
		got, err := environment.Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", s, err)
		}
		if got.String() != s {
			t.Errorf("Parse(%q).String() = %q", s, got.String())
		}
	}
}

func TestParse_RejectsAnythingNotExact(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "Dev", "PROD", " prod", "prod ", "production", "stage", "test"} {
		_, err := environment.Parse(s)
		if !errors.Is(err, environment.ErrUnknown) {
			t.Errorf("Parse(%q) error = %v, want ErrUnknown", s, err)
		}
	}
}

func TestList_HasEveryEnvironment(t *testing.T) {
	t.Parallel()
	want := []environment.Environment{"dev", "staging", "prod"}
	if got := environment.List(); !slices.Equal(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestList_ReturnsACopy(t *testing.T) {
	t.Parallel()
	environment.List()[0] = "mutated"
	if got := environment.List()[0]; got != "dev" {
		t.Errorf("List()[0] = %q after caller mutation, want dev", got)
	}
}

func TestList_EveryEntryParses(t *testing.T) {
	t.Parallel()
	for _, e := range environment.List() {
		got, err := environment.Parse(e.String())
		if err != nil || got != e {
			t.Errorf("Parse(%q) = %v, %v; want %v", e, got, err, e)
		}
	}
}

func TestFromEnv_ReadsNamedVariable(t *testing.T) {
	t.Setenv("TEST_ENVIRONMENT", "staging")
	got, err := environment.FromEnv("TEST_ENVIRONMENT")
	if err != nil {
		t.Fatalf("FromEnv error = %v", err)
	}
	if got != environment.Staging {
		t.Errorf("FromEnv = %q, want %q", got, environment.Staging)
	}
}

func TestFromEnv_UnsetIsMissing(t *testing.T) {
	_, err := environment.FromEnv("MJOLNIR_TEST_SURELY_UNSET")
	if !errors.Is(err, environment.ErrMissing) {
		t.Errorf("error = %v, want ErrMissing", err)
	}
	if errors.Is(err, environment.ErrUnknown) {
		t.Errorf("missing must not also be unknown: %v", err)
	}
}

func TestFromEnv_EmptyIsMissing(t *testing.T) {
	t.Setenv("TEST_ENVIRONMENT", "")
	_, err := environment.FromEnv("TEST_ENVIRONMENT")
	if !errors.Is(err, environment.ErrMissing) {
		t.Errorf("error = %v, want ErrMissing", err)
	}
}

func TestFromEnv_UnknownValueIsUnknown(t *testing.T) {
	t.Setenv("TEST_ENVIRONMENT", "Prod")
	_, err := environment.FromEnv("TEST_ENVIRONMENT")
	if !errors.Is(err, environment.ErrUnknown) {
		t.Errorf("error = %v, want ErrUnknown", err)
	}
	if errors.Is(err, environment.ErrMissing) {
		t.Errorf("unknown must not also be missing: %v", err)
	}
}
