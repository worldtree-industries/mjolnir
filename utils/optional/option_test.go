package optional

import (
	"testing"
)

func TestOf(t *testing.T) {
	t.Parallel()

	t.Run("present holds the given value", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of("hello").Get(); !ok || v != "hello" {
			t.Errorf("Of(\"hello\").Get() = (%q, %v), want (\"hello\", true)", v, ok)
		}
	})

	t.Run("present holds the zero value of int", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of(0).Get(); !ok || v != 0 {
			t.Errorf("Of(0).Get() = (%d, %v), want (0, true)", v, ok)
		}
	})

	t.Run("present holds the empty string", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of("").Get(); !ok || v != "" {
			t.Errorf("Of(\"\").Get() = (%q, %v), want (\"\", true)", v, ok)
		}
	})

	t.Run("present holds false", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of(false).Get(); !ok || v != false {
			t.Errorf("Of(false).Get() = (%v, %v), want (false, true)", v, ok)
		}
	})

	t.Run("present may hold a nil pointer", func(t *testing.T) {
		t.Parallel()
		v, ok := Of[*string](nil).Get()
		if !ok {
			t.Fatal("Of[*string](nil) is empty, want present")
		}
		if v != nil {
			t.Errorf("Of[*string](nil).Get() = %v, want nil", v)
		}
	})
}

func TestEmpty(t *testing.T) {
	t.Parallel()

	t.Run("empty holds no string value", func(t *testing.T) {
		t.Parallel()
		if v, ok := Empty[string]().Get(); ok || v != "" {
			t.Errorf("Empty[string]().Get() = (%q, %v), want (\"\", false)", v, ok)
		}
	})

	t.Run("empty holds no int value", func(t *testing.T) {
		t.Parallel()
		if v, ok := Empty[int]().Get(); ok || v != 0 {
			t.Errorf("Empty[int]().Get() = (%d, %v), want (0, false)", v, ok)
		}
	})

	t.Run("empty holds no bool value", func(t *testing.T) {
		t.Parallel()
		if v, ok := Empty[bool]().Get(); ok || v != false {
			t.Errorf("Empty[bool]().Get() = (%v, %v), want (false, false)", v, ok)
		}
	})
}

func TestIsPresent(t *testing.T) {
	t.Parallel()

	if !Of("value").IsPresent() {
		t.Error("Of(\"value\").IsPresent() = false, want true")
	}
	if Empty[string]().IsPresent() {
		t.Error("Empty[string]().IsPresent() = true, want false")
	}
}

func TestMap(t *testing.T) {
	t.Parallel()

	t.Run("present maps the held value", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of(21).Map(func(i int) int { return i * 2 }).Get(); !ok || v != 42 {
			t.Errorf("Of(21).Map(*2).Get() = (%d, %v), want (42, true)", v, ok)
		}
	})

	t.Run("present maps across types", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of("hello").Map(func(s string) int { return len(s) }).Get(); !ok || v != 5 {
			t.Errorf("Of(\"hello\").Map(len).Get() = (%d, %v), want (5, true)", v, ok)
		}
	})

	t.Run("empty stays empty through Map", func(t *testing.T) {
		t.Parallel()
		if v, ok := Empty[int]().Map(func(i int) int { return i + 1 }).Get(); ok || v != 0 {
			t.Errorf("Empty[int]().Map(f).Get() = (%d, %v), want (0, false)", v, ok)
		}
	})
}

func TestFilter(t *testing.T) {
	t.Parallel()

	t.Run("present with a passing predicate stays present", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of(42).Filter(func(i int) bool { return i > 0 }).Get(); !ok || v != 42 {
			t.Errorf("Of(42).Filter(>0).Get() = (%d, %v), want (42, true)", v, ok)
		}
	})

	t.Run("present with a failing predicate becomes empty", func(t *testing.T) {
		t.Parallel()
		if v, ok := Of(42).Filter(func(i int) bool { return i > 100 }).Get(); ok || v != 0 {
			t.Errorf("Of(42).Filter(>100).Get() = (%d, %v), want (0, false)", v, ok)
		}
	})

	t.Run("empty stays empty through Filter", func(t *testing.T) {
		t.Parallel()
		if v, ok := Empty[int]().Filter(func(i int) bool { return true }).Get(); ok || v != 0 {
			t.Errorf("Empty[int]().Filter(f).Get() = (%d, %v), want (0, false)", v, ok)
		}
	})
}

func TestOrElse(t *testing.T) {
	t.Parallel()

	t.Run("present yields the held value, not the default", func(t *testing.T) {
		t.Parallel()
		if v := Of("set").OrElse("fallback"); v != "set" {
			t.Errorf("Of(\"set\").OrElse(\"fallback\") = %q, want %q", v, "set")
		}
	})

	t.Run("empty yields the default", func(t *testing.T) {
		t.Parallel()
		if v := Empty[string]().OrElse("fallback"); v != "fallback" {
			t.Errorf("Empty[string]().OrElse(\"fallback\") = %q, want %q", v, "fallback")
		}
	})
}

func TestOrGet(t *testing.T) {
	t.Parallel()

	t.Run("present yields the held value without calling the function", func(t *testing.T) {
		t.Parallel()
		called := false
		v := Of("set").OrGet(func() string {
			called = true
			return "fallback"
		})
		if v != "set" {
			t.Errorf("Of(\"set\").OrGet(f) = %q, want %q", v, "set")
		}
		if called {
			t.Error("OrGet called the function on a present Option, want not called")
		}
	})

	t.Run("empty yields the function result", func(t *testing.T) {
		t.Parallel()
		if v := Empty[string]().OrGet(func() string { return "computed" }); v != "computed" {
			t.Errorf("Empty[string]().OrGet(f) = %q, want %q", v, "computed")
		}
	})
}
