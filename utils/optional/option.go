package optional

import (
	"errors"
	"reflect"
)

// ErrNilValue is returned by Of when the value is a nil pointer, map, slice,
// channel, function, or interface. A present Option never holds a nil.
var ErrNilValue = errors.New("optional: value is nil")

// Option is a container for a value that may be absent. It is intended for
// value types. A present Option never holds a nil: Of rejects nil values
// with ErrNilValue. The zero value of Option[T] is an empty Option.
//
// See docs/adr/0002-option-explicit-presence.md.
type Option[T any] struct {
	value   T
	present bool
}

// Of returns a present Option holding value. It returns ErrNilValue when
// value is a nil pointer, map, slice, channel, function, or interface: a
// present Option never holds a nil. For value types — the intended use —
// the error never occurs.
func Of[T any](value T) (Option[T], error) {
	if isNil(value) {
		return Empty[T](), ErrNilValue
	}
	return Option[T]{value: value, present: true}, nil
}

// isNil reports whether v is a nil pointer, map, slice, channel, function,
// or interface. Value types have no nil, so it is always false for them.
func isNil(v any) bool {
	if v == nil { // a fully nil interface
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func, reflect.Interface:
		return rv.IsNil()
	case reflect.Invalid, // unreachable: a nil interface is caught above
		reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.String, reflect.UnsafePointer, reflect.Array, reflect.Struct:
		return false
	}
	return false
}

// Empty returns an absent Option.
func Empty[T any]() Option[T] {
	return Option[T]{}
}

// IsPresent reports whether o holds a value.
func (o Option[T]) IsPresent() bool {
	return o.present
}

// Get returns the held value and reports whether o is present. For an empty
// Option it returns the zero value of T with false.
func (o Option[T]) Get() (T, bool) {
	return o.value, o.present
}

// Map returns an Option holding f(value) when o is present, and an empty
// Option when o is empty. f is not called on an empty Option. If f returns
// a nil, the result is an empty Option.
func (o Option[T]) Map[U any](f func(T) U) Option[U] {
	if !o.present {
		return Empty[U]()
	}
	result, err := Of(f(o.value))
	if err != nil {
		return Empty[U]()
	}
	return result
}

// Filter returns o unchanged when it is present and f(value) is true, and an
// empty Option otherwise. f is not called on an empty Option.
func (o Option[T]) Filter(f func(T) bool) Option[T] {
	if !o.present || !f(o.value) {
		return Empty[T]()
	}
	return o
}

// OrElse returns the held value when o is present, and def when o is empty.
func (o Option[T]) OrElse(def T) T {
	if !o.present {
		return def
	}
	return o.value
}

// OrGet returns the held value when o is present, and fn() when o is empty.
// fn is not called on a present Option.
func (o Option[T]) OrGet(fn func() T) T {
	if !o.present {
		return fn()
	}
	return o.value
}
