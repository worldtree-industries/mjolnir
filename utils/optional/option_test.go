package optional

import (
	"errors"
	"testing"
)

func TestOf(t *testing.T) {
	t.Parallel()

	t.Run("present holds the given value", func(t *testing.T) {
		t.Parallel()
		opt, err := Of("hello")
		if err != nil {
			t.Fatalf("Of(\"hello\") error = %v, want nil", err)
		}
		if v, ok := opt.Get(); !ok || v != "hello" {
			t.Errorf("Of(\"hello\").Get() = (%q, %v), want (\"hello\", true)", v, ok)
		}
	})

	t.Run("present holds the zero value of int", func(t *testing.T) {
		t.Parallel()
		opt, err := Of(0)
		if err != nil {
			t.Fatalf("Of(0) error = %v, want nil", err)
		}
		if v, ok := opt.Get(); !ok || v != 0 {
			t.Errorf("Of(0).Get() = (%d, %v), want (0, true)", v, ok)
		}
	})

	t.Run("present holds the empty string", func(t *testing.T) {
		t.Parallel()
		opt, err := Of("")
		if err != nil {
			t.Fatalf("Of(\"\") error = %v, want nil", err)
		}
		if v, ok := opt.Get(); !ok || v != "" {
			t.Errorf("Of(\"\").Get() = (%q, %v), want (\"\", true)", v, ok)
		}
	})

	t.Run("present holds false", func(t *testing.T) {
		t.Parallel()
		opt, err := Of(false)
		if err != nil {
			t.Fatalf("Of(false) error = %v, want nil", err)
		}
		if v, ok := opt.Get(); !ok || v != false {
			t.Errorf("Of(false).Get() = (%v, %v), want (false, true)", v, ok)
		}
	})

	t.Run("present holds a non-nil pointer", func(t *testing.T) {
		t.Parallel()
		s := "value"
		opt, err := Of(&s)
		if err != nil {
			t.Fatalf("Of(&s) error = %v, want nil", err)
		}
		v, ok := opt.Get()
		if !ok || v == nil || *v != "value" {
			t.Errorf("Of(&s).Get() = (%v, %v), want (&\"value\", true)", v, ok)
		}
	})

	t.Run("nil values return ErrNilValue and an empty option", func(t *testing.T) {
		t.Parallel()

		if opt, err := Of[*string](nil); !errors.Is(err, ErrNilValue) || opt.IsPresent() {
			t.Errorf("Of[*string](nil) = (%v, %v), want (empty, ErrNilValue)", opt.IsPresent(), err)
		}
		if opt, err := Of[map[string]int](nil); !errors.Is(err, ErrNilValue) || opt.IsPresent() {
			t.Errorf("Of[map](nil) = (%v, %v), want (empty, ErrNilValue)", opt.IsPresent(), err)
		}
		if opt, err := Of[[]int](nil); !errors.Is(err, ErrNilValue) || opt.IsPresent() {
			t.Errorf("Of[[]int](nil) = (%v, %v), want (empty, ErrNilValue)", opt.IsPresent(), err)
		}
		if opt, err := Of[chan int](nil); !errors.Is(err, ErrNilValue) || opt.IsPresent() {
			t.Errorf("Of[chan int](nil) = (%v, %v), want (empty, ErrNilValue)", opt.IsPresent(), err)
		}
		if opt, err := Of[func(int)](nil); !errors.Is(err, ErrNilValue) || opt.IsPresent() {
			t.Errorf("Of[func(int)](nil) = (%v, %v), want (empty, ErrNilValue)", opt.IsPresent(), err)
		}
		if opt, err := Of[any](nil); !errors.Is(err, ErrNilValue) || opt.IsPresent() {
			t.Errorf("Of[any](nil) = (%v, %v), want (empty, ErrNilValue)", opt.IsPresent(), err)
		}
	})
}

func TestEmpty(t *testing.T) {
	t.Parallel()

	t.Run("zero value of Option is empty", func(t *testing.T) {
		t.Parallel()
		var opt Option[string]
		if opt.IsPresent() {
			t.Error("zero Option[string] is present, want empty")
		}
		if v, ok := opt.Get(); ok || v != "" {
			t.Errorf("zero Option[string].Get() = (%q, %v), want (\"\", false)", v, ok)
		}
	})

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

	opt, err := Of("value")
	if err != nil {
		t.Fatalf("Of(\"value\") error = %v, want nil", err)
	}
	if !opt.IsPresent() {
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
		opt, err := Of(21)
		if err != nil {
			t.Fatalf("Of(21) error = %v, want nil", err)
		}
		if v, ok := opt.Map(func(i int) int { return i * 2 }).Get(); !ok || v != 42 {
			t.Errorf("Of(21).Map(*2).Get() = (%d, %v), want (42, true)", v, ok)
		}
	})

	t.Run("present maps across types", func(t *testing.T) {
		t.Parallel()
		opt, err := Of("hello")
		if err != nil {
			t.Fatalf("Of(\"hello\") error = %v, want nil", err)
		}
		if v, ok := opt.Map(func(s string) int { return len(s) }).Get(); !ok || v != 5 {
			t.Errorf("Of(\"hello\").Map(len).Get() = (%d, %v), want (5, true)", v, ok)
		}
	})

	t.Run("empty stays empty through Map without calling the function", func(t *testing.T) {
		t.Parallel()
		called := false
		v, ok := Empty[int]().Map(func(i int) int {
			called = true
			return i + 1
		}).Get()
		if ok || v != 0 {
			t.Errorf("Empty[int]().Map(f).Get() = (%d, %v), want (0, false)", v, ok)
		}
		if called {
			t.Error("Map called the function on an empty Option, want not called")
		}
	})

	t.Run("mapping to a nil pointer yields an empty option", func(t *testing.T) {
		t.Parallel()
		opt, err := Of(42)
		if err != nil {
			t.Fatalf("Of(42) error = %v, want nil", err)
		}
		if v, ok := opt.Map(func(i int) *int { return nil }).Get(); ok || v != nil {
			t.Errorf("Map to nil pointer Get() = (%v, %v), want (nil, false)", v, ok)
		}
	})
}

func TestFilter(t *testing.T) {
	t.Parallel()

	t.Run("present with a passing predicate stays present", func(t *testing.T) {
		t.Parallel()
		opt, err := Of(42)
		if err != nil {
			t.Fatalf("Of(42) error = %v, want nil", err)
		}
		if v, ok := opt.Filter(func(i int) bool { return i > 0 }).Get(); !ok || v != 42 {
			t.Errorf("Of(42).Filter(>0).Get() = (%d, %v), want (42, true)", v, ok)
		}
	})

	t.Run("present with a failing predicate becomes empty", func(t *testing.T) {
		t.Parallel()
		opt, err := Of(42)
		if err != nil {
			t.Fatalf("Of(42) error = %v, want nil", err)
		}
		if v, ok := opt.Filter(func(i int) bool { return i > 100 }).Get(); ok || v != 0 {
			t.Errorf("Of(42).Filter(>100).Get() = (%d, %v), want (0, false)", v, ok)
		}
	})

	t.Run("empty stays empty through Filter without calling the predicate", func(t *testing.T) {
		t.Parallel()
		called := false
		v, ok := Empty[int]().Filter(func(i int) bool {
			called = true
			return true
		}).Get()
		if ok || v != 0 {
			t.Errorf("Empty[int]().Filter(f).Get() = (%d, %v), want (0, false)", v, ok)
		}
		if called {
			t.Error("Filter called the predicate on an empty Option, want not called")
		}
	})
}

func TestOrElse(t *testing.T) {
	t.Parallel()

	t.Run("present yields the held value, not the default", func(t *testing.T) {
		t.Parallel()
		opt, err := Of("set")
		if err != nil {
			t.Fatalf("Of(\"set\") error = %v, want nil", err)
		}
		if v := opt.OrElse("fallback"); v != "set" {
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
		opt, err := Of("set")
		if err != nil {
			t.Fatalf("Of(\"set\") error = %v, want nil", err)
		}
		called := false
		v := opt.OrGet(func() string {
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
