package optional

import (
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    any
		expected any
		isEmpty  bool
	}{
		{
			name:     "string value",
			value:    "hello",
			expected: "hello",
			isEmpty:  false,
		},
		{
			name:     "int value",
			value:    42,
			expected: 42,
			isEmpty:  false,
		},
		{
			name:     "zero int value",
			value:    0,
			expected: 0,
			isEmpty:  false,
		},
		{
			name:     "empty string value",
			value:    "",
			expected: "",
			isEmpty:  false,
		},
		{
			name:     "nil pointer",
			value:    (*string)(nil),
			expected: (*string)(nil),
			isEmpty:  false,
		},
		{
			name:     "boolean true",
			value:    true,
			expected: true,
			isEmpty:  false,
		},
		{
			name:     "boolean false",
			value:    false,
			expected: false,
			isEmpty:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.value.(type) {
			case string:
				opt := New(v)
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("New(%v).IsEmpty() = %v, want %v", v, opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != tt.expected {
					t.Errorf("New(%v).Get() = %v, want %v", v, opt.Get(), tt.expected)
				}
			case int:
				opt := New(v)
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("New(%v).IsEmpty() = %v, want %v", v, opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != tt.expected {
					t.Errorf("New(%v).Get() = %v, want %v", v, opt.Get(), tt.expected)
				}
			case *string:
				opt := New(v)
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("New(%v).IsEmpty() = %v, want %v", v, opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != tt.expected {
					t.Errorf("New(%v).Get() = %v, want %v", v, opt.Get(), tt.expected)
				}
			case bool:
				opt := New(v)
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("New(%v).IsEmpty() = %v, want %v", v, opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != tt.expected {
					t.Errorf("New(%v).Get() = %v, want %v", v, opt.Get(), tt.expected)
				}
			}
		})
	}
}

func TestSome(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    any
		expected any
		isEmpty  bool
	}{
		{
			name:     "string value",
			value:    "world",
			expected: "world",
			isEmpty:  false,
		},
		{
			name:     "int value",
			value:    100,
			expected: 100,
			isEmpty:  false,
		},
		{
			name:     "zero value",
			value:    0,
			expected: 0,
			isEmpty:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch v := tt.value.(type) {
			case string:
				opt := Some(v)
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("Some(%v).IsEmpty() = %v, want %v", v, opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != tt.expected {
					t.Errorf("Some(%v).Get() = %v, want %v", v, opt.Get(), tt.expected)
				}
			case int:
				opt := Some(v)
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("Some(%v).IsEmpty() = %v, want %v", v, opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != tt.expected {
					t.Errorf("Some(%v).Get() = %v, want %v", v, opt.Get(), tt.expected)
				}
			}
		})
	}
}

func TestEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		typeStr string
		isEmpty bool
	}{
		{
			name:    "empty string option",
			typeStr: "string",
			isEmpty: true,
		},
		{
			name:    "empty int option",
			typeStr: "int",
			isEmpty: true,
		},
		{
			name:    "empty bool option",
			typeStr: "bool",
			isEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			switch tt.typeStr {
			case "string":
				opt := Empty[string]()
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("Empty[string]().IsEmpty() = %v, want %v", opt.IsEmpty(), tt.isEmpty)
				}
				// Get() should return zero value for empty option
				if opt.Get() != "" {
					t.Errorf("Empty[string]().Get() = %v, want empty string", opt.Get())
				}
			case "int":
				opt := Empty[int]()
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("Empty[int]().IsEmpty() = %v, want %v", opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != 0 {
					t.Errorf("Empty[int]().Get() = %v, want 0", opt.Get())
				}
			case "bool":
				opt := Empty[bool]()
				if opt.IsEmpty() != tt.isEmpty {
					t.Errorf("Empty[bool]().IsEmpty() = %v, want %v", opt.IsEmpty(), tt.isEmpty)
				}
				if opt.Get() != false {
					t.Errorf("Empty[bool]().Get() = %v, want false", opt.Get())
				}
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		option   func() Option[string]
		expected bool
	}{
		{
			name:     "non-empty option",
			option:   func() Option[string] { return New("test") },
			expected: false,
		},
		{
			name:     "empty option",
			option:   func() Option[string] { return Empty[string]() },
			expected: true,
		},
		{
			name:     "some option",
			option:   func() Option[string] { return Some("value") },
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := tt.option()
			if got := opt.IsEmpty(); got != tt.expected {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		option   func() Option[int]
		expected int
	}{
		{
			name:     "get from new option",
			option:   func() Option[int] { return New(42) },
			expected: 42,
		},
		{
			name:     "get from some option",
			option:   func() Option[int] { return Some(100) },
			expected: 100,
		},
		{
			name:     "get from empty option returns zero value",
			option:   func() Option[int] { return Empty[int]() },
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := tt.option()
			if got := opt.Get(); got != tt.expected {
				t.Errorf("Get() = %v, want %v", got, tt.expected)
			}
		})
	}
}
